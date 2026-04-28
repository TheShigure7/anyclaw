package notebooklm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSourceAddUsesDocumentedNotebookIDAndSource(t *testing.T) {
	const notebookID = "nb-1"
	const sourceURL = "https://example.com/source"

	var gotPath string
	var gotBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := New(Config{APIKey: "test-key"})
	client.baseURL = server.URL
	client.httpClient = server.Client()

	if _, err := client.Execute(context.Background(), []string{"source", "add", notebookID, sourceURL}); err != nil {
		t.Fatalf("source add returned error: %v", err)
	}
	if gotPath != "/notebooks/"+notebookID+"/sources" {
		t.Fatalf("expected source request for notebook %q, got path %q", notebookID, gotPath)
	}
	if gotBody["source"] != sourceURL {
		t.Fatalf("expected source %q, got body %#v", sourceURL, gotBody)
	}
}
