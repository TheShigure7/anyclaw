package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBaseURL = "http://localhost:11434"
	defaultModel   = "llama2"
)

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	model      string
}

type Config struct {
	BaseURL     string
	Model       string
	HTTPClient  *http.Client
	AllowRemote bool
}

type Model struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
	Digest     string `json:"digest"`
}

type generateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type generateResponse struct {
	Response string `json:"response"`
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Message Message `json:"message"`
}

type listResponse struct {
	Models []Model `json:"models"`
}

func NewClient(cfg Config) (*Client, error) {
	rawBaseURL := strings.TrimSpace(cfg.BaseURL)
	if rawBaseURL == "" {
		rawBaseURL = defaultBaseURL
	}
	parsed, err := validateBaseURL(rawBaseURL, cfg.AllowRemote)
	if err != nil {
		return nil, err
	}

	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = defaultModel
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	return &Client{
		baseURL:    parsed,
		httpClient: httpClient,
		model:      model,
	}, nil
}

func (c *Client) Run(ctx context.Context, args []string) (string, error) {
	if len(args) == 0 {
		return c.listModelsText(ctx)
	}

	switch args[0] {
	case "list", "models":
		return c.listModelsText(ctx)
	case "generate", "run":
		if len(args) < 2 {
			return "", fmt.Errorf("usage: ollama run <prompt>")
		}
		return c.Generate(ctx, strings.Join(args[1:], " "))
	case "chat":
		if len(args) < 2 {
			return "", fmt.Errorf("usage: ollama chat <message>")
		}
		return c.Chat(ctx, []Message{{Role: "user", Content: strings.Join(args[1:], " ")}})
	case "show":
		model := c.model
		if len(args) > 1 {
			model = args[1]
		}
		return c.Show(ctx, model)
	case "status":
		if c.IsRunning(ctx) {
			return "Ollama is running", nil
		}
		return "Ollama is not reachable", nil
	default:
		return c.Generate(ctx, strings.Join(args, " "))
	}
}

func (c *Client) Generate(ctx context.Context, prompt string) (string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}

	body, err := json.Marshal(generateRequest{
		Model:  c.model,
		Prompt: prompt,
		Stream: false,
	})
	if err != nil {
		return "", err
	}

	var result generateResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/generate", bytes.NewReader(body), &result); err != nil {
		return "", err
	}
	return result.Response, nil
}

func (c *Client) Chat(ctx context.Context, messages []Message) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("at least one message is required")
	}

	body, err := json.Marshal(chatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   false,
	})
	if err != nil {
		return "", err
	}

	var result chatResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/chat", bytes.NewReader(body), &result); err != nil {
		return "", err
	}
	return result.Message.Content, nil
}

func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	var result listResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/tags", nil, &result); err != nil {
		return nil, err
	}
	return append([]Model(nil), result.Models...), nil
}

func (c *Client) Show(ctx context.Context, model string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", fmt.Errorf("model is required")
	}

	body, err := json.Marshal(map[string]string{"name": model})
	if err != nil {
		return "", err
	}

	var result map[string]any
	if err := c.doJSON(ctx, http.MethodPost, "/api/show", bytes.NewReader(body), &result); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (c *Client) IsRunning(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/api/tags"), nil)
	if err != nil {
		return false
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < http.StatusMultipleChoices
}

func (c *Client) listModelsText(ctx context.Context) (string, error) {
	models, err := c.ListModels(ctx)
	if err != nil {
		return "", err
	}
	if len(models) == 0 {
		return "No Ollama models found", nil
	}
	lines := make([]string, 0, len(models))
	for _, model := range models {
		lines = append(lines, model.Name)
	}
	return strings.Join(lines, "\n"), nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(path), body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusMultipleChoices {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("ollama returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode ollama response: %w", err)
	}
	return nil
}

func (c *Client) endpoint(path string) string {
	resolved := *c.baseURL
	resolved.Path = strings.TrimRight(c.baseURL.Path, "/") + "/" + strings.TrimLeft(path, "/")
	resolved.RawQuery = ""
	resolved.Fragment = ""
	return resolved.String()
}

func validateBaseURL(raw string, allowRemote bool) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid ollama base URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("ollama base URL must use http or https")
	}
	if parsed.Hostname() == "" {
		return nil, fmt.Errorf("ollama base URL must include a host")
	}
	if !allowRemote && !isLoopbackHost(parsed.Hostname()) {
		return nil, fmt.Errorf("ollama base URL must be loopback unless AllowRemote is set")
	}
	return parsed, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
