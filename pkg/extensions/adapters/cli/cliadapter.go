package cliadapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	ce "github.com/1024XEngineer/anyclaw/pkg/extensions/adapters/cli/exec"
	cr "github.com/1024XEngineer/anyclaw/pkg/extensions/adapters/cli/registry"
)

var defaultRegistry *cr.Registry
var defaultExecutor *ce.Executor

func Init(root string) error {
	reg, err := cr.NewRegistry(root)
	if err != nil {
		return err
	}

	defaultRegistry = reg
	defaultExecutor = ce.NewExecutor(reg)

	registerBuiltInHandlers()

	return nil
}

func InitFromEnv() error {
	roots := []string{
		os.Getenv("ANYCLAW_CLIADAPTER_ROOT"),
		"CLI-Anything-0.2.0",
		"../CLI-Anything-0.2.0",
		"../../CLI-Anything-0.2.0",
	}

	for _, root := range roots {
		if root == "" {
			continue
		}

		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}

		if _, err := os.Stat(filepath.Join(absRoot, "registry.json")); err == nil {
			return Init(absRoot)
		}
	}

	return nil
}

func GetRegistry() *cr.Registry {
	return defaultRegistry
}

func GetExecutor() *ce.Executor {
	return defaultExecutor
}

type SearchResult struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Installed   bool   `json:"installed"`
}

func Search(query, category string, limit int) ([]SearchResult, error) {
	if defaultExecutor == nil {
		return nil, nil
	}

	entries := defaultExecutor.Search(query, category, limit)
	results := make([]SearchResult, 0, len(entries))

	for _, e := range entries {
		results = append(results, SearchResult{
			Name:        e.Name,
			DisplayName: e.DisplayName,
			Category:    e.Category,
			Description: e.Description,
			Installed:   e.Installed,
		})

		if limit > 0 && len(results) >= limit {
			break
		}
	}

	return results, nil
}

func Exec(ctx context.Context, name string, args []string) (string, error) {
	if defaultExecutor == nil {
		return "", nil
	}

	result := defaultExecutor.Exec(ctx, name, args)
	if result.Error != "" {
		return result.Output, errors.New(result.Error)
	}
	return result.Output, nil
}

func ListCategories() map[string]int {
	if defaultExecutor == nil {
		return nil
	}
	return defaultExecutor.Categories()
}

func registerBuiltInHandlers() {
	if defaultExecutor == nil {
		return
	}

	registerBuiltinHandler("echo", "Echo CLI adapter arguments", "utility", func(ctx context.Context, args []string) (string, error) {
		return strings.Join(args, " "), nil
	})

	registerBuiltinHandler("date", "Return the current adapter date", "utility", func(ctx context.Context, args []string) (string, error) {
		return "2026-04-03", nil
	})

	registerBuiltinHandler("pwd", "Return the current working directory", "utility", func(ctx context.Context, args []string) (string, error) {
		dir, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return dir, nil
	})

	// Concrete CLI adapter handlers are registered by follow-up adapter packages.
}

func registerBuiltinHandler(name, desc, category string, handler ce.CommandHandler) {
	defaultExecutor.RegisterHandler(name, handler)
	cr.RegisterBuiltinAdapter(name, desc, category, func(args []string) (string, error) {
		return handler(context.Background(), args)
	})
}

func DiscoverRoot(start string) (string, bool) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}

	for {
		for _, candidate := range cliRootCandidates(current) {
			if _, err := os.Stat(filepath.Join(candidate, "registry.json")); err == nil {
				return candidate, true
			}
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	return "", false
}

func cliRootCandidates(base string) []string {
	return []string{
		base,
		filepath.Join(base, "CLI-Anything-0.2.0"),
		filepath.Join(base, "CLI-Anything"),
	}
}
