package memory

type Item struct {
	ID          string            `json:"id"`
	AssistantID string            `json:"assistant_id"`
	WorkspaceID string            `json:"workspace_id"`
	Kind        string            `json:"kind"`
	Content     string            `json:"content"`
	Metadata    map[string]string `json:"metadata"`
}
