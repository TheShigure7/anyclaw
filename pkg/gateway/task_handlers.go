package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	taskModule "github.com/anyclaw/anyclaw/pkg/task"
)

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !HasPermission(UserFromContext(r.Context()), "tasks.read") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden", "required_permission": "tasks.read"})
			return
		}
		items := s.store.ListTasks()
		workspace := strings.TrimSpace(r.URL.Query().Get("workspace"))
		status := strings.TrimSpace(r.URL.Query().Get("status"))
		filtered := make([]*Task, 0, len(items))
		for _, task := range items {
			if workspace != "" && task.Workspace != workspace {
				continue
			}
			if status != "" && !strings.EqualFold(task.Status, status) {
				continue
			}
			filtered = append(filtered, task)
		}
		s.appendAudit(UserFromContext(r.Context()), "tasks.read", "tasks", map[string]any{"count": len(filtered)})
		writeJSON(w, http.StatusOK, filtered)
	case http.MethodPost:
		if !HasPermission(UserFromContext(r.Context()), "tasks.write") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden", "required_permission": "tasks.write"})
			return
		}
		var req struct {
			Title     string `json:"title"`
			Input     string `json:"input"`
			Agent     string `json:"agent"`
			Assistant string `json:"assistant"`
			SessionID string `json:"session_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if strings.TrimSpace(req.Input) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "input is required"})
			return
		}
		assistantName, err := s.resolveAgentName(requestedAgentName(req.Agent, req.Assistant))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		var orgID, projectID, workspaceID string
		if strings.TrimSpace(req.SessionID) != "" {
			session, ok := s.sessions.Get(strings.TrimSpace(req.SessionID))
			if !ok {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
				return
			}
			orgID, projectID, workspaceID = session.Org, session.Project, session.Workspace
		} else {
			queryOrg, queryProject, queryWorkspace := s.resolveHierarchyFromQuery(r)
			org, project, workspace, err := s.validateResourceSelection(queryOrg, queryProject, queryWorkspace)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			orgID, projectID, workspaceID = org.ID, project.ID, workspace.ID
		}
		task, err := s.tasks.Create(TaskCreateOptions{
			Title:     req.Title,
			Input:     req.Input,
			Assistant: assistantName,
			Org:       orgID,
			Project:   projectID,
			Workspace: workspaceID,
			SessionID: req.SessionID,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		result, err := s.tasks.Execute(r.Context(), task.ID)
		if err != nil {
			if errors.Is(err, ErrTaskWaitingApproval) {
				s.appendAudit(UserFromContext(r.Context()), "tasks.write", task.ID, map[string]any{"status": "waiting_approval"})
				response := s.taskResponse(result.Task, result.Session)
				response["status"] = "waiting_approval"
				writeJSON(w, http.StatusAccepted, response)
				return
			}
			s.appendAudit(UserFromContext(r.Context()), "tasks.write", task.ID, map[string]any{"status": "failed"})
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "task": task})
			return
		}
		s.recordTaskCompletion(result, "task_api")
		s.appendAudit(UserFromContext(r.Context()), "tasks.write", task.ID, map[string]any{"status": result.Task.Status})
		writeJSON(w, http.StatusCreated, s.taskResponse(result.Task, result.Session))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/tasks/")
	path = strings.TrimSpace(path)
	if path == "" {
		http.Error(w, "task id required", http.StatusBadRequest)
		return
	}
	parts := strings.Split(path, "/")
	taskID := strings.TrimSpace(parts[0])
	task, ok := s.tasks.Get(taskID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	if len(parts) > 1 && parts[1] == "steps" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !HasPermission(UserFromContext(r.Context()), "tasks.read") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden", "required_permission": "tasks.read"})
			return
		}
		writeJSON(w, http.StatusOK, s.tasks.Steps(taskID))
		return
	}
	if len(parts) > 1 && parts[1] == "execute" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !HasPermission(UserFromContext(r.Context()), "tasks.write") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden", "required_permission": "tasks.write"})
			return
		}
		result, err := s.tasks.Execute(r.Context(), taskID)
		if err != nil {
			if errors.Is(err, ErrTaskWaitingApproval) {
				s.appendAudit(UserFromContext(r.Context()), "tasks.write", taskID, map[string]any{"status": "waiting_approval", "resume": true})
				response := s.taskResponse(result.Task, result.Session)
				response["status"] = "waiting_approval"
				writeJSON(w, http.StatusAccepted, response)
				return
			}
			s.appendAudit(UserFromContext(r.Context()), "tasks.write", taskID, map[string]any{"status": "failed", "resume": true})
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "task": task})
			return
		}
		s.recordTaskCompletion(result, "task_resume")
		s.appendAudit(UserFromContext(r.Context()), "tasks.write", taskID, map[string]any{"status": result.Task.Status, "resume": true})
		writeJSON(w, http.StatusOK, s.taskResponse(result.Task, result.Session))
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !HasPermission(UserFromContext(r.Context()), "tasks.read") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden", "required_permission": "tasks.read"})
		return
	}
	response := s.taskResponse(task, nil)
	s.appendAudit(UserFromContext(r.Context()), "tasks.read", taskID, nil)
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) taskResponse(task *Task, session *Session) map[string]any {
	response := map[string]any{
		"task":      task,
		"steps":     s.tasks.Steps(task.ID),
		"approvals": s.store.ListTaskApprovals(task.ID),
	}
	if session != nil {
		response["session"] = session
	} else if strings.TrimSpace(task.SessionID) != "" {
		if linkedSession, ok := s.sessions.Get(task.SessionID); ok {
			response["session"] = linkedSession
		}
	}
	return response
}

func (s *Server) recordTaskCompletion(result *TaskExecutionResult, source string) {
	if result == nil || result.Task == nil || result.Session == nil {
		return
	}
	s.appendEvent("task.completed", result.Session.ID, map[string]any{"task_id": result.Task.ID, "status": result.Task.Status, "source": source})
	app, getErr := s.runtimePool.GetOrCreate(result.Task.Assistant, result.Task.Org, result.Task.Project, result.Task.Workspace)
	if getErr != nil {
		return
	}
	freshSession, ok := s.sessions.Get(result.Session.ID)
	if !ok {
		return
	}
	if len(result.ToolActivities) > 0 {
		s.recordSessionToolActivities(freshSession, result.ToolActivities)
		return
	}
	s.recordSessionToolActivities(freshSession, app.Agent.GetLastToolActivities())
}

func (s *Server) handleV2Tasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tasks := s.taskModule.ListTasks()
		writeJSON(w, http.StatusOK, tasks)

	case http.MethodPost:
		var req struct {
			Title          string   `json:"title"`
			Input          string   `json:"input"`
			Mode           string   `json:"mode"`
			SelectedAgent  string   `json:"selected_agent"`
			SelectedAgents []string `json:"selected_agents"`
			Sync           bool     `json:"sync"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}

		if strings.TrimSpace(req.Input) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "input is required"})
			return
		}

		mode := taskModule.ExecutionMode(req.Mode)
		if mode == "" {
			mode = taskModule.ModeSingle
		}
		if mode != taskModule.ModeSingle && mode != taskModule.ModeMulti {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode must be 'single' or 'multi'"})
			return
		}

		taskReq := taskModule.TaskRequest{
			Title:          req.Title,
			Input:          req.Input,
			Mode:           mode,
			SelectedAgent:  req.SelectedAgent,
			SelectedAgents: req.SelectedAgents,
		}

		taskResp, err := s.taskModule.CreateTask(taskReq)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		if req.Sync {
			result, err := s.taskModule.ExecuteTask(r.Context(), taskResp.ID)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
					"task":  result,
					"error": err.Error(),
				})
				return
			}
			writeJSON(w, http.StatusOK, result)
			return
		}

		go func() {
			ctx := context.Background()
			_, _ = s.taskModule.ExecuteTask(ctx, taskResp.ID)
		}()

		writeJSON(w, http.StatusAccepted, taskResp)

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleV2TaskByID(w http.ResponseWriter, r *http.Request) {
	if s.taskModule == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "task module not available"})
		return
	}

	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	taskID := strings.TrimPrefix(r.URL.Path, "/v2/tasks/")
	if taskID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task id required"})
		return
	}

	taskResp, err := s.taskModule.GetTask(taskID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, taskResp)
}
