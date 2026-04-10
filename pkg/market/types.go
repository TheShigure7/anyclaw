package market

import "github.com/anyclaw/anyclaw/pkg/config"

type PackageKind string

const (
	KindAgent PackageKind = "agent"
	KindSkill PackageKind = "skill"
	KindCLI   PackageKind = "cli"
)

type PackageManifest struct {
	ID           string            `json:"id"`
	Kind         PackageKind       `json:"kind"`
	Name         string            `json:"name"`
	DisplayName  string            `json:"display_name"`
	Version      string            `json:"version"`
	Description  string            `json:"description,omitempty"`
	Author       string            `json:"author,omitempty"`
	Homepage     string            `json:"homepage,omitempty"`
	Tags         []string          `json:"tags,omitempty"`
	Dependencies []PackageRef      `json:"dependencies,omitempty"`
	Agent        *AgentSpec        `json:"agent,omitempty"`
	Skill        *SkillSpec        `json:"skill,omitempty"`
	CLI          *CLISpec          `json:"cli,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type PackageRef struct {
	ID      string      `json:"id"`
	Kind    PackageKind `json:"kind"`
	Version string      `json:"version,omitempty"`
}

type AgentSpec struct {
	Mode              string                 `json:"mode"`
	ManagedByMain     bool                   `json:"managed_by_main"`
	ModelMode         string                 `json:"model_mode,omitempty"`
	Visibility        string                 `json:"visibility,omitempty"`
	Persona           string                 `json:"persona,omitempty"`
	Domain            string                 `json:"domain,omitempty"`
	Expertise         []string               `json:"expertise,omitempty"`
	SystemPrompt      string                 `json:"system_prompt,omitempty"`
	RequiredSkills    []string               `json:"required_skills,omitempty"`
	RequiredCLIs      []string               `json:"required_clis,omitempty"`
	PermissionProfile string                 `json:"permission_profile,omitempty"`
	WorkingDir        string                 `json:"working_dir,omitempty"`
	AvatarPreset      string                 `json:"avatar_preset,omitempty"`
	AvatarDataURL     string                 `json:"avatar_data_url,omitempty"`
	Personality       config.PersonalitySpec `json:"personality,omitempty"`
}

type SkillSpec struct {
	Name       string `json:"name,omitempty"`
	Source     string `json:"source,omitempty"`
	Entrypoint string `json:"entrypoint,omitempty"`
}

type CLISpec struct {
	Name        string   `json:"name,omitempty"`
	Entrypoint  string   `json:"entrypoint,omitempty"`
	InstallHint string   `json:"install_hint,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type InstalledPackage struct {
	Manifest    PackageManifest `json:"manifest"`
	InstalledAt string          `json:"installed_at"`
	Source      string          `json:"source,omitempty"`
}

type InstalledState struct {
	Packages []InstalledPackage `json:"packages"`
}

type ListFilter struct {
	Kind    PackageKind `json:"kind,omitempty"`
	Keyword string      `json:"keyword,omitempty"`
}

type InstallReceipt struct {
	PackageID    string         `json:"package_id"`
	Kind         PackageKind    `json:"kind"`
	Version      string         `json:"version,omitempty"`
	InstalledAt  string         `json:"installed_at"`
	Source       string         `json:"source,omitempty"`
	ManifestPath string         `json:"manifest_path,omitempty"`
	PackageRoot  string         `json:"package_root,omitempty"`
	Manifest     PackageManifest `json:"manifest"`
}

type InstallHistoryRecord struct {
	Action    string      `json:"action"`
	PackageID string      `json:"package_id"`
	Kind      PackageKind `json:"kind"`
	Version   string      `json:"version,omitempty"`
	At        string      `json:"at"`
	Source    string      `json:"source,omitempty"`
	Status    string      `json:"status"`
	Error     string      `json:"error,omitempty"`
}
