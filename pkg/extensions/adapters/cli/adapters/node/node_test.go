package node

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
	if os.Getenv("ANYCLAW_NODE_ADAPTER_HELPER") == "1" {
		name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
		name = strings.TrimSuffix(name, "-helper")
		fmt.Print(strings.TrimSpace(name + " " + strings.Join(os.Args[1:], " ")))
		os.Exit(0)
	}

	os.Exit(m.Run())
}

func TestNPMCommandsUseNPMExecutable(t *testing.T) {
	t.Setenv("ANYCLAW_NODE_ADAPTER_HELPER", "1")
	nodePath, npmPath := fakeNodeCommandPaths(t)
	client := NewClient(Config{NodePath: nodePath, NPMPath: npmPath})

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "info uses npm for npm version",
			args: []string{"info"},
			want: "Node: node --version\nNPM: npm --version",
		},
		{
			name: "run uses npm run",
			args: []string{"run", "test"},
			want: "npm run test",
		},
		{
			name: "npm command uses npm directly",
			args: []string{"npm", "install", "left-pad"},
			want: "npm install left-pad",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := client.Run(context.Background(), tt.args)
			if err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Run(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func fakeNodeCommandPaths(t *testing.T) (string, string) {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	dir := t.TempDir()
	nodePath := filepath.Join(dir, "node-helper.exe")
	npmPath := filepath.Join(dir, "npm-helper.exe")
	copyFile(t, exe, nodePath)
	copyFile(t, exe, npmPath)
	return nodePath, npmPath
}

func copyFile(t *testing.T, src, dst string) {
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
