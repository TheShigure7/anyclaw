package runtimecore

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/anyclaw/anyclaw/pkg/domain/task"
	"github.com/anyclaw/anyclaw/pkg/security"
	"github.com/anyclaw/anyclaw/pkg/tools"
)

type Plan struct {
	Summary    string
	PlanSteps  []string
	Operations []task.Operation
}

type Engine struct {
	registry *tools.Registry
}

func NewEngine() *Engine {
	return &Engine{registry: tools.NewRegistry()}
}

func (e *Engine) BuildPlan(goal string, requested []task.Operation) Plan {
	if len(requested) > 0 {
		steps := make([]string, 0, len(requested))
		for _, op := range requested {
			steps = append(steps, fmt.Sprintf("Use %s", op.Tool))
		}
		return Plan{Summary: goal, PlanSteps: steps, Operations: requested}
	}

	summary := strings.TrimSpace(goal)
	if summary == "" {
		summary = "No goal provided"
	}
	return Plan{
		Summary:   summary,
		PlanSteps: []string{"Inspect workspace root"},
		Operations: []task.Operation{{
			Tool:   "list_dir",
			Params: map[string]string{"path": "."},
		}},
	}
}

func (e *Engine) ExecuteOperation(op task.Operation, workspaceRoot string, securityService *security.Service) (string, error) {
	if _, ok := e.registry.Get(op.Tool); !ok {
		return "", fmt.Errorf("unsupported tool %q", op.Tool)
	}

	switch op.Tool {
	case "list_dir":
		resolved, err := securityService.ResolveWorkspacePath(workspaceRoot, op.Params["path"])
		if err != nil {
			return "", err
		}
		entries, err := os.ReadDir(resolved)
		if err != nil {
			return "", err
		}
		if len(entries) == 0 {
			return "(empty directory)", nil
		}
		parts := make([]string, 0, len(entries))
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() {
				name += "/"
			}
			parts = append(parts, name)
		}
		return strings.Join(parts, "\n"), nil
	case "read_file":
		resolved, err := securityService.ResolveWorkspacePath(workspaceRoot, op.Params["path"])
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return "", err
		}
		return string(data), nil
	case "write_file":
		resolved, err := securityService.ResolveWorkspacePath(workspaceRoot, op.Params["path"])
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
			return "", err
		}
		content := op.Params["content"]
		if err := os.WriteFile(resolved, []byte(content), 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("wrote %d bytes to %s", len(content), op.Params["path"]), nil
	case "run_command":
		commandText := op.Params["command"]
		if strings.TrimSpace(commandText) == "" {
			return "", fmt.Errorf("command is required")
		}
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", commandText)
		} else {
			cmd = exec.Command("sh", "-c", commandText)
		}
		cmd.Dir = workspaceRoot
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		output := strings.TrimSpace(stdout.String())
		errOutput := strings.TrimSpace(stderr.String())
		if errOutput != "" {
			if output != "" {
				output += "\n"
			}
			output += errOutput
		}
		if err != nil {
			if output == "" {
				output = err.Error()
			}
			return output, err
		}
		if output == "" {
			output = "command completed with no output"
		}
		return output, nil
	default:
		return "", fmt.Errorf("unsupported tool %q", op.Tool)
	}
}
