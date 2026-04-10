package market

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreInstallManifestFileAndPersistentSubagentProfiles(t *testing.T) {
	workDir := t.TempDir()
	store, err := NewStore(workDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	manifestPath := filepath.Join(workDir, "code-reviewer.json")
	writeTestFile(t, manifestPath, `{
  "id": "agent:code-reviewer",
  "kind": "agent",
  "name": "code-reviewer",
  "display_name": "Code Reviewer",
  "version": "1.0.0",
  "description": "Internal code review persistent subagent",
  "agent": {
    "mode": "persistent_subagent",
    "managed_by_main": true,
    "model_mode": "inherit_main",
    "visibility": "internal_visible",
    "domain": "code review",
    "expertise": ["go", "tests"],
    "system_prompt": "Review code carefully."
  }
}`)

	manifest, err := store.InstallManifestFile(manifestPath)
	if err != nil {
		t.Fatalf("InstallManifestFile: %v", err)
	}
	if manifest.Kind != KindAgent {
		t.Fatalf("expected agent kind, got %q", manifest.Kind)
	}

	items, err := store.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 installed package, got %d", len(items))
	}

	profiles, err := store.PersistentSubagentProfiles()
	if err != nil {
		t.Fatalf("PersistentSubagentProfiles: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("expected 1 persistent subagent profile, got %d", len(profiles))
	}
	if profiles[0].ID != "agent:code-reviewer" {
		t.Fatalf("unexpected persistent subagent id %q", profiles[0].ID)
	}
	if !profiles[0].IsManagedByMain() {
		t.Fatal("expected market persistent subagent to be managed by main")
	}
	receipt, err := store.Receipt("agent:code-reviewer")
	if err != nil {
		t.Fatalf("Receipt: %v", err)
	}
	if receipt.PackageID != "agent:code-reviewer" {
		t.Fatalf("unexpected receipt package id %q", receipt.PackageID)
	}
	history, err := store.History()
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history) != 1 || history[0].Action != "install" {
		t.Fatalf("expected install history, got %#v", history)
	}
	if err := store.Uninstall("agent:code-reviewer"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	items, err = store.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled after uninstall: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 installed packages after uninstall, got %d", len(items))
	}
}

func TestValidateManifestRejectsUnmanagedAgent(t *testing.T) {
	manifest := PackageManifest{
		ID:   "agent:bad",
		Kind: KindAgent,
		Agent: &AgentSpec{
			Mode:          "persistent_subagent",
			ManagedByMain: false,
		},
	}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected validation error for unmanaged agent")
	}
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
