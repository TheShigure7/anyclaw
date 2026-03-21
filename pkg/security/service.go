package security

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/anyclaw/anyclaw/pkg/domain/assistant"
	"github.com/anyclaw/anyclaw/pkg/domain/task"
)

type Decision struct {
	Allowed          bool
	RequiresApproval bool
	RiskLevel        string
	Reason           string
}

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) PrepareWorkspace(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("workspace path is required")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(absPath, 0o755); err != nil {
		return "", err
	}
	return absPath, nil
}

func (s *Service) EvaluateOperation(actor assistant.Assistant, op task.Operation, workspaceRoot string) Decision {
	tool := strings.TrimSpace(op.Tool)
	if tool == "" {
		return Decision{Allowed: false, RiskLevel: "high", Reason: "tool is required"}
	}

	switch tool {
	case "list_dir", "read_file", "write_file":
		path := op.Params["path"]
		if path == "" {
			return Decision{Allowed: false, RiskLevel: "high", Reason: "path is required"}
		}
		if _, err := s.ResolveWorkspacePath(workspaceRoot, path); err != nil {
			return Decision{Allowed: false, RiskLevel: "high", Reason: err.Error()}
		}
	case "run_command":
		command := strings.TrimSpace(op.Params["command"])
		if command == "" {
			return Decision{Allowed: false, RiskLevel: "high", Reason: "command is required"}
		}
		if strings.EqualFold(actor.PermissionProfile, "readonly") {
			return Decision{Allowed: false, RiskLevel: "high", Reason: "readonly assistants cannot run commands"}
		}
		decision := Decision{Allowed: true, RiskLevel: "medium", Reason: "command allowed"}
		if looksDangerous(command) {
			decision.RequiresApproval = true
			decision.RiskLevel = "high"
			decision.Reason = "command requires approval"
		}
		return decision
	default:
		return Decision{Allowed: false, RiskLevel: "high", Reason: "unsupported tool"}
	}

	if tool == "write_file" {
		if strings.EqualFold(actor.PermissionProfile, "readonly") {
			return Decision{Allowed: false, RiskLevel: "high", Reason: "readonly assistants cannot write files"}
		}
		return Decision{Allowed: true, RequiresApproval: true, RiskLevel: "medium", Reason: "file writes require approval"}
	}

	return Decision{Allowed: true, RiskLevel: "low", Reason: "operation allowed"}
}

func (s *Service) ResolveWorkspacePath(workspaceRoot, target string) (string, error) {
	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", err
	}
	joined := filepath.Join(root, target)
	resolved, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	if samePath(root, resolved) {
		return resolved, nil
	}
	prefix := root + string(os.PathSeparator)
	if !strings.HasPrefix(resolved, prefix) {
		return "", fmt.Errorf("path escapes workspace")
	}
	return resolved, nil
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func looksDangerous(command string) bool {
	lower := strings.ToLower(command)
	keywords := []string{" rm ", "del ", "rmdir", "format ", "shutdown", "reboot", "diskpart", "mkfs", "reg delete", "sc delete"}
	padded := " " + lower + " "
	for _, keyword := range keywords {
		if strings.Contains(padded, keyword) {
			return true
		}
	}
	return false
}
