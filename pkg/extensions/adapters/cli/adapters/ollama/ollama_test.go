package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunListsModels(t *testing.T) {
	server := newOllamaTestServer(t)
	client := newTestClient(t, server)

	output, err := client.Run(context.Background(), []string{"list"})
	if err != nil {
		t.Fatalf("Run list: %v", err)
	}
	if output != "llama3.2\ncodellama" {
		t.Fatalf("output = %q, want model list", output)
	}
}

func TestGenerateSendsPromptAndReturnsResponse(t *testing.T) {
	server := newOllamaTestServer(t)
	client := newTestClient(t, server)

	output, err := client.Run(context.Background(), []string{"run", "hello", "ollama"})
	if err != nil {
		t.Fatalf("Run generate: %v", err)
	}
	if output != "generated response" {
		t.Fatalf("output = %q, want generated response", output)
	}
}

func TestChatSendsMessageAndReturnsAssistantContent(t *testing.T) {
	server := newOllamaTestServer(t)
	client := newTestClient(t, server)

	output, err := client.Run(context.Background(), []string{"chat", "hello"})
	if err != nil {
		t.Fatalf("Run chat: %v", err)
	}
	if output != "chat response" {
		t.Fatalf("output = %q, want chat response", output)
	}
}

func TestShowReturnsFormattedJSON(t *testing.T) {
	server := newOllamaTestServer(t)
	client := newTestClient(t, server)

	output, err := client.Run(context.Background(), []string{"show", "llama3.2"})
	if err != nil {
		t.Fatalf("Run show: %v", err)
	}
	if !strings.Contains(output, `"name": "llama3.2"`) {
		t.Fatalf("output = %q, want model json", output)
	}
}

func TestStatusReportsReachability(t *testing.T) {
	server := newOllamaTestServer(t)
	client := newTestClient(t, server)

	output, err := client.Run(context.Background(), []string{"status"})
	if err != nil {
		t.Fatalf("Run status: %v", err)
	}
	if output != "Ollama is running" {
		t.Fatalf("output = %q, want running", output)
	}
}

func TestNewClientRejectsRemoteBaseURLByDefault(t *testing.T) {
	_, err := NewClient(Config{BaseURL: "http://example.com:11434"})
	if err == nil {
		t.Fatal("expected remote URL rejection")
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("error = %v, want loopback rejection", err)
	}
}

func TestNewClientAllowsRemoteWhenExplicit(t *testing.T) {
	client, err := NewClient(Config{BaseURL: "http://example.com:11434", AllowRemote: true})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.endpoint("/api/tags") != "http://example.com:11434/api/tags" {
		t.Fatalf("endpoint = %q, want remote endpoint", client.endpoint("/api/tags"))
	}
}

func TestEndpointPreservesBasePathAndDropsQuery(t *testing.T) {
	client, err := NewClient(Config{BaseURL: "http://127.0.0.1:11434/ollama?token=secret#frag"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if got := client.endpoint("/api/tags"); got != "http://127.0.0.1:11434/ollama/api/tags" {
		t.Fatalf("endpoint = %q, want base path without query", got)
	}
}

func TestRunReturnsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	client := newTestClient(t, server)

	_, err := client.Run(context.Background(), []string{"list"})
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	if !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("error = %v, want HTTP status", err)
	}
}

func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	client, err := NewClient(Config{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func newOllamaTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tags":
			writeJSON(t, w, listResponse{Models: []Model{
				{Name: "llama3.2"},
				{Name: "codellama"},
			}})
		case "/api/generate":
			var req generateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode generate request: %v", err)
			}
			if req.Prompt != "hello ollama" || req.Model != defaultModel || req.Stream {
				t.Fatalf("generate request = %+v, want prompt/model/no stream", req)
			}
			writeJSON(t, w, generateResponse{Response: "generated response"})
		case "/api/chat":
			var req chatRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode chat request: %v", err)
			}
			if len(req.Messages) != 1 || req.Messages[0].Content != "hello" || req.Messages[0].Role != "user" {
				t.Fatalf("chat request = %+v, want user hello", req)
			}
			writeJSON(t, w, chatResponse{Message: Message{Role: "assistant", Content: "chat response"}})
		case "/api/show":
			var req map[string]string
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode show request: %v", err)
			}
			if req["name"] != "llama3.2" {
				t.Fatalf("show request = %+v, want llama3.2", req)
			}
			writeJSON(t, w, map[string]any{"name": "llama3.2", "family": "llama"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("Encode: %v", err)
	}
}
