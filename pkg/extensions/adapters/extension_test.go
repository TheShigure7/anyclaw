package extension

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBuiltinExtensionCatalogHasRecommendedCount(t *testing.T) {
	if got := len(builtinExtensionManifests()); got != 22 {
		t.Fatalf("expected 22 builtin extensions, got %d", got)
	}
}

func TestDiscoverIncludesBuiltinExtensionsWithoutDirectory(t *testing.T) {
	registry := NewRegistry("missing-dir")
	items, err := registry.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(items) != 22 {
		t.Fatalf("expected 22 builtin extensions from discover, got %d", len(items))
	}
}

func TestLoadAllRegistersBuiltinExtensionsWithoutDirectory(t *testing.T) {
	registry := NewRegistry("missing-dir")
	if err := registry.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if got := len(registry.List()); got != 22 {
		t.Fatalf("expected 22 loaded builtin extensions, got %d", got)
	}
	ext, ok := registry.Get("zoom")
	if !ok {
		t.Fatal("expected zoom builtin extension")
	}
	if !ext.Manifest.Builtin {
		t.Fatal("expected builtin flag on zoom manifest")
	}
}

func TestRegistryReturnsDefensiveCopies(t *testing.T) {
	registry := NewRegistry("missing-dir")
	ext := &Extension{
		Manifest: Manifest{
			ID:       "custom",
			Name:     "Custom",
			Version:  "1.0.0",
			Kind:     "tool",
			Channels: []string{"original"},
		},
		Enabled: true,
		Config:  map[string]any{"mode": "safe"},
	}
	if err := registry.Register(ext); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ext.Manifest.Name = "mutated"
	ext.Config["mode"] = "changed"

	got, ok := registry.Get("custom")
	if !ok {
		t.Fatal("expected registered extension")
	}
	got.Manifest.Name = "external mutation"
	got.Manifest.Channels[0] = "changed"
	got.Config["mode"] = "external"

	again, ok := registry.Get("custom")
	if !ok {
		t.Fatal("expected registered extension")
	}
	if again.Manifest.Name != "Custom" {
		t.Fatalf("expected stored manifest name to remain Custom, got %q", again.Manifest.Name)
	}
	if again.Manifest.Channels[0] != "original" {
		t.Fatalf("expected stored channel to remain original, got %q", again.Manifest.Channels[0])
	}
	if again.Config["mode"] != "safe" {
		t.Fatalf("expected stored config to remain safe, got %v", again.Config["mode"])
	}
}

func TestDiscoverRejectsDuplicateManifestID(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, filepath.Join(dir, "zoom"), Manifest{
		ID:      "zoom",
		Name:    "Duplicate Zoom",
		Version: "1.0.0",
		Kind:    "tool",
	})

	registry := NewRegistry(dir)
	if _, err := registry.Discover(); err == nil {
		t.Fatal("expected duplicate manifest ID error")
	}
}

func TestLoadAllUsesManifestDirectoryNotManifestID(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, filepath.Join(dir, "custom-dir"), Manifest{
		ID:         "custom-extension",
		Name:       "Custom Extension",
		Version:    "1.0.0",
		Kind:       "tool",
		Entrypoint: "bin/custom",
	})

	registry := NewRegistry(dir)
	if err := registry.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	ext, ok := registry.Get("custom-extension")
	if !ok {
		t.Fatal("expected custom extension")
	}
	if ext.Path != filepath.Join(dir, "custom-dir") {
		t.Fatalf("expected extension path to use containing directory, got %q", ext.Path)
	}
}

func TestLoadExtensionRejectsUnsafeEntrypoint(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, Manifest{
		ID:         "unsafe",
		Name:       "Unsafe",
		Version:    "1.0.0",
		Kind:       "tool",
		Entrypoint: "../outside",
	})

	if _, err := LoadExtension(dir); err == nil {
		t.Fatal("expected unsafe entrypoint error")
	}
}

func writeManifest(t *testing.T, dir string, manifest Manifest) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestFileName), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
