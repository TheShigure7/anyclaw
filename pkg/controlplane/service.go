package controlplane

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/anyclaw/anyclaw/pkg/domain/assistant"
	"github.com/anyclaw/anyclaw/pkg/domain/audit"
	"github.com/anyclaw/anyclaw/pkg/storage"
)

type CreateAssistantInput struct {
	Name              string   `json:"name"`
	Role              string   `json:"role"`
	Persona           string   `json:"persona"`
	DefaultModel      string   `json:"default_model"`
	EnabledSkills     []string `json:"enabled_skills"`
	PermissionProfile string   `json:"permission_profile"`
	WorkspaceID       string   `json:"workspace_id"`
	WorkspacePath     string   `json:"workspace_path"`
	AllowedTools      []string `json:"allowed_tools"`
}

type Service struct {
	store *storage.LocalStore
}

func NewService(store *storage.LocalStore) *Service {
	return &Service{store: store}
}

func (s *Service) CreateAssistant(input CreateAssistantInput) (*assistant.Assistant, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("assistant name is required")
	}

	workspacePath := strings.TrimSpace(input.WorkspacePath)
	if workspacePath == "" {
		workspacePath = "."
	}
	absPath, err := filepath.Abs(workspacePath)
	if err != nil {
		return nil, err
	}

	profile := strings.TrimSpace(input.PermissionProfile)
	if profile == "" {
		profile = "limited"
	}

	model := strings.TrimSpace(input.DefaultModel)
	if model == "" {
		model = "local-planner"
	}

	item := assistant.Assistant{
		ID:                newID("ast"),
		Name:              name,
		Role:              strings.TrimSpace(input.Role),
		Persona:           strings.TrimSpace(input.Persona),
		DefaultModel:      model,
		EnabledSkills:     input.EnabledSkills,
		PermissionProfile: profile,
		WorkspaceID:       absPath,
		Status:            "active",
	}

	if err := s.store.SaveAssistant(item); err != nil {
		return nil, err
	}
	if err := s.store.AppendAudit(audit.Event{
		ID:        newID("aud"),
		Actor:     "system",
		Action:    "assistant.created",
		Target:    item.ID,
		Result:    "success",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Service) ListAssistants() ([]assistant.Assistant, error) {
	return s.store.ListAssistants()
}

func (s *Service) GetAssistant(id string) (*assistant.Assistant, error) {
	return s.store.GetAssistant(id)
}

func (s *Service) ListAudits() ([]audit.Event, error) {
	return s.store.ListAudits()
}

func newID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UTC().UnixNano())
}
