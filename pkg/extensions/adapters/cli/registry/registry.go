package cliadapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type Entry struct {
	Name           string `json:"name"`
	DisplayName    string `json:"display_name"`
	Version        string `json:"version"`
	Description    string `json:"description"`
	Requires       string `json:"requires"`
	Homepage       string `json:"homepage"`
	InstallCmd     string `json:"install_cmd"`
	EntryPoint     string `json:"entry_point"`
	SkillMD        string `json:"skill_md"`
	Category       string `json:"category"`
	Contributor    string `json:"contributor"`
	ContributorURL string `json:"contributor_url"`
}

type EntryStatus struct {
	Entry
	Installed      bool   `json:"installed"`
	ExecutablePath string `json:"executable_path,omitempty"`
}

type Registry struct {
	mu         sync.RWMutex
	root       string
	entries    map[string]*EntryStatus
	categories map[string]int
}

func NewRegistry(root string) (*Registry, error) {
	r := &Registry{
		root:       root,
		entries:    make(map[string]*EntryStatus),
		categories: make(map[string]int),
	}

	if err := r.load(); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *Registry) load() error {
	data, err := os.ReadFile(filepath.Join(r.root, "registry.json"))
	if err != nil {
		return err
	}

	var file struct {
		Meta struct {
			Repo        string `json:"repo"`
			Description string `json:"description"`
			Updated     string `json:"updated"`
		} `json:"meta"`
		CLIs []Entry `json:"clis"`
	}

	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}

	for _, e := range file.CLIs {
		entry := e
		entry.Name = strings.TrimSpace(entry.Name)
		if entry.Name == "" {
			return fmt.Errorf("registry entry name is required")
		}
		key := strings.ToLower(entry.Name)
		if _, exists := r.entries[key]; exists {
			return fmt.Errorf("duplicate registry entry %q", entry.Name)
		}
		r.entries[key] = &EntryStatus{
			Entry:     entry,
			Installed: false,
		}
		r.categories[entry.Category]++
	}

	return nil
}

func (r *Registry) Get(name string) (*EntryStatus, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return nil, false
	}
	return cloneEntryStatus(entry), true
}

func (r *Registry) List() []*EntryStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*EntryStatus, 0, len(r.entries))
	for _, e := range r.entries {
		result = append(result, cloneEntryStatus(e))
	}
	sortEntryStatuses(result)
	return result
}

func (r *Registry) Search(query string, category string, limit int) []*EntryStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries := make([]*EntryStatus, 0, len(r.entries))
	for _, e := range r.entries {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	var results []*EntryStatus
	query = strings.ToLower(query)
	category = strings.ToLower(category)

	for _, e := range entries {
		if category != "" && strings.ToLower(e.Category) != category {
			continue
		}

		if query == "" {
			results = append(results, cloneEntryStatus(e))
		} else if strings.Contains(strings.ToLower(e.Name), query) ||
			strings.Contains(strings.ToLower(e.DisplayName), query) ||
			strings.Contains(strings.ToLower(e.Description), query) {
			results = append(results, cloneEntryStatus(e))
		}

		if limit > 0 && len(results) >= limit {
			break
		}
	}

	return results
}

func (r *Registry) Categories() map[string]int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]int)
	for k, v := range r.categories {
		result[k] = v
	}
	return result
}

func (r *Registry) Root() string {
	return r.root
}

func (r *Registry) EntriesCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}

type builtinAdapter struct {
	Name        string
	Description string
	Category    string
	Handler     func(args []string) (string, error)
}

var builtinAdapters = map[string]*builtinAdapter{}
var builtinAdaptersMu sync.RWMutex

func RegisterBuiltinAdapter(name, desc, category string, handler func(args []string) (string, error)) {
	builtinAdaptersMu.Lock()
	defer builtinAdaptersMu.Unlock()
	builtinAdapters[strings.ToLower(strings.TrimSpace(name))] = &builtinAdapter{
		Name:        name,
		Description: desc,
		Category:    category,
		Handler:     handler,
	}
}

func GetBuiltinAdapter(name string) (*builtinAdapter, bool) {
	builtinAdaptersMu.RLock()
	defer builtinAdaptersMu.RUnlock()
	a, ok := builtinAdapters[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return nil, false
	}
	return cloneBuiltinAdapter(a), true
}

func ListBuiltinAdapters() []*builtinAdapter {
	builtinAdaptersMu.RLock()
	defer builtinAdaptersMu.RUnlock()
	result := make([]*builtinAdapter, 0, len(builtinAdapters))
	for _, a := range builtinAdapters {
		result = append(result, cloneBuiltinAdapter(a))
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func (r *Registry) Find(name string) (*EntryStatus, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	name = strings.ToLower(strings.TrimSpace(name))

	if entry, ok := r.entries[name]; ok {
		return cloneEntryStatus(entry), true
	}

	for _, e := range r.entries {
		if strings.ToLower(e.EntryPoint) == name ||
			strings.ToLower(e.DisplayName) == name {
			return cloneEntryStatus(e), true
		}
	}

	builtinAdaptersMu.RLock()
	adapter, ok := builtinAdapters[name]
	builtinAdaptersMu.RUnlock()
	if ok {
		return &EntryStatus{
			Entry: Entry{
				Name:        adapter.Name,
				DisplayName: adapter.Name,
				Description: adapter.Description,
				Category:    adapter.Category,
			},
			Installed: true,
		}, true
	}

	return nil, false
}

func (r *Registry) MarkInstalled(name string, path string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if e, ok := r.entries[strings.ToLower(strings.TrimSpace(name))]; ok {
		e.Installed = true
		e.ExecutablePath = path
	}
}

func (r *Registry) JSON() (string, error) {
	builtinAdaptersMu.RLock()
	builtinCount := len(builtinAdapters)
	builtinAdaptersMu.RUnlock()

	data := map[string]any{
		"root":          r.root,
		"entries_count": r.EntriesCount(),
		"categories":    r.Categories(),
		"entries":       r.List(),
		"builtin_count": builtinCount,
	}

	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", err
	}

	return string(b), nil
}

func cloneEntryStatus(entry *EntryStatus) *EntryStatus {
	if entry == nil {
		return nil
	}
	cloned := *entry
	return &cloned
}

func cloneBuiltinAdapter(adapter *builtinAdapter) *builtinAdapter {
	if adapter == nil {
		return nil
	}
	cloned := *adapter
	return &cloned
}

func sortEntryStatuses(entries []*EntryStatus) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
}
