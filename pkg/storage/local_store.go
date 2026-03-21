package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/anyclaw/anyclaw/pkg/domain/assistant"
	"github.com/anyclaw/anyclaw/pkg/domain/audit"
	"github.com/anyclaw/anyclaw/pkg/domain/task"
)

type LocalStore struct {
	BaseDir string
	mu      sync.Mutex
}

func NewLocalStore(baseDir string) *LocalStore {
	return &LocalStore{BaseDir: baseDir}
}

func (s *LocalStore) SaveAssistant(item assistant.Assistant) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := s.readAssistantsLocked()
	if err != nil {
		return err
	}

	replaced := false
	for i := range items {
		if items[i].ID == item.ID {
			items[i] = item
			replaced = true
			break
		}
	}
	if !replaced {
		items = append(items, item)
	}

	return s.writeJSONLocked("assistants.json", items)
}

func (s *LocalStore) ListAssistants() ([]assistant.Assistant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := s.readAssistantsLocked()
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (s *LocalStore) GetAssistant(id string) (*assistant.Assistant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := s.readAssistantsLocked()
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.ID == id {
			copyItem := item
			return &copyItem, nil
		}
	}
	return nil, fmt.Errorf("assistant %q not found", id)
}

func (s *LocalStore) SaveTask(item task.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := s.readTasksLocked()
	if err != nil {
		return err
	}

	replaced := false
	for i := range items {
		if items[i].ID == item.ID {
			items[i] = item
			replaced = true
			break
		}
	}
	if !replaced {
		items = append(items, item)
	}

	return s.writeJSONLocked("tasks.json", items)
}

func (s *LocalStore) GetTask(id string) (*task.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := s.readTasksLocked()
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.ID == id {
			copyItem := item
			return &copyItem, nil
		}
	}
	return nil, fmt.Errorf("task %q not found", id)
}

func (s *LocalStore) ListTasks() ([]task.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := s.readTasksLocked()
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt > items[j].CreatedAt
	})
	return items, nil
}

func (s *LocalStore) AppendAudit(event audit.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := s.readAuditsLocked()
	if err != nil {
		return err
	}
	items = append(items, event)
	return s.writeJSONLocked("audits.json", items)
}

func (s *LocalStore) ListAudits() ([]audit.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := s.readAuditsLocked()
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Timestamp > items[j].Timestamp
	})
	return items, nil
}

func (s *LocalStore) readAssistantsLocked() ([]assistant.Assistant, error) {
	var items []assistant.Assistant
	if err := s.readJSONLocked("assistants.json", &items); err != nil {
		return nil, err
	}
	if items == nil {
		items = []assistant.Assistant{}
	}
	return items, nil
}

func (s *LocalStore) readTasksLocked() ([]task.Task, error) {
	var items []task.Task
	if err := s.readJSONLocked("tasks.json", &items); err != nil {
		return nil, err
	}
	if items == nil {
		items = []task.Task{}
	}
	return items, nil
}

func (s *LocalStore) readAuditsLocked() ([]audit.Event, error) {
	var items []audit.Event
	if err := s.readJSONLocked("audits.json", &items); err != nil {
		return nil, err
	}
	if items == nil {
		items = []audit.Event{}
	}
	return items, nil
}

func (s *LocalStore) readJSONLocked(name string, dst any) error {
	if err := s.ensureBaseDirLocked(); err != nil {
		return err
	}
	path := filepath.Join(s.BaseDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, dst)
}

func (s *LocalStore) writeJSONLocked(name string, value any) error {
	if err := s.ensureBaseDirLocked(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.BaseDir, name)
	return os.WriteFile(path, data, 0o644)
}

func (s *LocalStore) ensureBaseDirLocked() error {
	if s.BaseDir == "" {
		return fmt.Errorf("storage base dir is empty")
	}
	return os.MkdirAll(s.BaseDir, 0o755)
}
