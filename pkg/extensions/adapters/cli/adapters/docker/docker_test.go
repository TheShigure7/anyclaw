package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("ANYCLAW_DOCKER_ADAPTER_HELPER") == "1" {
		if logPath := os.Getenv("ANYCLAW_DOCKER_ADAPTER_LOG"); logPath != "" {
			_ = os.WriteFile(logPath, []byte(strings.Join(os.Args[1:], " ")), 0o644)
		}
		fmt.Print(strings.Join(os.Args[1:], " "))
		os.Exit(0)
	}

	os.Exit(m.Run())
}

func TestStartUsesDockerStartSubcommand(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "docker-args.txt")
	t.Setenv("ANYCLAW_DOCKER_ADAPTER_HELPER", "1")
	t.Setenv("ANYCLAW_DOCKER_ADAPTER_LOG", logPath)
	client := NewClient(Config{DockerPath: fakeDockerPath(t)})

	if _, err := client.Run(context.Background(), []string{"start", "existing-container"}); err != nil {
		t.Fatalf("Run start returned error: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read helper log: %v", err)
	}
	if got, want := string(data), "start existing-container"; got != want {
		t.Fatalf("docker args = %q, want %q", got, want)
	}
}

func fakeDockerPath(t *testing.T) string {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	path := filepath.Join(t.TempDir(), "docker-helper.exe")
	copyExecutable(t, exe, path)
	return path
}

func copyExecutable(t *testing.T, src, dst string) {
	t.Helper()

	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open source executable: %v", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatalf("create helper executable: %v", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy helper executable: %v", err)
	}
}
