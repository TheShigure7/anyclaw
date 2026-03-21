package tools

type Capability struct {
	Name        string
	Description string
	RiskLevel   string
}

type Registry struct {
	Capabilities []Capability
}

func NewRegistry() *Registry {
	return &Registry{Capabilities: []Capability{
		{Name: "list_dir", Description: "List files in a directory", RiskLevel: "low"},
		{Name: "read_file", Description: "Read a file from workspace", RiskLevel: "low"},
		{Name: "write_file", Description: "Write a file in workspace", RiskLevel: "medium"},
		{Name: "run_command", Description: "Run a command in workspace", RiskLevel: "high"},
	}}
}

func (r *Registry) Get(name string) (Capability, bool) {
	for _, capability := range r.Capabilities {
		if capability.Name == name {
			return capability, true
		}
	}
	return Capability{}, false
}
