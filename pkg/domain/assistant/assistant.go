package assistant

type Assistant struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Role              string   `json:"role"`
	Persona           string   `json:"persona"`
	DefaultModel      string   `json:"default_model"`
	EnabledSkills     []string `json:"enabled_skills"`
	PermissionProfile string   `json:"permission_profile"`
	WorkspaceID       string   `json:"workspace_id"`
	Status            string   `json:"status"`
}
