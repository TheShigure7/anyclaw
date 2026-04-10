package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSessionToolsSpawnAndSend(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST /sessions, got %s", r.Method)
		}
		if got := r.URL.Query().Get("workspace"); got != "workspace-1" {
			t.Fatalf("expected workspace query, got %q", got)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer token-1" {
			t.Fatalf("expected bearer token, got %q", auth)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode spawn body: %v", err)
		}
		if body["title"] != "My Session" {
			t.Fatalf("unexpected spawn body: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"sess-1","title":"My Session","agent":"assistant"}`))
	})
	mux.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST /chat, got %s", r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode chat body: %v", err)
		}
		if body["session_id"] != "sess-1" || body["message"] != "hello" {
			t.Fatalf("unexpected chat body: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":"done","session":{"id":"sess-1","title":"My Session"}}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	registry := NewRegistry()
	RegisterSessionTools(registry, BuiltinOptions{
		GatewayBaseURL:    server.URL,
		GatewayAPIToken:   "token-1",
		GatewayHTTPClient: server.Client(),
	})

	spawnResult, err := registry.Call(context.Background(), "sessions_spawn", map[string]any{
		"title":     "My Session",
		"agent":     "assistant",
		"workspace": "workspace-1",
	})
	if err != nil {
		t.Fatalf("sessions_spawn: %v", err)
	}
	if !strings.Contains(spawnResult, `"session_key":"sess-1"`) {
		t.Fatalf("expected normalized session_key, got %q", spawnResult)
	}

	sendResult, err := registry.Call(context.Background(), "sessions_send", map[string]any{
		"session_key": "sess-1",
		"message":     "hello",
	})
	if err != nil {
		t.Fatalf("sessions_send: %v", err)
	}
	if !strings.Contains(sendResult, `"response":"done"`) {
		t.Fatalf("expected response payload, got %q", sendResult)
	}
	if !strings.Contains(sendResult, `"session_key":"sess-1"`) {
		t.Fatalf("expected normalized session_key in send result, got %q", sendResult)
	}
}

func TestSessionToolsRequireWorkspaceAndSessionID(t *testing.T) {
	registry := NewRegistry()
	RegisterSessionTools(registry, BuiltinOptions{})

	if _, err := registry.Call(context.Background(), "sessions_spawn", map[string]any{"title": "missing workspace"}); err == nil {
		t.Fatal("expected workspace validation error")
	}
	if _, err := registry.Call(context.Background(), "sessions_send", map[string]any{"message": "hello"}); err == nil {
		t.Fatal("expected session_id validation error")
	}
}
