package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/1024XEngineer/anyclaw/pkg/extensions/plugin"
)

const (
	defaultMaxLoopIterations = 1000
	defaultMaxNodeExecutions = 10000
)

// WorkflowActionRunner executes action nodes for a workflow graph.
//
// The executor owns graph traversal and state transitions. Integrations own the
// actual side effects behind action nodes by providing this runner.
type WorkflowActionRunner interface {
	RunAction(ctx context.Context, node Node, inputs map[string]any) (map[string]any, error)
}

// WorkflowActionRunnerFunc adapts a function into a WorkflowActionRunner.
type WorkflowActionRunnerFunc func(ctx context.Context, node Node, inputs map[string]any) (map[string]any, error)

// RunAction executes a workflow action node.
func (fn WorkflowActionRunnerFunc) RunAction(ctx context.Context, node Node, inputs map[string]any) (map[string]any, error) {
	return fn(ctx, node, inputs)
}

// WorkflowExecutor executes workflow graphs and records node state transitions.
type WorkflowExecutor struct {
	pluginRegistry    *plugin.Registry
	graphStore        GraphStore
	actionRunner      WorkflowActionRunner
	maxLoopIterations int
	maxNodeExecutions int
}

// NewWorkflowExecutor creates a workflow graph executor.
func NewWorkflowExecutor(pluginRegistry *plugin.Registry, graphStore GraphStore) *WorkflowExecutor {
	return &WorkflowExecutor{
		pluginRegistry:    pluginRegistry,
		graphStore:        graphStore,
		maxLoopIterations: defaultMaxLoopIterations,
		maxNodeExecutions: defaultMaxNodeExecutions,
	}
}

// SetActionRunner sets the side-effect runner used by action nodes.
func (e *WorkflowExecutor) SetActionRunner(runner WorkflowActionRunner) {
	if e == nil {
		return
	}
	e.actionRunner = runner
}

// PluginRegistry returns the plugin registry associated with this executor.
func (e *WorkflowExecutor) PluginRegistry() *plugin.Registry {
	if e == nil {
		return nil
	}
	return e.pluginRegistry
}

// ExecuteGraph executes a graph with a background context.
func (e *WorkflowExecutor) ExecuteGraph(graph *Graph, inputs map[string]any) (*ExecutionContext, error) {
	return e.ExecuteGraphContext(context.Background(), graph, inputs)
}

// ExecuteStoredGraph loads a graph by ID from the configured store and executes it.
func (e *WorkflowExecutor) ExecuteStoredGraph(ctx context.Context, graphID string, inputs map[string]any) (*ExecutionContext, error) {
	if e == nil || e.graphStore == nil {
		return nil, fmt.Errorf("workflow graph store is not configured")
	}
	graphID = strings.TrimSpace(graphID)
	if graphID == "" {
		return nil, fmt.Errorf("graph ID is required")
	}
	graph, err := e.graphStore.LoadGraph(graphID)
	if err != nil {
		return nil, fmt.Errorf("load graph %q: %w", graphID, err)
	}
	if graph == nil {
		return nil, fmt.Errorf("load graph %q: graph is nil", graphID)
	}
	return e.ExecuteGraphContext(ctx, graph, inputs)
}

// ExecuteGraphContext executes a graph and returns the final execution context.
func (e *WorkflowExecutor) ExecuteGraphContext(ctx context.Context, graph *Graph, inputs map[string]any) (exec *ExecutionContext, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := graph.Validate(); err != nil {
		return nil, fmt.Errorf("graph validation failed: %w", err)
	}
	exec = NewExecutionContext(graph.ID, inputs)
	for _, variable := range graph.Variables {
		if strings.TrimSpace(variable.Name) == "" {
			continue
		}
		exec.Variables[variable.Name] = cloneAny(variable.InitialValue)
	}

	run := &workflowExecutionRun{
		executor: e,
		graph:    graph,
		exec:     exec,
		active:   make(map[string]bool),
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			exec.Status = ExecutionFailed
			exec.Error = &ExecutionError{
				Code:    "panic",
				Message: fmt.Sprintf("workflow executor panic: %v", recovered),
			}
			err = fmt.Errorf("%s", exec.Error.Message)
		}
	}()

	for _, startNode := range graph.GetStartNodes() {
		if err := run.executeNode(ctx, startNode.ID); err != nil {
			if exec.Status != ExecutionFailed {
				exec.Status = ExecutionFailed
				exec.Error = &ExecutionError{
					Code:    "execution_failed",
					Message: err.Error(),
					NodeID:  startNode.ID,
				}
			}
			return exec, err
		}
	}
	if exec.Status != ExecutionFailed {
		exec.MarkExecutionCompleted(run.collectGraphOutputs())
	}
	return exec, nil
}

type workflowExecutionRun struct {
	executor   *WorkflowExecutor
	graph      *Graph
	exec       *ExecutionContext
	active     map[string]bool
	executions int
}

func (r *workflowExecutionRun) executeNode(ctx context.Context, nodeID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	node, ok := r.graph.GetNodeByID(nodeID)
	if !ok {
		return fmt.Errorf("node not found: %s", nodeID)
	}
	if state, ok := r.exec.NodeStates[node.ID]; ok {
		if state.Status == NodeCompleted || state.Status == NodeSkipped {
			return nil
		}
	}
	if node.Type == "join" && !r.joinParentsReady(node.ID) {
		return nil
	}
	if r.active[node.ID] {
		return fmt.Errorf("cycle detected while executing node: %s", node.ID)
	}
	r.executions++
	if r.maxNodeExecutions() > 0 && r.executions > r.maxNodeExecutions() {
		return fmt.Errorf("workflow exceeded %d node executions", r.maxNodeExecutions())
	}

	r.active[node.ID] = true
	defer delete(r.active, node.ID)

	inputs := r.exec.ResolveInputs(node, r.graph)
	maxAttempts := nodeMaxAttempts(node)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		r.exec.MarkNodeStarted(node.ID, inputs)
		nodeCtx, cancel := nodeExecutionContext(ctx, node)
		outputs, err := r.executeNodeBody(nodeCtx, node, inputs)
		cancel()
		if err == nil {
			outputs = cloneAnyMap(outputs)
			r.applyNodeOutputMappings(node, outputs)
			r.exec.MarkNodeCompleted(node.ID, outputs)
			return r.executeNextNodes(ctx, node, outputs)
		}

		state := r.exec.NodeStates[node.ID]
		if state != nil && state.Attempts < maxAttempts {
			failedAttempts := state.Attempts
			r.exec.MarkNodeRetrying(node.ID)
			if err := sleepWithContext(ctx, retryDelay(node.RetryPolicy, failedAttempts)); err != nil {
				return err
			}
			continue
		}
		return r.handleNodeError(ctx, node, err)
	}
}

func (r *workflowExecutionRun) executeNodeBody(ctx context.Context, node *Node, inputs map[string]any) (map[string]any, error) {
	switch node.Type {
	case "action":
		return r.executeActionNode(ctx, node, inputs)
	case "condition":
		return r.executeConditionNode(node, inputs)
	case "loop":
		return r.executeLoopNode(node)
	case "parallel":
		return r.executeParallelNode(node)
	case "join":
		return r.executeJoinNode(node)
	default:
		return nil, fmt.Errorf("unsupported node type: %s", node.Type)
	}
}

func (r *workflowExecutionRun) executeActionNode(ctx context.Context, node *Node, inputs map[string]any) (map[string]any, error) {
	if strings.TrimSpace(node.Plugin) == "" {
		return nil, fmt.Errorf("plugin is required for action node: %s", node.ID)
	}
	if strings.TrimSpace(node.Action) == "" {
		return nil, fmt.Errorf("action is required for action node: %s", node.ID)
	}
	if r.executor == nil || r.executor.actionRunner == nil {
		return nil, fmt.Errorf("workflow action runner is not configured")
	}
	outputs, err := r.executor.actionRunner.RunAction(ctx, cloneNode(*node), cloneAnyMap(inputs))
	if err != nil {
		return nil, err
	}
	return cloneAnyMap(outputs), nil
}

func (r *workflowExecutionRun) executeConditionNode(node *Node, inputs map[string]any) (map[string]any, error) {
	vars := r.evaluationVars(inputs)
	result, err := EvalCondition(node.Condition, vars)
	if err != nil {
		return nil, fmt.Errorf("condition node %s: %w", node.ID, err)
	}
	return map[string]any{"result": result}, nil
}

func (r *workflowExecutionRun) executeLoopNode(node *Node) (map[string]any, error) {
	value := r.resolveLoopValue(node.LoopOver)
	items, err := loopItems(value)
	if err != nil {
		return nil, fmt.Errorf("loop node %s: %w", node.ID, err)
	}
	limit := r.maxLoopIterations()
	if limit > 0 && len(items) > limit {
		return nil, fmt.Errorf("loop node %s exceeds %d iterations", node.ID, limit)
	}
	return map[string]any{
		"iterations": len(items),
		"items":      cloneAny(items),
	}, nil
}

func (r *workflowExecutionRun) executeParallelNode(node *Node) (map[string]any, error) {
	branches := r.nextNodeIDsForTypes(node.ID, map[string]bool{
		"branch":  true,
		"default": true,
		"success": true,
		"":        true,
	})
	return map[string]any{
		"branch_count": len(branches),
		"branches":     branches,
	}, nil
}

func (r *workflowExecutionRun) executeJoinNode(node *Node) (map[string]any, error) {
	parents := r.graph.GetPreviousNodes(node.ID)
	completed := 0
	results := make(map[string]any)
	for _, parentID := range parents {
		state := r.exec.NodeStates[parentID]
		if state == nil || (state.Status != NodeCompleted && state.Status != NodeSkipped) {
			continue
		}
		completed++
		results[parentID] = cloneAnyMap(state.Outputs)
	}
	return map[string]any{
		"completed_count": completed,
		"total_parents":   len(parents),
		"results":         results,
	}, nil
}

func (r *workflowExecutionRun) executeNextNodes(ctx context.Context, node *Node, outputs map[string]any) error {
	nextIDs, err := r.nextNodeIDs(node, outputs)
	if err != nil {
		return err
	}
	for _, nextID := range nextIDs {
		if err := r.executeNode(ctx, nextID); err != nil {
			return err
		}
	}
	return nil
}

func (r *workflowExecutionRun) nextNodeIDs(node *Node, outputs map[string]any) ([]string, error) {
	next := make([]string, 0)
	for _, edge := range r.graph.Edges {
		if edge.Source != node.ID {
			continue
		}
		follow, err := r.shouldFollowEdge(edge, outputs)
		if err != nil {
			return nil, err
		}
		if follow {
			next = append(next, edge.Target)
		}
	}
	return next, nil
}

func (r *workflowExecutionRun) shouldFollowEdge(edge Edge, outputs map[string]any) (bool, error) {
	switch normalizeEdgeType(edge.Type) {
	case "default", "success", "branch":
		return true, nil
	case "failure":
		return false, nil
	case "condition", "condition_true", "true":
		if strings.TrimSpace(edge.Condition) != "" {
			ok, err := EvalCondition(edge.Condition, r.evaluationVars(outputs))
			if err != nil {
				return false, fmt.Errorf("edge %s condition: %w", edge.ID, err)
			}
			return ok, nil
		}
		return toBool(outputs["result"]), nil
	case "condition_false", "false":
		if strings.TrimSpace(edge.Condition) != "" {
			ok, err := EvalCondition(edge.Condition, r.evaluationVars(outputs))
			if err != nil {
				return false, fmt.Errorf("edge %s condition: %w", edge.ID, err)
			}
			return !ok, nil
		}
		return !toBool(outputs["result"]), nil
	default:
		return true, nil
	}
}

func (r *workflowExecutionRun) handleNodeError(ctx context.Context, node *Node, err error) error {
	if node.ErrorHandling != nil {
		switch strings.TrimSpace(node.ErrorHandling.OnError) {
		case "skip":
			r.markNodeSkipped(node.ID, err)
			return r.executeNextNodes(ctx, node, map[string]any{
				"skipped": true,
				"error":   err.Error(),
			})
		case "goto":
			if target := strings.TrimSpace(node.ErrorHandling.TargetNode); target != "" {
				r.markNodeSkipped(node.ID, err)
				return r.executeNode(ctx, target)
			}
		}
	}
	nodeErr := &NodeError{
		Code:      "execution_failed",
		Message:   err.Error(),
		Retryable: nodeMaxAttempts(node) > 1,
	}
	r.exec.MarkNodeFailed(node.ID, nodeErr)
	return err
}

func (r *workflowExecutionRun) applyNodeOutputMappings(node *Node, outputs map[string]any) {
	for outputName, variableName := range node.Outputs {
		variableName = strings.TrimSpace(strings.TrimPrefix(variableName, "$"))
		if variableName == "" {
			continue
		}
		if value, ok := outputs[outputName]; ok {
			r.exec.Variables[variableName] = cloneAny(value)
		}
	}
}

func (r *workflowExecutionRun) collectGraphOutputs() map[string]any {
	if len(r.graph.Outputs) == 0 {
		return cloneAnyMap(r.exec.Outputs)
	}
	outputs := make(map[string]any, len(r.graph.Outputs))
	for _, param := range r.graph.Outputs {
		name := strings.TrimSpace(param.Name)
		if name == "" || strings.TrimSpace(param.Source) == "" {
			continue
		}
		outputs[name] = cloneAny(r.exec.resolveValue(param.Source, r.graph))
	}
	return outputs
}

func (r *workflowExecutionRun) evaluationVars(overlays ...map[string]any) map[string]any {
	vars := cloneAnyMap(r.exec.Inputs)
	if vars == nil {
		vars = make(map[string]any)
	}
	for key, value := range r.exec.Variables {
		vars[key] = cloneAny(value)
	}
	for nodeID, state := range r.exec.NodeStates {
		if state == nil || state.Outputs == nil {
			continue
		}
		vars["_node_outputs:"+nodeID] = cloneAnyMap(state.Outputs)
	}
	for _, overlay := range overlays {
		for key, value := range overlay {
			vars[key] = cloneAny(value)
		}
	}
	return vars
}

func (r *workflowExecutionRun) joinParentsReady(nodeID string) bool {
	for _, parentID := range r.graph.GetPreviousNodes(nodeID) {
		state := r.exec.NodeStates[parentID]
		if state == nil || (state.Status != NodeCompleted && state.Status != NodeSkipped) {
			return false
		}
	}
	return true
}

func (r *workflowExecutionRun) nextNodeIDsForTypes(nodeID string, allowed map[string]bool) []string {
	next := make([]string, 0)
	for _, edge := range r.graph.Edges {
		if edge.Source != nodeID {
			continue
		}
		if allowed[normalizeEdgeType(edge.Type)] {
			next = append(next, edge.Target)
		}
	}
	return next
}

func (r *workflowExecutionRun) markNodeSkipped(nodeID string, err error) {
	now := time.Now().UTC()
	state, ok := r.exec.NodeStates[nodeID]
	if !ok {
		state = &NodeState{NodeID: nodeID}
		r.exec.NodeStates[nodeID] = state
	}
	state.Status = NodeSkipped
	state.EndTime = &now
	state.Error = &NodeError{
		Code:    "skipped",
		Message: err.Error(),
	}
}

func (r *workflowExecutionRun) resolveLoopValue(expr string) any {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil
	}
	if strings.HasPrefix(expr, "$") {
		return r.exec.resolveValue(expr, r.graph)
	}
	if value, ok := r.exec.Variables[expr]; ok {
		return value
	}
	if value, ok := r.exec.Inputs[expr]; ok {
		return value
	}
	return expr
}

func (r *workflowExecutionRun) maxLoopIterations() int {
	if r.executor == nil || r.executor.maxLoopIterations <= 0 {
		return defaultMaxLoopIterations
	}
	return r.executor.maxLoopIterations
}

func (r *workflowExecutionRun) maxNodeExecutions() int {
	if r.executor == nil || r.executor.maxNodeExecutions <= 0 {
		return defaultMaxNodeExecutions
	}
	return r.executor.maxNodeExecutions
}

func nodeMaxAttempts(node *Node) int {
	if node == nil || node.RetryPolicy == nil || node.RetryPolicy.MaxAttempts <= 0 {
		return 1
	}
	return node.RetryPolicy.MaxAttempts
}

func nodeExecutionContext(ctx context.Context, node *Node) (context.Context, context.CancelFunc) {
	if node == nil || node.TimeoutSec <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Duration(node.TimeoutSec)*time.Second)
}

func retryDelay(policy *RetryPolicy, attempts int) time.Duration {
	if policy == nil || policy.InitialDelay <= 0 {
		return 0
	}
	delay := float64(policy.InitialDelay)
	factor := policy.BackoffFactor
	if factor <= 0 {
		factor = 1
	}
	for i := 1; i < attempts; i++ {
		delay *= factor
	}
	if policy.MaxDelay > 0 {
		delay = math.Min(delay, float64(policy.MaxDelay))
	}
	return time.Duration(delay) * time.Millisecond
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func loopItems(value any) ([]any, error) {
	switch v := value.(type) {
	case nil:
		return []any{}, nil
	case []any:
		return cloneAny(v).([]any), nil
	case []string:
		items := make([]any, len(v))
		for i, item := range v {
			items[i] = item
		}
		return items, nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return []any{}, nil
		}
		if strings.HasPrefix(trimmed, "[") {
			var items []any
			if err := json.Unmarshal([]byte(trimmed), &items); err != nil {
				return nil, fmt.Errorf("parse loop_over JSON array: %w", err)
			}
			return items, nil
		}
		return []any{v}, nil
	default:
		return []any{cloneAny(v)}, nil
	}
}
