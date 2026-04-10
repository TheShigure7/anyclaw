package agenthub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anyclaw/anyclaw/pkg/agent"
	"github.com/anyclaw/anyclaw/pkg/config"
	"github.com/anyclaw/anyclaw/pkg/memory"
	"github.com/anyclaw/anyclaw/pkg/prompt"
	"github.com/anyclaw/anyclaw/pkg/skills"
	"github.com/anyclaw/anyclaw/pkg/tools"
)

type PersistentSubagentRegistryOptions struct {
	ConfigPath        string
	DefaultWorkingDir string
	LLM               agent.LLMCaller
	BaseSkills        *skills.SkillsManager
	BaseTools         *tools.Registry
	SessionTTL        time.Duration
}

type PersistentSubagentRegistry struct {
	entries map[string]*persistentSubagentEntry
}

type persistentSubagentEntry struct {
	profile config.PersistentSubagentProfile
	view    PersistentSubagentView
	manager *sessionAgentManager
	memory  memory.MemoryBackend
}

func NewPersistentSubagentRegistry(cfg config.PersistentSubagentsConfig, opts PersistentSubagentRegistryOptions) (*PersistentSubagentRegistry, error) {
	registry := &PersistentSubagentRegistry{entries: make(map[string]*persistentSubagentEntry)}
	if !cfg.Enabled || opts.LLM == nil {
		return registry, nil
	}

	for _, profile := range cfg.Profiles {
		if !profile.IsEnabled() {
			continue
		}
		id := strings.TrimSpace(profile.ID)
		if id == "" {
			continue
		}

		workingDir, err := resolvePersistentSubagentWorkingDir(opts.ConfigPath, opts.DefaultWorkingDir, profile)
		if err != nil {
			continue
		}
		if err := os.MkdirAll(workingDir, 0o755); err != nil {
			continue
		}

		memCfg := memory.DefaultConfig(workingDir)
		mem, err := memory.NewMemoryBackend(memCfg)
		if err != nil || mem.Init() != nil {
			memCfg.Backend = memory.BackendFile
			memCfg.DSN = ""
			mem, err = memory.NewMemoryBackend(memCfg)
			if err != nil {
				continue
			}
			if err := mem.Init(); err != nil {
				continue
			}
		}

		subagentSkills := filteredPersistentSubagentSkills(opts.BaseSkills, profile.RequiredSkills)
		subagentTools := filteredPersistentSubagentTools(opts.BaseTools, profile.PermissionLevel)
		if subagentSkills != nil {
			subagentSkills.RegisterTools(subagentTools, skills.ExecutionOptions{AllowExec: true, ExecTimeoutSeconds: 30})
		}

		personality := buildPersistentSubagentPrompt(profile)
		manager := newSessionAgentManager(func() *agent.Agent {
			return agent.New(agent.Config{
				Name:             persistentSubagentDisplayName(profile),
				Description:      strings.TrimSpace(profile.Description),
				Personality:      personality,
				LLM:              opts.LLM,
				Memory:           mem,
				Skills:           subagentSkills,
				Tools:            subagentTools,
				WorkDir:          workingDir,
				WorkingDir:       workingDir,
				MaxContextTokens: 8192,
			})
		}, opts.SessionTTL)

		registry.entries[id] = &persistentSubagentEntry{
			profile: profile,
			view: PersistentSubagentView{
				ID:              id,
				DisplayName:     persistentSubagentDisplayName(profile),
				Description:     strings.TrimSpace(profile.Description),
				AvatarPreset:    strings.TrimSpace(profile.AvatarPreset),
				AvatarDataURL:   strings.TrimSpace(profile.AvatarDataURL),
				Domain:          strings.TrimSpace(profile.Domain),
				Expertise:       append([]string(nil), profile.Expertise...),
				Status:          "idle",
				ManagedByMain:   profile.IsManagedByMain(),
				Visibility:      firstNonEmpty(strings.TrimSpace(profile.Visibility), "internal_visible"),
				PermissionLevel: firstNonEmpty(strings.TrimSpace(profile.PermissionLevel), "limited"),
				ModelMode:       firstNonEmpty(strings.TrimSpace(profile.ModelMode), "inherit_main"),
				WorkingDir:      workingDir,
				RequiredSkills:  append([]string(nil), profile.RequiredSkills...),
				RequiredCLIs:    append([]string(nil), profile.RequiredCLIs...),
			},
			manager: manager,
			memory:  mem,
		}
	}

	return registry, nil
}

func (r *PersistentSubagentRegistry) Run(ctx context.Context, id string, sessionID string, history []prompt.Message, historyProvided bool, input string) (string, []agent.ToolActivity, error) {
	entry, ok := r.entries[strings.TrimSpace(id)]
	if !ok {
		return "", nil, fmt.Errorf("persistent subagent not found: %s", id)
	}
	result, activities, err := entry.manager.Run(ctx, sessionID, history, historyProvided, input)
	count, lastActive := entry.manager.Stats()
	entry.view.SessionCount = count
	entry.view.LastActiveAt = lastActive
	if count > 0 {
		entry.view.Status = "busy"
	}
	if strings.TrimSpace(result) != "" {
		entry.view.RecentTaskSummary = summarizeText(result, 120)
		entry.view.Status = "idle"
	}
	return result, activities, err
}

func (r *PersistentSubagentRegistry) ClearSession(sessionID string) {
	for _, entry := range r.entries {
		entry.manager.Clear(sessionID)
		count, lastActive := entry.manager.Stats()
		entry.view.SessionCount = count
		entry.view.LastActiveAt = lastActive
		if count == 0 {
			entry.view.Status = "idle"
		}
	}
}

func (r *PersistentSubagentRegistry) History(id string, sessionID string) []prompt.Message {
	entry, ok := r.entries[strings.TrimSpace(id)]
	if !ok {
		return nil
	}
	return entry.manager.History(sessionID)
}

func (r *PersistentSubagentRegistry) RecordExchange(id string, sessionID string, userInput string, response string) {
	entry, ok := r.entries[strings.TrimSpace(id)]
	if !ok {
		return
	}
	entry.manager.RecordExchange(sessionID, userInput, response)
}

func (r *PersistentSubagentRegistry) Match(input string, preferred string) (PersistentSubagentView, string, bool) {
	if preferred != "" {
		if entry, ok := r.entries[strings.TrimSpace(preferred)]; ok {
			return entry.view, "preferred persistent subagent requested", true
		}
	}

	bestScore := 0
	var best PersistentSubagentView
	var bestReason string
	for _, entry := range r.entries {
		if !entry.profile.IsEnabled() || !entry.profile.IsManagedByMain() {
			continue
		}
		score, reason := persistentSubagentMatchScore(input, entry.profile, entry.view)
		if score > bestScore {
			bestScore = score
			best = entry.view
			bestReason = reason
		}
	}
	if bestScore <= 0 {
		return PersistentSubagentView{}, "", false
	}
	return best, bestReason, true
}

func (r *PersistentSubagentRegistry) List() []PersistentSubagentView {
	items := make([]PersistentSubagentView, 0, len(r.entries))
	for _, entry := range r.entries {
		count, lastActive := entry.manager.Stats()
		entry.view.SessionCount = count
		entry.view.LastActiveAt = lastActive
		if count > 0 {
			entry.view.Status = "busy"
		} else {
			entry.view.Status = "idle"
		}
		items = append(items, entry.view)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].DisplayName != items[j].DisplayName {
			return strings.ToLower(items[i].DisplayName) < strings.ToLower(items[j].DisplayName)
		}
		return strings.ToLower(items[i].ID) < strings.ToLower(items[j].ID)
	})
	return items
}

func (r *PersistentSubagentRegistry) Get(id string) (PersistentSubagentView, bool) {
	entry, ok := r.entries[strings.TrimSpace(id)]
	if !ok {
		return PersistentSubagentView{}, false
	}
	count, lastActive := entry.manager.Stats()
	entry.view.SessionCount = count
	entry.view.LastActiveAt = lastActive
	if count > 0 {
		entry.view.Status = "busy"
	} else {
		entry.view.Status = "idle"
	}
	return entry.view, true
}

func (r *PersistentSubagentRegistry) Close() {
	for _, entry := range r.entries {
		if entry.memory != nil {
			_ = entry.memory.Close()
		}
	}
}

func filteredPersistentSubagentSkills(base *skills.SkillsManager, names []string) *skills.SkillsManager {
	if base == nil {
		return skills.NewSkillsManager("")
	}
	return base.FilterEnabled(names)
}

func filteredPersistentSubagentTools(base *tools.Registry, permissionLevel string) *tools.Registry {
	filtered := tools.NewRegistry()
	if base == nil {
		return filtered
	}
	perm := strings.TrimSpace(permissionLevel)
	if perm == "" {
		perm = "limited"
	}
	for _, info := range base.List() {
		tool, ok := base.Get(info.Name)
		if !ok {
			continue
		}
		if !isToolAllowedForPermission(tool.Name, perm) {
			continue
		}
		filtered.Register(tool)
	}
	return filtered
}

func resolvePersistentSubagentWorkingDir(configPath string, defaultWorkingDir string, profile config.PersistentSubagentProfile) (string, error) {
	workingDir := strings.TrimSpace(profile.WorkingDir)
	if workingDir == "" {
		base := defaultWorkingDir
		if strings.TrimSpace(base) == "" {
			base = "workflows"
		}
		workingDir = filepath.Join(base, "persistent-subagents", sanitizeName(profile.ID))
	}
	if resolved := config.ResolvePath(configPath, workingDir); resolved != "" {
		workingDir = resolved
	}
	return filepath.Abs(workingDir)
}

func persistentSubagentDisplayName(profile config.PersistentSubagentProfile) string {
	return firstNonEmpty(strings.TrimSpace(profile.DisplayName), strings.TrimSpace(profile.ID))
}

func buildPersistentSubagentPrompt(profile config.PersistentSubagentProfile) string {
	parts := []string{}
	if prompt := strings.TrimSpace(profile.SystemPrompt); prompt != "" {
		parts = append(parts, prompt)
	}
	if persona := strings.TrimSpace(profile.Persona); persona != "" {
		parts = append(parts, "Persona: "+persona)
	}
	if domain := strings.TrimSpace(profile.Domain); domain != "" {
		parts = append(parts, "Domain: "+domain)
	}
	if len(profile.Expertise) > 0 {
		parts = append(parts, "Expertise: "+strings.Join(profile.Expertise, ", "))
	}
	if personality := strings.TrimSpace(agent.BuildPersonalityPrompt(profile.Personality)); personality != "" {
		parts = append(parts, personality)
	}
	return strings.Join(parts, "\n\n")
}

func persistentSubagentMatchScore(input string, profile config.PersistentSubagentProfile, view PersistentSubagentView) (int, string) {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return 0, ""
	}
	if strings.Contains(input, strings.ToLower(strings.TrimSpace(profile.ID))) ||
		strings.Contains(input, strings.ToLower(strings.TrimSpace(view.DisplayName))) {
		return 100, "persistent subagent explicitly named"
	}

	score := 0
	reasons := []string{}
	for _, token := range keywordParts(profile.Domain) {
		if strings.Contains(input, token) {
			score += 20
			reasons = append(reasons, "domain:"+token)
		}
	}
	for _, expertise := range profile.Expertise {
		for _, token := range keywordParts(expertise) {
			if strings.Contains(input, token) {
				score += 10
				reasons = append(reasons, "expertise:"+token)
			}
		}
	}
	for _, skill := range profile.RequiredSkills {
		for _, token := range keywordParts(skill) {
			if strings.Contains(input, token) {
				score += 4
				reasons = append(reasons, "skill:"+token)
			}
		}
	}
	return score, strings.Join(reasons, ", ")
}

func keywordParts(value string) []string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return nil
	}
	splitter := strings.NewReplacer(",", " ", "/", " ", "-", " ", "_", " ", ".", " ")
	items := strings.Fields(splitter.Replace(value))
	unique := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if len(item) < 3 {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		unique = append(unique, item)
	}
	return unique
}

func summarizeText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		limit = 120
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}

func sanitizeName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "persistent-subagent"
	}
	replacer := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", "_", "-")
	value = replacer.Replace(value)
	return strings.Trim(value, "-.")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func isToolAllowedForPermission(toolName string, permLevel string) bool {
	switch permLevel {
	case "full":
		return true
	case "read-only":
		switch toolName {
		case "read_file", "list_directory", "search_files",
			"web_search", "fetch_url",
			"browser_navigate", "browser_screenshot", "browser_snapshot",
			"browser_click", "browser_wait", "browser_scroll",
			"browser_tab_list", "browser_tab_new", "browser_tab_switch", "browser_tab_close",
			"browser_close", "browser_eval", "browser_select", "browser_press", "browser_type",
			"desktop_screenshot", "desktop_screenshot_window", "desktop_list_windows", "desktop_wait_window", "desktop_inspect_ui", "desktop_resolve_target", "desktop_match_image", "desktop_wait_image", "desktop_ocr", "desktop_verify_text", "desktop_find_text", "desktop_wait_text", "desktop_clipboard_get":
			return true
		default:
			return !strings.HasPrefix(toolName, "write_") &&
				!strings.HasPrefix(toolName, "run_command") &&
				!strings.HasPrefix(toolName, "desktop_") &&
				toolName != "browser_upload" &&
				toolName != "browser_download" &&
				toolName != "browser_pdf"
		}
	default:
		return true
	}
}
