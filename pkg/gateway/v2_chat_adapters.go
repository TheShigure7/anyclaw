package gateway

import (
	"strings"
	"time"

	"github.com/anyclaw/anyclaw/pkg/chat"
)

func chatResponseFromSession(session *Session) *chat.ChatResponse {
	if session == nil {
		return nil
	}
	history := chatMessagesFromSession(session)
	last := chat.Message{}
	if len(history) > 0 {
		last = history[len(history)-1]
	}
	return &chat.ChatResponse{
		SessionID: session.ID,
		AgentName: session.Agent,
		Message:   last,
		History:   history,
	}
}

func chatSessionFromGateway(session *Session) chat.Session {
	if session == nil {
		return chat.Session{}
	}
	return chat.Session{
		ID:        session.ID,
		AgentName: session.Agent,
		Title:     session.Title,
		Messages:  chatMessagesFromSession(session),
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
	}
}

func chatMessagesFromSession(session *Session) []chat.Message {
	if session == nil || len(session.Messages) == 0 {
		return nil
	}
	items := make([]chat.Message, 0, len(session.Messages))
	for _, message := range session.Messages {
		agentName := strings.TrimSpace(message.Agent)
		if agentName == "" && strings.EqualFold(strings.TrimSpace(message.Role), "assistant") {
			agentName = strings.TrimSpace(session.Agent)
		}
		items = append(items, chat.Message{
			Role:      message.Role,
			Content:   message.Content,
			AgentName: agentName,
			Timestamp: firstNonZeroTime(message.CreatedAt, session.UpdatedAt, session.CreatedAt),
		})
	}
	return items
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}
