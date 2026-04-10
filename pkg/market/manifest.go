package market

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/anyclaw/anyclaw/pkg/config"
)

func LoadManifestFile(path string) (PackageManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PackageManifest{}, err
	}
	var manifest PackageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return PackageManifest{}, err
	}
	return manifest, ValidateManifest(manifest)
}

func ValidateManifest(manifest PackageManifest) error {
	if strings.TrimSpace(manifest.ID) == "" {
		return fmt.Errorf("manifest id is required")
	}
	switch manifest.Kind {
	case KindAgent, KindSkill, KindCLI:
	default:
		return fmt.Errorf("manifest kind must be one of: agent, skill, cli")
	}
	switch manifest.Kind {
	case KindAgent:
		if manifest.Agent == nil {
			return fmt.Errorf("agent manifest requires agent spec")
		}
		if mode := strings.TrimSpace(strings.ToLower(manifest.Agent.Mode)); mode != "persistent_subagent" {
			return fmt.Errorf("agent manifest mode must be persistent_subagent for now")
		}
		if !manifest.Agent.ManagedByMain {
			return fmt.Errorf("agent manifest must set managed_by_main=true")
		}
	case KindSkill:
		if manifest.Skill == nil {
			return fmt.Errorf("skill manifest requires skill spec")
		}
	case KindCLI:
		if manifest.CLI == nil {
			return fmt.Errorf("cli manifest requires cli spec")
		}
	}
	return nil
}

func persistentSubagentProfileFromManifest(manifest PackageManifest) config.PersistentSubagentProfile {
	spec := manifest.Agent
	return config.PersistentSubagentProfile{
		ID:              strings.TrimSpace(manifest.ID),
		DisplayName:     firstNonEmpty(manifest.DisplayName, manifest.Name, manifest.ID),
		Description:     strings.TrimSpace(manifest.Description),
		ManagedByMain:   config.BoolPtr(true),
		ModelMode:       firstNonEmpty(spec.ModelMode, "inherit_main"),
		Visibility:      firstNonEmpty(spec.Visibility, "internal_visible"),
		Persona:         strings.TrimSpace(spec.Persona),
		Domain:          strings.TrimSpace(spec.Domain),
		Expertise:       append([]string(nil), spec.Expertise...),
		SystemPrompt:    strings.TrimSpace(spec.SystemPrompt),
		Personality:     spec.Personality,
		RequiredSkills:  append([]string(nil), spec.RequiredSkills...),
		RequiredCLIs:    append([]string(nil), spec.RequiredCLIs...),
		PermissionLevel: firstNonEmpty(spec.PermissionProfile, "limited"),
		WorkingDir:      strings.TrimSpace(spec.WorkingDir),
		AvatarPreset:    strings.TrimSpace(spec.AvatarPreset),
		AvatarDataURL:   strings.TrimSpace(spec.AvatarDataURL),
		Enabled:         config.BoolPtr(true),
	}
}
