package ingress

import (
	"fmt"
	"strings"
	"time"
)

func NewChannelRawRequest(sessionID string, message string, meta map[string]string) RawRequest {
	channelID := firstNonEmpty(meta["channel"], "channel")
	replyTarget := strings.TrimSpace(meta["reply_target"])
	conversationID := firstNonEmpty(meta["chat_id"], replyTarget, meta["source"], meta["channel_id"], meta["user_id"])
	requestID := firstNonEmpty(meta["message_id"], fmt.Sprintf("%s-%d", channelID, time.Now().UTC().UnixNano()))
	userID := firstNonEmpty(meta["user_id"], meta["source"])
	displayName := firstNonEmpty(meta["username"], meta["user_name"], meta["sender"])

	return RawRequest{
		RequestID: requestID,
		SourceRef: SourceRef{
			SourceType:     "channel",
			EntryPoint:     "channel",
			ChannelID:      channelID,
			ConversationID: conversationID,
			MessageID:      strings.TrimSpace(meta["message_id"]),
			CallbackID:     strings.TrimSpace(meta["callback_id"]),
		},
		ActorHint: ActorHint{
			UserID:      strings.TrimSpace(userID),
			SessionID:   strings.TrimSpace(sessionID),
			DisplayName: strings.TrimSpace(displayName),
		},
		Payload: RawPayload{
			Kind:     firstNonEmpty(meta["message_type"], "message"),
			Text:     message,
			Metadata: cloneStringMap(meta),
		},
		ReceivedAt: time.Now().UTC(),
	}
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for k, v := range input {
		if strings.TrimSpace(k) == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
