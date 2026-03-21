package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"github.com/anyclaw/anyclaw/pkg/domain/audit"
	"github.com/anyclaw/anyclaw/pkg/domain/task"
	"github.com/anyclaw/anyclaw/pkg/runtimecore"
	"github.com/anyclaw/anyclaw/pkg/security"
	"github.com/anyclaw/anyclaw/pkg/storage"
)

type CreateTaskInput struct {
	AssistantID string           `json:"assistant_id"`
	Goal        string           `json:"goal"`
	Priority    string           `json:"priority"`
	Operations  []task.Operation `json:"operations"`
}

type Service struct {
	runtime  *runtimecore.Engine
	security *security.Service
	store    *storage.LocalStore
}

func NewService(runtime *runtimecore.Engine, security *security.Service, store *storage.LocalStore) *Service {
	return &Service{runtime: runtime, security: security, store: store}
}

func (s *Service) CreateTask(input CreateTaskInput) (*task.Task, error) {
	assistantItem, err := s.store.GetAssistant(input.AssistantID)
	if err != nil {
		return nil, err
	}

	workspaceRoot, err := s.security.PrepareWorkspace(assistantItem.WorkspaceID)
	if err != nil {
		return nil, err
	}

	plan := s.runtime.BuildPlan(input.Goal, input.Operations)
	now := time.Now().UTC().Format(time.RFC3339)
	item := task.Task{
		ID:            fmt.Sprintf("tsk_%d", time.Now().UTC().UnixNano()),
		AssistantID:   assistantItem.ID,
		WorkspaceID:   workspaceRoot,
		Goal:          strings.TrimSpace(input.Goal),
		PlanSteps:     plan.PlanSteps,
		Operations:    plan.Operations,
		CurrentStep:   0,
		ApprovedStep:  -1,
		Priority:      defaultString(input.Priority, "normal"),
		ApprovalState: "not_required",
		RetryState:    "idle",
		Result:        "",
		Status:        "pending",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.store.SaveTask(item); err != nil {
		return nil, err
	}
	if err := s.appendAudit(item.ID, "task.created", assistantItem.ID, "success", "low", "system", item.Goal); err != nil {
		return nil, err
	}
	return s.executeTask(item.ID)
}

func (s *Service) GetTask(id string) (*task.Task, error) {
	return s.store.GetTask(id)
}

func (s *Service) ListTasks() ([]task.Task, error) {
	return s.store.ListTasks()
}

func (s *Service) ApproveTask(id string) (*task.Task, error) {
	item, err := s.store.GetTask(id)
	if err != nil {
		return nil, err
	}
	if item.Status != "waiting_approval" {
		return nil, fmt.Errorf("task %q is not waiting for approval", id)
	}
	item.ApprovalState = "approved"
	item.ApprovedStep = item.CurrentStep
	item.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.store.SaveTask(*item); err != nil {
		return nil, err
	}
	if err := s.appendAudit(item.ID, "task.approved", item.AssistantID, "success", "medium", "user", fmt.Sprintf("approved step %d", item.CurrentStep)); err != nil {
		return nil, err
	}
	return s.executeTask(item.ID)
}

func (s *Service) executeTask(id string) (*task.Task, error) {
	item, err := s.store.GetTask(id)
	if err != nil {
		return nil, err
	}
	assistantItem, err := s.store.GetAssistant(item.AssistantID)
	if err != nil {
		return nil, err
	}

	item.Status = "running"
	item.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.store.SaveTask(*item); err != nil {
		return nil, err
	}

	results := make([]string, 0, len(item.Operations))
	for i := item.CurrentStep; i < len(item.Operations); i++ {
		op := item.Operations[i]
		decision := s.security.EvaluateOperation(*assistantItem, op, item.WorkspaceID)
		item.Operations[i].RiskLevel = decision.RiskLevel
		item.Operations[i].RequiresApproval = decision.RequiresApproval

		if !decision.Allowed {
			item.Operations[i].Status = "blocked"
			item.Operations[i].Result = decision.Reason
			item.Status = "failed"
			item.Result = decision.Reason
			item.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			if err := s.store.SaveTask(*item); err != nil {
				return nil, err
			}
			_ = s.appendAudit(item.ID, "task.blocked", item.AssistantID, "blocked", decision.RiskLevel, "security", decision.Reason)
			return item, nil
		}

		approvedForStep := item.ApprovedStep == i && item.ApprovalState == "approved"
		if decision.RequiresApproval && !approvedForStep {
			item.Operations[i].Status = "waiting_approval"
			item.Operations[i].Result = decision.Reason
			item.Status = "waiting_approval"
			item.ApprovalState = "pending"
			item.CurrentStep = i
			item.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			if err := s.store.SaveTask(*item); err != nil {
				return nil, err
			}
			_ = s.appendAudit(item.ID, "task.approval_requested", item.AssistantID, "pending", decision.RiskLevel, "security", decision.Reason)
			return item, nil
		}

		result, err := s.runtime.ExecuteOperation(op, item.WorkspaceID, s.security)
		item.Operations[i].Status = "completed"
		item.Operations[i].Result = result
		item.CurrentStep = i + 1
		item.ApprovalState = "not_required"
		item.ApprovedStep = -1
		item.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		results = append(results, fmt.Sprintf("[%s] %s", op.Tool, result))
		if err != nil {
			item.Operations[i].Status = "failed"
			item.Status = "failed"
			item.Result = result
			if saveErr := s.store.SaveTask(*item); saveErr != nil {
				return nil, saveErr
			}
			_ = s.appendAudit(item.ID, "task.operation_failed", item.AssistantID, "failed", item.Operations[i].RiskLevel, op.Tool, result)
			return item, nil
		}
		if err := s.store.SaveTask(*item); err != nil {
			return nil, err
		}
		_ = s.appendAudit(item.ID, "task.operation_completed", item.AssistantID, "success", item.Operations[i].RiskLevel, op.Tool, result)
	}

	if len(item.Operations) == 0 {
		results = append(results, "No executable operations were provided")
	}
	item.Status = "success"
	item.Result = strings.Join(results, "\n")
	item.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.store.SaveTask(*item); err != nil {
		return nil, err
	}
	_ = s.appendAudit(item.ID, "task.completed", item.AssistantID, "success", "low", "system", item.Result)
	return item, nil
}

func (s *Service) appendAudit(taskID, action, target, result, risk, actor, message string) error {
	return s.store.AppendAudit(audit.Event{
		ID:               fmt.Sprintf("aud_%d", time.Now().UTC().UnixNano()),
		TaskID:           taskID,
		Actor:            actor,
		Action:           action,
		Target:           target,
		RiskLevel:        risk,
		ConfirmationMode: "manual",
		ToolName:         actor,
		Result:           result + ": " + message,
		Timestamp:        time.Now().UTC().Format(time.RFC3339),
	})
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
