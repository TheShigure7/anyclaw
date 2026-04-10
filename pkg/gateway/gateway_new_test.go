package gateway

import (
	"os"
	"path/filepath"
	"testing"

	appRuntime "github.com/anyclaw/anyclaw/pkg/runtime"
)

func TestNewReturnsErrorForNilApp(t *testing.T) {
	server, err := New(nil)
	if err == nil {
		t.Fatal("expected error for nil runtime app")
	}
	if server != nil {
		t.Fatalf("expected nil server on error, got %#v", server)
	}
}

func TestNewReturnsErrorWhenStoreInitFails(t *testing.T) {
	tempDir := t.TempDir()
	workDirFile := filepath.Join(tempDir, "workdir.txt")
	if err := os.WriteFile(workDirFile, []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("WriteFile(workdir): %v", err)
	}

	server, err := New(&appRuntime.App{WorkDir: workDirFile})
	if err == nil {
		t.Fatal("expected store init error")
	}
	if server != nil {
		t.Fatalf("expected nil server on error, got %#v", server)
	}
}
