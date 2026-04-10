package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type sessionGatewayTool struct {
	baseURL  string
	apiToken string
	client   *http.Client
}

func RegisterSessionTools(r *Registry, opts BuiltinOptions) {
	tool := newSessionGatewayTool(opts)

	r.RegisterTool(
		"sessions_spawn",
		"Create a new AnyClaw/OpenClaw-style session through the gateway HTTP API",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":        map[string]any{"type": "string", "description": "Optional session title"},
				"agent":        map[string]any{"type": "string", "description": "Optional agent/profile name"},
				"assistant":    map[string]any{"type": "string", "description": "Alias for agent"},
				"org":          map[string]any{"type": "string", "description": "Optional org id"},
				"project":      map[string]any{"type": "string", "description": "Optional project id"},
				"workspace":    map[string]any{"type": "string", "description": "Workspace id"},
				"session_mode": map[string]any{"type": "string", "description": "Optional session mode"},
				"queue_mode":   map[string]any{"type": "string", "description": "Optional queue mode"},
				"reply_back":   map[string]any{"type": "boolean", "description": "Whether replies should be sent back to the source channel"},
			},
			"required": []string{"workspace"},
		},
		func(ctx context.Context, input map[string]any) (string, error) {
			return auditCall(opts, "sessions_spawn", input, tool.spawnSession)(ctx, input)
		},
	)

	r.RegisterTool(
		"sessions_send",
		"Send a message to an existing AnyClaw/OpenClaw-style session through the gateway HTTP API",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"session_id":  map[string]any{"type": "string", "description": "Target session id"},
				"session_key": map[string]any{"type": "string", "description": "Alias for session_id"},
				"message":     map[string]any{"type": "string", "description": "Message content to send"},
				"title":       map[string]any{"type": "string", "description": "Optional display title for this turn"},
			},
			"required": []string{"message"},
		},
		func(ctx context.Context, input map[string]any) (string, error) {
			return auditCall(opts, "sessions_send", input, tool.sendToSession)(ctx, input)
		},
	)
}

func newSessionGatewayTool(opts BuiltinOptions) *sessionGatewayTool {
	client := opts.GatewayHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &sessionGatewayTool{
		baseURL:  strings.TrimRight(strings.TrimSpace(opts.GatewayBaseURL), "/"),
		apiToken: strings.TrimSpace(opts.GatewayAPIToken),
		client:   client,
	}
}

func (t *sessionGatewayTool) spawnSession(ctx context.Context, input map[string]any) (string, error) {
	workspace := firstNonEmptyString(input, "workspace", "workspace_id")
	if workspace == "" {
		return "", fmt.Errorf("workspace is required")
	}
	query := url.Values{}
	query.Set("workspace", workspace)
	if org := firstNonEmptyString(input, "org"); org != "" {
		query.Set("org", org)
	}
	if project := firstNonEmptyString(input, "project"); project != "" {
		query.Set("project", project)
	}

	body := map[string]any{}
	copyOptionalString(input, body, "title")
	copyOptionalString(input, body, "agent")
	copyOptionalString(input, body, "assistant")
	copyOptionalString(input, body, "session_mode")
	copyOptionalString(input, body, "queue_mode")
	copyOptionalBool(input, body, "reply_back")

	response, err := t.doJSONRequest(ctx, http.MethodPost, "/sessions", query, body)
	if err != nil {
		return "", err
	}
	return marshalSessionToolResult(response), nil
}

func (t *sessionGatewayTool) sendToSession(ctx context.Context, input map[string]any) (string, error) {
	sessionID := firstNonEmptyString(input, "session_id", "session_key", "sessionKey")
	if sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}
	message := firstNonEmptyString(input, "message")
	if message == "" {
		return "", fmt.Errorf("message is required")
	}
	body := map[string]any{
		"session_id": sessionID,
		"message":    message,
	}
	copyOptionalString(input, body, "title")

	response, err := t.doJSONRequest(ctx, http.MethodPost, "/chat", nil, body)
	if err != nil {
		return "", err
	}
	return marshalSessionToolResult(response), nil
}

func (t *sessionGatewayTool) doJSONRequest(ctx context.Context, method, path string, query url.Values, body map[string]any) (map[string]any, error) {
	baseURL := t.baseURL
	if baseURL == "" {
		baseURL = "http://127.0.0.1:18789"
	}
	targetURL := baseURL + path
	if len(query) > 0 {
		targetURL += "?" + query.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if t.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+t.apiToken)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gateway request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("gateway request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("invalid gateway response: %w", err)
	}
	return payload, nil
}

func marshalSessionToolResult(payload map[string]any) string {
	if payload == nil {
		return "{}"
	}
	if _, hasSession := payload["session"]; !hasSession {
		if id, ok := payload["id"].(string); ok && strings.TrimSpace(id) != "" {
			payload = map[string]any{
				"session":     payload,
				"session_key": id,
			}
		}
	}
	if session, ok := payload["session"].(map[string]any); ok {
		if _, exists := payload["session_key"]; !exists {
			if id, ok := session["id"].(string); ok && strings.TrimSpace(id) != "" {
				payload["session_key"] = id
			}
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(`{"error":"failed to marshal gateway response: %s"}`, strings.TrimSpace(err.Error()))
	}
	return string(data)
}

func firstNonEmptyString(input map[string]any, keys ...string) string {
	for _, key := range keys {
		value, _ := input[key].(string)
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func copyOptionalString(src map[string]any, dst map[string]any, key string) {
	if value := firstNonEmptyString(src, key); value != "" {
		dst[key] = value
	}
}

func copyOptionalBool(src map[string]any, dst map[string]any, key string) {
	if src == nil {
		return
	}
	if value, ok := src[key].(bool); ok {
		dst[key] = value
	}
}
