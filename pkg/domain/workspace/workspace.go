package workspace

type Workspace struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	OrgID         string   `json:"org_id"`
	ProjectID     string   `json:"project_id"`
	LocalPath     string   `json:"local_path"`
	AllowedTools  []string `json:"allowed_tools"`
	SandboxPolicy string   `json:"sandbox_policy"`
	RetentionRule string   `json:"retention_rule"`
}
