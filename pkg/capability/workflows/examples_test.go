package workflow

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

func TestBuiltinWorkflowExamplesValidateAndRoundTrip(t *testing.T) {
	examples := BuiltinWorkflowExamples()
	if len(examples) != 3 {
		t.Fatalf("examples = %d, want 3", len(examples))
	}

	seen := make(map[string]bool, len(examples))
	for _, example := range examples {
		if example.ID == "" || example.Name == "" || example.Description == "" {
			t.Fatalf("example metadata incomplete: %+v", example)
		}
		if example.Graph == nil {
			t.Fatalf("example %q has nil graph", example.ID)
		}
		if example.ID != example.Graph.ID {
			t.Fatalf("example ID = %q, graph ID = %q", example.ID, example.Graph.ID)
		}
		if seen[example.ID] {
			t.Fatalf("duplicate example ID %q", example.ID)
		}
		seen[example.ID] = true

		if err := example.Graph.Validate(); err != nil {
			t.Fatalf("example %q Validate: %v", example.ID, err)
		}
		data, err := example.Graph.ToJSON()
		if err != nil {
			t.Fatalf("example %q ToJSON: %v", example.ID, err)
		}
		if !json.Valid(data) {
			t.Fatalf("example %q produced invalid JSON: %s", example.ID, string(data))
		}
		loaded, err := FromJSON(data)
		if err != nil {
			t.Fatalf("example %q FromJSON: %v", example.ID, err)
		}
		if err := loaded.Validate(); err != nil {
			t.Fatalf("example %q round-trip Validate: %v", example.ID, err)
		}
	}
}

func TestBuiltinWorkflowExamplesReturnFreshGraphs(t *testing.T) {
	first := BuiltinWorkflowExamples()
	first[0].Name = "mutated metadata"
	first[0].Tags[0] = "mutated tag"
	first[0].Graph.Name = "mutated graph"
	first[0].Graph.Tags[0] = "mutated graph tag"

	second := BuiltinWorkflowExamples()
	if second[0].Name == "mutated metadata" || second[0].Graph.Name == "mutated graph" {
		t.Fatal("BuiltinWorkflowExamples leaked mutable graph state across calls")
	}
	if second[0].Tags[0] == "mutated tag" || second[0].Graph.Tags[0] == "mutated graph tag" {
		t.Fatal("BuiltinWorkflowExamples leaked mutable tag state across calls")
	}
}

func TestWorkflowExampleReferencesPointAtKnownNodes(t *testing.T) {
	for _, example := range BuiltinWorkflowExamples() {
		nodeIDs := make(map[string]bool, len(example.Graph.Nodes))
		for _, node := range example.Graph.Nodes {
			nodeIDs[node.ID] = true
		}

		for _, node := range example.Graph.Nodes {
			for _, ref := range collectSimpleRefs(node.Inputs) {
				nodeID := strings.TrimPrefix(strings.SplitN(ref, ".", 2)[0], "$")
				if !nodeIDs[nodeID] {
					t.Fatalf("example %q node %q references unknown node %q in %q", example.ID, node.ID, nodeID, ref)
				}
			}
		}
		for _, output := range example.Graph.Outputs {
			for _, ref := range collectSimpleRefs(output.Source) {
				nodeID := strings.TrimPrefix(strings.SplitN(ref, ".", 2)[0], "$")
				if !nodeIDs[nodeID] {
					t.Fatalf("example %q output %q references unknown node %q in %q", example.ID, output.Name, nodeID, ref)
				}
			}
		}
	}
}

func TestWorkflowExamplesUseExamplePluginNamespace(t *testing.T) {
	for _, example := range BuiltinWorkflowExamples() {
		for _, node := range example.Graph.Nodes {
			if node.Type != "action" {
				continue
			}
			if !strings.HasPrefix(node.Plugin, "example.") {
				t.Fatalf("example %q action node %q plugin = %q, want example.* namespace", example.ID, node.ID, node.Plugin)
			}
		}
	}
}

func TestApprovalGateExampleDoesNotMergeExclusiveOutputs(t *testing.T) {
	graph := ApprovalGateExampleGraph()
	for _, node := range graph.Nodes {
		refs := collectSimpleRefs(node.Inputs)
		if containsRef(refs, "$manual_approval.approved") && containsRef(refs, "$auto_approve.approved") {
			t.Fatalf("node %q reads both exclusive approval branch outputs: %v", node.ID, refs)
		}
	}
}

var simpleNodeReferencePattern = regexp.MustCompile(`^\$[A-Za-z0-9_-]+\.[A-Za-z0-9_.-]+$`)

func collectSimpleRefs(value any) []string {
	switch v := value.(type) {
	case string:
		if simpleNodeReferencePattern.MatchString(v) {
			return []string{v}
		}
	case map[string]any:
		var refs []string
		for _, nested := range v {
			refs = append(refs, collectSimpleRefs(nested)...)
		}
		return refs
	case []any:
		var refs []string
		for _, nested := range v {
			refs = append(refs, collectSimpleRefs(nested)...)
		}
		return refs
	}
	return nil
}

func containsRef(refs []string, want string) bool {
	for _, ref := range refs {
		if ref == want {
			return true
		}
	}
	return false
}
