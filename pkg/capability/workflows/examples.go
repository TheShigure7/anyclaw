package workflow

// WorkflowExample packages an example graph with catalog metadata.
type WorkflowExample struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
	Graph       *Graph   `json:"graph"`
}

// BuiltinWorkflowExamples returns fresh example graphs for editor demos and docs.
//
// The examples use the "example.*" plugin namespace to make it clear that they
// are graph construction templates, not a claim that matching plugins exist.
func BuiltinWorkflowExamples() []WorkflowExample {
	graphs := []*Graph{
		FileReviewExampleGraph(),
		ApprovalGateExampleGraph(),
		ParallelResearchExampleGraph(),
	}

	examples := make([]WorkflowExample, 0, len(graphs))
	for _, graph := range graphs {
		examples = append(examples, WorkflowExample{
			ID:          graph.ID,
			Name:        graph.Name,
			Description: graph.Description,
			Tags:        append([]string(nil), graph.Tags...),
			Graph:       graph,
		})
	}
	return examples
}

// FileReviewExampleGraph builds a simple file processing graph with a review branch.
func FileReviewExampleGraph() *Graph {
	graph := NewGraph("File review workflow", "Read a file, summarize it, optionally request review, and write the summary.")
	graph.ID = "example_file_review"
	graph.Version = "1.0.0"
	graph.Author = "AnyClaw"
	graph.Tags = []string{"example", "files", "review"}

	graph.AddInputParam("source_file", "string", "File path to read.", true, nil)
	graph.AddInputParam("summary_file", "string", "File path to write the summary.", true, nil)
	graph.AddInputParam("require_review", "boolean", "Whether a human review is required.", false, true)
	graph.AddOutputParam("summary", "string", "Generated summary text.", "$summarize_file.summary")
	graph.AddOutputParam("written", "boolean", "Whether the summary was written.", "$write_summary.ok")

	readID := addExampleAction(graph, "read_source", "Read source file", "example.files", "read", map[string]any{
		"path": "$source_file",
	}, 0, 0)
	summarizeID := addExampleAction(graph, "summarize_file", "Summarize file", "example.text", "summarize", map[string]any{
		"text": "$read_source.content",
	}, 260, 0)
	decisionID := addExampleCondition(graph, "review_required", "Review required?", "$require_review == true", 520, 0)
	reviewID := addExampleAction(graph, "request_review", "Request review", "example.approvals", "request", map[string]any{
		"title":   "Review generated summary",
		"summary": "$summarize_file.summary",
	}, 780, -90)
	writeID := addExampleAction(graph, "write_summary", "Write summary", "example.files", "write", map[string]any{
		"path":    "$summary_file",
		"content": "$summarize_file.summary",
	}, 780, 90)

	graph.AddEdge(readID, summarizeID, "default")
	graph.AddEdge(summarizeID, decisionID, "default")
	graph.AddEdge(decisionID, reviewID, "condition_true")
	graph.AddEdge(decisionID, writeID, "condition_false")
	graph.AddEdge(reviewID, writeID, "default")

	return graph
}

// ApprovalGateExampleGraph builds a graph that gates execution on a score.
func ApprovalGateExampleGraph() *Graph {
	graph := NewGraph("Approval gate workflow", "Score an incoming request and route high-risk items to approval.")
	graph.ID = "example_approval_gate"
	graph.Version = "1.0.0"
	graph.Author = "AnyClaw"
	graph.Tags = []string{"example", "approval", "routing"}

	graph.AddInputParam("request", "object", "Incoming request payload.", true, nil)
	graph.AddInputParam("risk_threshold", "number", "Score threshold that requires approval.", false, 0.7)
	graph.AddOutputParam("manual_approved", "boolean", "Whether the manual branch approved the request.", "$finalize_manual.approved")
	graph.AddOutputParam("auto_approved", "boolean", "Whether the automatic branch approved the request.", "$finalize_auto.approved")

	scoreID := addExampleAction(graph, "score_request", "Score request", "example.policy", "score", map[string]any{
		"request": "$request",
	}, 0, 0)
	checkID := addExampleCondition(graph, "high_risk", "High risk?", "$score_request.score >= $risk_threshold", 260, 0)
	approvalID := addExampleAction(graph, "manual_approval", "Manual approval", "example.approvals", "request", map[string]any{
		"request": "$request",
		"score":   "$score_request.score",
	}, 520, -90)
	autoID := addExampleAction(graph, "auto_approve", "Auto approve", "example.policy", "approve", map[string]any{
		"request": "$request",
		"reason":  "below threshold",
	}, 520, 90)
	finalizeManualID := addExampleAction(graph, "finalize_manual", "Finalize manual approval", "example.policy", "finalize", map[string]any{
		"approved": "$manual_approval.approved",
		"path":     "manual",
	}, 780, -90)
	finalizeAutoID := addExampleAction(graph, "finalize_auto", "Finalize auto approval", "example.policy", "finalize", map[string]any{
		"approved": "$auto_approve.approved",
		"path":     "auto",
	}, 780, 90)

	graph.AddEdge(scoreID, checkID, "default")
	graph.AddEdge(checkID, approvalID, "condition_true")
	graph.AddEdge(checkID, autoID, "condition_false")
	graph.AddEdge(approvalID, finalizeManualID, "default")
	graph.AddEdge(autoID, finalizeAutoID, "default")

	return graph
}

// ParallelResearchExampleGraph builds a graph that fans out research tasks and joins them.
func ParallelResearchExampleGraph() *Graph {
	graph := NewGraph("Parallel research workflow", "Collect notes from multiple sources, combine them, and draft a brief.")
	graph.ID = "example_parallel_research"
	graph.Version = "1.0.0"
	graph.Author = "AnyClaw"
	graph.Tags = []string{"example", "research", "parallel"}

	graph.AddInputParam("topic", "string", "Research topic.", true, nil)
	graph.AddOutputParam("brief", "string", "Drafted research brief.", "$draft_brief.content")

	startID := addExampleAction(graph, "normalize_topic", "Normalize topic", "example.text", "normalize", map[string]any{
		"text": "$topic",
	}, 0, 0)
	fanoutID := addExampleNode(graph, Node{
		ID:       "fanout_sources",
		Type:     "parallel",
		Name:     "Fan out sources",
		Position: Position{X: 260, Y: 0},
	})
	docsID := addExampleAction(graph, "search_docs", "Search docs", "example.search", "docs", map[string]any{
		"query": "$normalize_topic.text",
	}, 520, -140)
	webID := addExampleAction(graph, "search_web", "Search web", "example.search", "web", map[string]any{
		"query": "$normalize_topic.text",
	}, 520, 0)
	notesID := addExampleAction(graph, "search_notes", "Search notes", "example.search", "notes", map[string]any{
		"query": "$normalize_topic.text",
	}, 520, 140)
	joinID := addExampleNode(graph, Node{
		ID:       "join_sources",
		Type:     "join",
		Name:     "Join sources",
		Position: Position{X: 780, Y: 0},
	})
	draftID := addExampleAction(graph, "draft_brief", "Draft brief", "example.text", "draft", map[string]any{
		"docs":  "$search_docs.results",
		"web":   "$search_web.results",
		"notes": "$search_notes.results",
	}, 1040, 0)

	graph.AddEdge(startID, fanoutID, "default")
	graph.AddEdge(fanoutID, docsID, "branch")
	graph.AddEdge(fanoutID, webID, "branch")
	graph.AddEdge(fanoutID, notesID, "branch")
	graph.AddEdge(docsID, joinID, "default")
	graph.AddEdge(webID, joinID, "default")
	graph.AddEdge(notesID, joinID, "default")
	graph.AddEdge(joinID, draftID, "default")

	return graph
}

func addExampleAction(graph *Graph, id, name, plugin, action string, inputs map[string]any, x, y float64) string {
	return addExampleNode(graph, Node{
		ID:       id,
		Type:     "action",
		Name:     name,
		Plugin:   plugin,
		Action:   action,
		Inputs:   inputs,
		Position: Position{X: x, Y: y},
	})
}

func addExampleCondition(graph *Graph, id, name, condition string, x, y float64) string {
	return addExampleNode(graph, Node{
		ID:        id,
		Type:      "condition",
		Name:      name,
		Condition: condition,
		Position:  Position{X: x, Y: y},
	})
}

func addExampleNode(graph *Graph, node Node) string {
	return graph.AddNode(node)
}
