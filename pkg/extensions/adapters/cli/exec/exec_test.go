package exec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cr "github.com/1024XEngineer/anyclaw/pkg/extensions/adapters/cli/registry"
)

func TestExecutorRunsRegisteredHandler(t *testing.T) {
	registry := newTestRegistry(t)
	executor := NewExecutor(registry)
	executor.RegisterHandler("echo", func(ctx context.Context, args []string) (string, error) {
		return strings.Join(args, " "), nil
	})

	args := []string{"hello", "world"}
	result := executor.Exec(context.Background(), "echo", args)
	args[0] = "mutated"

	if result.Error != "" || result.ExitCode != 0 {
		t.Fatalf("Exec result = %+v, want success", result)
	}
	if result.Output != "hello world" {
		t.Fatalf("output = %q, want hello world", result.Output)
	}
	if result.Args[0] != "hello" {
		t.Fatalf("result args aliased input args: %+v", result.Args)
	}
}

func TestExecutorReportsHandlerError(t *testing.T) {
	registry := newTestRegistry(t)
	executor := NewExecutor(registry)
	executor.RegisterHandler("echo", func(ctx context.Context, args []string) (string, error) {
		return "partial", errors.New("boom")
	})

	result := executor.Exec(context.Background(), "echo", nil)
	if result.Output != "partial" || result.Error != "boom" || result.ExitCode != 1 {
		t.Fatalf("Exec result = %+v, want handler error", result)
	}
}

func TestExecutorReportsMissingOrUninstalledCLI(t *testing.T) {
	registry := newTestRegistry(t)
	executor := NewExecutor(registry)

	missing := executor.Exec(context.Background(), "missing", nil)
	if !strings.Contains(missing.Error, "CLI not found") {
		t.Fatalf("missing result = %+v", missing)
	}

	uninstalled := executor.Exec(context.Background(), "git", nil)
	if !strings.Contains(uninstalled.Error, "CLI not installed") || uninstalled.Installed {
		t.Fatalf("uninstalled result = %+v", uninstalled)
	}
}

func TestExecutorDelegatesRegistryViews(t *testing.T) {
	registry := newTestRegistry(t)
	executor := NewExecutor(registry)

	if got := len(executor.List()); got != 2 {
		t.Fatalf("List count = %d, want 2", got)
	}
	if got := len(executor.Search("git", "", 10)); got != 1 {
		t.Fatalf("Search count = %d, want 1", got)
	}
	if got := executor.Categories()["dev"]; got != 1 {
		t.Fatalf("dev category = %d, want 1", got)
	}
	if data, err := executor.JSON(); err != nil || !strings.Contains(data, `"entries_count": 2`) {
		t.Fatalf("JSON = (%q, %v), want entries_count", data, err)
	}
}

func TestExecutorExecBinarySuccess(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "success")
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("Executable: %v", err)
	}

	result := (&Executor{}).execBinary(
		context.Background(),
		executable,
		[]string{"-test.run=TestHelperProcess", "--", "hello"},
		&ExecResult{Name: "helper"},
	)

	if result.Error != "" || result.ExitCode != 0 || !result.Installed {
		t.Fatalf("execBinary result = %+v, want success", result)
	}
	if !strings.Contains(result.Output, "helper output") {
		t.Fatalf("output = %q, want helper output", result.Output)
	}
}

func TestExecutorExecBinaryFailure(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "failure")
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("Executable: %v", err)
	}

	result := (&Executor{}).execBinary(
		context.Background(),
		executable,
		[]string{"-test.run=TestHelperProcess"},
		&ExecResult{Name: "helper"},
	)

	if result.Error == "" || result.ExitCode != 7 || !result.Installed {
		t.Fatalf("execBinary result = %+v, want exit 7 failure", result)
	}
}

func TestExecutorAutoInstallRejectsInvalidInstallCommand(t *testing.T) {
	registry := newTestRegistry(t)
	executor := NewExecutor(registry)

	result := executor.AutoInstall(context.Background(), "git")
	if result.Error != "Invalid install command" || result.ExitCode != 1 {
		t.Fatalf("AutoInstall result = %+v, want invalid install command without process execution", result)
	}
}

func TestExecutorAutoInstallEarlyErrors(t *testing.T) {
	registry := newTestRegistry(t)
	executor := NewExecutor(registry)

	missing := executor.AutoInstall(context.Background(), "missing")
	if missing.Error != "CLI not found in registry" || missing.ExitCode != 1 {
		t.Fatalf("missing install = %+v", missing)
	}

	noInstallRegistry := newRegistryWithEntries(t, []byte(`{"clis":[{"name":"no-install","display_name":"No Install"}]}`))
	noInstall := NewExecutor(noInstallRegistry).AutoInstall(context.Background(), "no-install")
	if noInstall.Error != "No install command available" || noInstall.ExitCode != 1 {
		t.Fatalf("no install command = %+v", noInstall)
	}
}

func TestHelperProcess(t *testing.T) {
	switch os.Getenv("GO_WANT_HELPER_PROCESS") {
	case "success":
		fmt.Fprint(os.Stdout, "helper output")
		os.Exit(0)
	case "failure":
		fmt.Fprint(os.Stderr, "helper failure")
		os.Exit(7)
	}
}

func newTestRegistry(t *testing.T) *cr.Registry {
	t.Helper()
	data := []byte(`{"clis":[
		{"name":"echo","display_name":"Echo","category":"utility","entry_point":"echo","install_cmd":"builtin"},
		{"name":"git","display_name":"Git","category":"dev","entry_point":"git","install_cmd":"git"}
	]}`)
	return newRegistryWithEntries(t, data)
}

func newRegistryWithEntries(t *testing.T, data []byte) *cr.Registry {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "registry.json"), data, 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	registry, err := cr.NewRegistry(root)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return registry
}
