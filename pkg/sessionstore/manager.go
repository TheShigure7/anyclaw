package sessionstore

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anyclaw/anyclaw/pkg/prompt"
)

type Session struct {
	ID                string            `json:"id"`
	Title             string            `json:"title"`
	Agent             string            `json:"agent,omitempty"`
	Participants      []string          `json:"participants,omitempty"`
	Org               string            `json:"org,omitempty"`
	Project           string            `json:"project,omitempty"`
	Workspace         string            `json:"workspace,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
	MessageCount      int               `json:"message_count"`
	LastUserText      string            `json:"last_user_text,omitempty"`
	History           []prompt.Message  `json:"history"`
	Messages          []SessionMessage  `json:"messages,omitempty"`
	SessionMode       string            `json:"session_mode,omitempty"`
	QueueMode         string            `json:"queue_mode,omitempty"`
	ReplyBack         bool              `json:"reply_back,omitempty"`
	SourceChannel     string            `json:"source_channel,omitempty"`
	SourceID          string            `json:"source_id,omitempty"`
	UserID            string            `json:"user_id,omitempty"`
	UserName          string            `json:"user_name,omitempty"`
	ReplyTarget       string            `json:"reply_target,omitempty"`
	ThreadID          string            `json:"thread_id,omitempty"`
	TransportMeta     map[string]string `json:"transport_meta,omitempty"`
	ParentSessionID   string            `json:"parent_session_id,omitempty"`
	GroupKey          string            `json:"group_key,omitempty"`
	IsGroup           bool              `json:"is_group,omitempty"`
	Presence          string            `json:"presence,omitempty"`
	Typing            bool              `json:"typing,omitempty"`
	QueueDepth        int               `json:"queue_depth,omitempty"`
	LastActiveAt      time.Time         `json:"last_active_at,omitempty"`
	LastAssistantText string            `json:"last_assistant_text,omitempty"`
}

type SessionMessage struct {
	ID        string         `json:"id"`
	Role      string         `json:"role"`
	Agent     string         `json:"agent,omitempty"`
	Content   string         `json:"content"`
	Kind      string         `json:"kind,omitempty"`
	TaskID    string         `json:"task_id,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	Meta      map[string]any `json:"meta,omitempty"`
}

type Store interface {
	SaveSession(session *Session) error
	GetSession(id string) (*Session, bool)
	ListSessions() []*Session
	DeleteSession(id string) error
}

type SessionAgent interface {
	Run(ctx context.Context, userInput string) (string, error)
	GetHistory() []prompt.Message
	SetHistory(history []prompt.Message)
}

type SessionManager struct {
	mu      sync.Mutex
	store   Store
	agent   SessionAgent
	nextID  func() string
	nowFunc func() time.Time
}

type SessionCreateOptions struct {
	Title           string
	AgentName       string
	Participants    []string
	Org             string
	Project         string
	Workspace       string
	SessionMode     string
	QueueMode       string
	ReplyBack       bool
	SourceChannel   string
	SourceID        string
	UserID          string
	UserName        string
	ReplyTarget     string
	ThreadID        string
	TransportMeta   map[string]string
	ParentSessionID string
	GroupKey        string
	IsGroup         bool
}

type SessionPatchOptions struct {
	Title       string
	AgentName   string
	Org         string
	Project     string
	Workspace   string
	SessionMode string
	QueueMode   string
	ReplyBack   *bool
}

func NewSessionManager(store Store, agent SessionAgent) *SessionManager {
	return &SessionManager{
		store: store,
		agent: agent,
		nextID: func() string {
			return uniqueID("sess")
		},
		nowFunc: func() time.Time { return time.Now().UTC() },
	}
}

func (m *SessionManager) Create(title string, agentName string, org string, project string, workspace string) (*Session, error) {
	return m.CreateWithOptions(SessionCreateOptions{Title: title, AgentName: agentName, Org: org, Project: project, Workspace: workspace})
}

func (m *SessionManager) CreateWithOptions(opts SessionCreateOptions) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.nowFunc()
	participants := NormalizeParticipants(opts.AgentName, opts.Participants)
	primaryAgent := opts.AgentName
	if primaryAgent == "" && len(participants) > 0 {
		primaryAgent = participants[0]
	}
	session := &Session{
		ID:              m.nextID(),
		Title:           opts.Title,
		Agent:           primaryAgent,
		Participants:    nil,
		Org:             opts.Org,
		Project:         opts.Project,
		Workspace:       opts.Workspace,
		CreatedAt:       now,
		UpdatedAt:       now,
		History:         []prompt.Message{},
		Messages:        []SessionMessage{},
		SessionMode:     defaultSessionMode(opts.SessionMode),
		QueueMode:       defaultQueueMode(opts.QueueMode),
		ReplyBack:       opts.ReplyBack,
		SourceChannel:   opts.SourceChannel,
		SourceID:        opts.SourceID,
		UserID:          opts.UserID,
		UserName:        opts.UserName,
		ReplyTarget:     opts.ReplyTarget,
		ThreadID:        opts.ThreadID,
		TransportMeta:   cloneStringMap(opts.TransportMeta),
		ParentSessionID: opts.ParentSessionID,
		GroupKey:        "",
		IsGroup:         false,
		Presence:        "idle",
		Typing:          false,
		LastActiveAt:    now,
	}
	if session.Title == "" {
		session.Title = "New session"
	}
	if err := m.store.SaveSession(session); err != nil {
		return nil, err
	}
	return cloneSession(session), nil
}

func (m *SessionManager) List() []*Session {
	return m.store.ListSessions()
}

func (m *SessionManager) Get(id string) (*Session, bool) {
	return m.store.GetSession(id)
}

func (m *SessionManager) FindByBinding(sourceChannel string, replyTarget string, threadID string, agentName string) (*Session, bool) {
	sourceChannel = strings.TrimSpace(sourceChannel)
	replyTarget = strings.TrimSpace(replyTarget)
	threadID = strings.TrimSpace(threadID)
	agentName = strings.TrimSpace(agentName)

	sessions := m.store.ListSessions()
	var best *Session
	for _, session := range sessions {
		if session == nil {
			continue
		}
		if sourceChannel != "" && !strings.EqualFold(strings.TrimSpace(session.SourceChannel), sourceChannel) {
			continue
		}
		if agentName != "" && !strings.EqualFold(strings.TrimSpace(session.Agent), agentName) {
			continue
		}
		sessionReplyTarget := strings.TrimSpace(session.ReplyTarget)
		if replyTarget != "" && sessionReplyTarget != replyTarget {
			continue
		}
		sessionThreadID := strings.TrimSpace(session.ThreadID)
		if threadID != "" && sessionThreadID != threadID {
			continue
		}
		if best == nil || session.UpdatedAt.After(best.UpdatedAt) {
			best = session
		}
	}
	if best == nil {
		return nil, false
	}
	return cloneSession(best), true
}

func (m *SessionManager) AddExchange(sessionID string, userText string, assistantText string) (*Session, error) {
	messages := []SessionMessage{
		{
			ID:        uniqueID("msg"),
			Role:      "user",
			Content:   userText,
			CreatedAt: m.nowFunc(),
		},
		{
			ID:        uniqueID("msg"),
			Role:      "assistant",
			Content:   assistantText,
			CreatedAt: m.nowFunc(),
		},
	}
	return m.AddMessages(sessionID, messages)
}

func (m *SessionManager) AddMessages(sessionID string, messages []SessionMessage) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.store.GetSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	now := m.nowFunc()
	for _, message := range messages {
		normalized := normalizeSessionMessage(message, now)
		session.Messages = append(session.Messages, normalized)
	}
	session.History = buildPromptHistory(session)
	session.MessageCount = len(session.Messages)
	session.LastUserText = lastSessionMessageContent(session.Messages, "user")
	session.LastAssistantText = lastSessionMessageContent(session.Messages, "assistant")
	session.UpdatedAt = now
	session.LastActiveAt = now
	session.Presence = "idle"
	session.Typing = false
	if session.QueueDepth > 0 {
		session.QueueDepth--
	}
	if session.Title == "New session" && session.LastUserText != "" {
		session.Title = ShortenTitle(session.LastUserText)
	}
	if err := m.store.SaveSession(session); err != nil {
		return nil, err
	}
	return cloneSession(session), nil
}

func (m *SessionManager) SetUserMapping(sessionID string, userID string, userName string, replyTarget string, threadID string, transportMeta map[string]string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.store.GetSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	if strings.TrimSpace(userID) != "" {
		session.UserID = strings.TrimSpace(userID)
	}
	if strings.TrimSpace(userName) != "" {
		session.UserName = strings.TrimSpace(userName)
	}
	if strings.TrimSpace(replyTarget) != "" {
		session.ReplyTarget = strings.TrimSpace(replyTarget)
	}
	if strings.TrimSpace(threadID) != "" {
		session.ThreadID = strings.TrimSpace(threadID)
	}
	if len(transportMeta) > 0 {
		if session.TransportMeta == nil {
			session.TransportMeta = map[string]string{}
		}
		for k, v := range transportMeta {
			if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
				session.TransportMeta[k] = v
			}
		}
	}
	session.UpdatedAt = m.nowFunc()
	if err := m.store.SaveSession(session); err != nil {
		return nil, err
	}
	return cloneSession(session), nil
}

func (m *SessionManager) MoveSession(sessionID string, org string, project string, workspace string, agent string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.store.GetSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	if strings.TrimSpace(org) != "" {
		session.Org = org
	}
	if strings.TrimSpace(project) != "" {
		session.Project = project
	}
	if strings.TrimSpace(workspace) != "" {
		session.Workspace = workspace
	}
	if strings.TrimSpace(agent) != "" {
		session.Agent = agent
	}
	session.UpdatedAt = m.nowFunc()
	if err := m.store.SaveSession(session); err != nil {
		return nil, err
	}
	return cloneSession(session), nil
}

func (m *SessionManager) PatchSession(sessionID string, opts SessionPatchOptions) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.store.GetSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	if strings.TrimSpace(opts.Title) != "" {
		session.Title = strings.TrimSpace(opts.Title)
	}
	if strings.TrimSpace(opts.AgentName) != "" {
		session.Agent = strings.TrimSpace(opts.AgentName)
	}
	if strings.TrimSpace(opts.Org) != "" {
		session.Org = strings.TrimSpace(opts.Org)
	}
	if strings.TrimSpace(opts.Project) != "" {
		session.Project = strings.TrimSpace(opts.Project)
	}
	if strings.TrimSpace(opts.Workspace) != "" {
		session.Workspace = strings.TrimSpace(opts.Workspace)
	}
	if strings.TrimSpace(opts.SessionMode) != "" {
		session.SessionMode = defaultSessionMode(opts.SessionMode)
	}
	if strings.TrimSpace(opts.QueueMode) != "" {
		session.QueueMode = defaultQueueMode(opts.QueueMode)
	}
	if opts.ReplyBack != nil {
		session.ReplyBack = *opts.ReplyBack
	}
	session.UpdatedAt = m.nowFunc()
	session.LastActiveAt = session.UpdatedAt
	if err := m.store.SaveSession(session); err != nil {
		return nil, err
	}
	return cloneSession(session), nil
}

func (m *SessionManager) Delete(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store.DeleteSession(sessionID)
}

func (m *SessionManager) Abort(sessionID string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.store.GetSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	session.QueueDepth = 0
	session.Presence = "idle"
	session.Typing = false
	session.UpdatedAt = m.nowFunc()
	session.LastActiveAt = session.UpdatedAt
	if err := m.store.SaveSession(session); err != nil {
		return nil, err
	}
	return cloneSession(session), nil
}

func (m *SessionManager) SetPresence(sessionID string, presence string, typing bool) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.store.GetSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	if strings.TrimSpace(presence) != "" {
		session.Presence = strings.TrimSpace(presence)
	}
	session.Typing = typing
	session.UpdatedAt = m.nowFunc()
	session.LastActiveAt = session.UpdatedAt
	if err := m.store.SaveSession(session); err != nil {
		return nil, err
	}
	return cloneSession(session), nil
}

func (m *SessionManager) EnqueueTurn(sessionID string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.store.GetSession(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	session.QueueDepth++
	session.Presence = "queued"
	session.UpdatedAt = m.nowFunc()
	if err := m.store.SaveSession(session); err != nil {
		return nil, err
	}
	return cloneSession(session), nil
}

func NormalizeParticipants(primary string, participants []string) []string {
	seen := make(map[string]bool)
	items := make([]string, 0, len(participants)+1)
	appendName := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		items = append(items, name)
	}
	appendName(primary)
	for _, name := range participants {
		appendName(name)
	}
	return items
}

func ShortenTitle(input string) string {
	trimmed := input
	if len(trimmed) > 48 {
		trimmed = trimmed[:48]
	}
	if trimmed == "" {
		return "New session"
	}
	return trimmed
}

func defaultSessionMode(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "main"
	}
	switch value {
	case "group", "group-shared", "channel-group":
		return "main"
	}
	return value
}

func defaultQueueMode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "fifo"
	}
	return value
}

func CloneSession(session *Session) *Session {
	if session == nil {
		return nil
	}
	clone := *session
	clone.TransportMeta = cloneStringMap(session.TransportMeta)
	clone.Participants = nil
	clone.GroupKey = ""
	clone.IsGroup = false
	clone.History = append([]prompt.Message(nil), session.History...)
	clone.Messages = cloneSessionMessages(session.Messages)
	if len(clone.Messages) == 0 && len(clone.History) > 0 {
		clone.Messages = legacyMessagesFromHistory(&clone)
	}
	clone.MessageCount = len(clone.Messages)
	if clone.MessageCount > 0 {
		clone.LastUserText = lastSessionMessageContent(clone.Messages, "user")
		clone.LastAssistantText = lastSessionMessageContent(clone.Messages, "assistant")
	}
	return &clone
}

func cloneSession(session *Session) *Session {
	return CloneSession(session)
}

func cloneSessionMessage(message SessionMessage) SessionMessage {
	clone := message
	if message.Meta != nil {
		clone.Meta = make(map[string]any, len(message.Meta))
		for k, v := range message.Meta {
			clone.Meta[k] = v
		}
	}
	return clone
}

func cloneSessionMessages(messages []SessionMessage) []SessionMessage {
	if len(messages) == 0 {
		return nil
	}
	items := make([]SessionMessage, 0, len(messages))
	for _, message := range messages {
		items = append(items, cloneSessionMessage(message))
	}
	return items
}

func normalizeSessionMessage(message SessionMessage, fallbackTime time.Time) SessionMessage {
	if strings.TrimSpace(message.ID) == "" {
		message.ID = uniqueID("msg")
	}
	message.Role = strings.TrimSpace(strings.ToLower(message.Role))
	if message.Role == "" {
		message.Role = "assistant"
	}
	message.Agent = strings.TrimSpace(message.Agent)
	message.Kind = strings.TrimSpace(message.Kind)
	if message.CreatedAt.IsZero() {
		message.CreatedAt = fallbackTime
	}
	if message.Meta != nil {
		meta := make(map[string]any, len(message.Meta))
		for k, v := range message.Meta {
			meta[k] = v
		}
		message.Meta = meta
	}
	return message
}

func buildPromptHistory(session *Session) []prompt.Message {
	messages := session.Messages
	if len(messages) == 0 {
		return append([]prompt.Message(nil), session.History...)
	}
	history := make([]prompt.Message, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case "user":
			history = append(history, prompt.Message{Role: "user", Content: message.Content})
		case "assistant":
			history = append(history, prompt.Message{Role: "assistant", Content: message.Content})
		case "system":
			history = append(history, prompt.Message{Role: "assistant", Content: fmt.Sprintf("[system] %s", message.Content)})
		}
	}
	return history
}

func lastSessionMessageContent(messages []SessionMessage, role string) string {
	role = strings.TrimSpace(strings.ToLower(role))
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.TrimSpace(strings.ToLower(messages[i].Role)) == role {
			return messages[i].Content
		}
	}
	return ""
}

func legacyMessagesFromHistory(session *Session) []SessionMessage {
	items := make([]SessionMessage, 0, len(session.History))
	for _, message := range session.History {
		role := strings.TrimSpace(strings.ToLower(message.Role))
		sessionMessage := SessionMessage{
			ID:        uniqueID("msg"),
			Role:      role,
			Content:   message.Content,
			CreatedAt: session.UpdatedAt,
		}
		if role == "assistant" {
			sessionMessage.Agent = session.Agent
		}
		items = append(items, sessionMessage)
	}
	return items
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	clone := make(map[string]string, len(input))
	for k, v := range input {
		clone[k] = v
	}
	return clone
}

var idCounter uint64

func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UTC().UnixNano(), atomic.AddUint64(&idCounter, 1))
}
