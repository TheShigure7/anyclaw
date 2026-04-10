package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/anyclaw/anyclaw/pkg/config"
	"github.com/anyclaw/anyclaw/pkg/market"
	"github.com/anyclaw/anyclaw/pkg/ui"
)

func runPackagesCommand(args []string) error {
	if len(args) == 0 {
		printPackagesUsage()
		return nil
	}
	switch args[0] {
	case "list", "installed":
		return runPackagesList(args[1:])
	case "info":
		return runPackagesInfo(args[1:])
	case "install-file":
		return runPackagesInstallFile(args[1:])
	case "uninstall":
		return runPackagesUninstall(args[1:])
	default:
		printPackagesUsage()
		return fmt.Errorf("unknown packages command: %s", args[0])
	}
}

func runMarketCommand(args []string) error {
	return runPackagesCommand(args)
}

func printPackagesUsage() {
	fmt.Print(`AnyClaw packages commands:

Usage:
  anyclaw packages list [--json]
  anyclaw packages info <id> [--json]
  anyclaw packages install-file <manifest.json>
  anyclaw packages uninstall <id>

Compatibility alias:
  anyclaw market ...
`)
}

func runPackagesList(args []string) error {
	fs := flag.NewFlagSet("packages list", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	store, err := loadPackageStore("anyclaw.json")
	if err != nil {
		return err
	}
	items, err := store.ListInstalled()
	if err != nil {
		return err
	}
	if *jsonOut {
		data, _ := json.MarshalIndent(map[string]any{"count": len(items), "packages": items}, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	if len(items) == 0 {
		fmt.Println("No packages installed.")
		return nil
	}
	fmt.Printf("%s\n\n", ui.Bold.Sprint(fmt.Sprintf("Installed packages (%d)", len(items))))
	for _, item := range items {
		fmt.Printf("  - %s [%s] %s\n", item.Manifest.ID, item.Manifest.Kind, firstNonEmptyMarket(item.Manifest.DisplayName, item.Manifest.Name))
		if desc := strings.TrimSpace(item.Manifest.Description); desc != "" {
			fmt.Printf("    %s\n", ui.Dim.Sprint(desc))
		}
		fmt.Printf("    version: %s | installed: %s\n", firstNonEmptyMarket(item.Manifest.Version, "unknown"), item.InstalledAt)
	}
	return nil
}

func runPackagesInfo(args []string) error {
	fs := flag.NewFlagSet("packages info", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("usage: anyclaw packages info <id>")
	}
	id := fs.Arg(0)
	store, err := loadPackageStore("anyclaw.json")
	if err != nil {
		return err
	}
	item, ok, err := store.GetInstalled(id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("package not installed: %s", id)
	}
	if *jsonOut {
		data, _ := json.MarshalIndent(item, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	fmt.Printf("%s [%s]\n\n", ui.Bold.Sprint(item.Manifest.ID), item.Manifest.Kind)
	fmt.Printf("  name:        %s\n", firstNonEmptyMarket(item.Manifest.DisplayName, item.Manifest.Name))
	fmt.Printf("  version:     %s\n", firstNonEmptyMarket(item.Manifest.Version, "unknown"))
	fmt.Printf("  author:      %s\n", firstNonEmptyMarket(item.Manifest.Author, "unknown"))
	fmt.Printf("  installed:   %s\n", item.InstalledAt)
	fmt.Printf("  source:      %s\n", firstNonEmptyMarket(item.Source, "unknown"))
	if desc := strings.TrimSpace(item.Manifest.Description); desc != "" {
		fmt.Printf("  description: %s\n", desc)
	}
	if item.Manifest.Agent != nil {
		fmt.Printf("  agent mode:  %s\n", item.Manifest.Agent.Mode)
		fmt.Printf("  domain:      %s\n", item.Manifest.Agent.Domain)
	}
	return nil
}

func runPackagesInstallFile(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: anyclaw packages install-file <manifest.json>")
	}
	store, err := loadPackageStore("anyclaw.json")
	if err != nil {
		return err
	}
	manifest, err := store.InstallManifestFile(args[0])
	if err != nil {
		return err
	}
	printSuccess("Installed package: %s [%s]", manifest.ID, manifest.Kind)
	if manifest.Kind == market.KindAgent {
		printInfo("Restart AnyClaw or reload the runtime to activate the persistent subagent.")
	}
	return nil
}

func runPackagesUninstall(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: anyclaw packages uninstall <id>")
	}
	store, err := loadPackageStore("anyclaw.json")
	if err != nil {
		return err
	}
	if err := store.Uninstall(args[0]); err != nil {
		return err
	}
	printSuccess("Uninstalled package: %s", args[0])
	return nil
}

func loadPackageStore(configPath string) (*market.Store, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	workDir := cfg.Agent.WorkDir
	if resolved := config.ResolvePath(configPath, workDir); resolved != "" {
		workDir = resolved
	}
	return market.NewStore(workDir)
}

func firstNonEmptyMarket(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
