package vision

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testLLMVisionClient struct {
	analyzeFunc func(ctx context.Context, imageData []byte, mimeType string, prompt string) (string, error)
}

func (c *testLLMVisionClient) AnalyzeImageWithPrompt(ctx context.Context, imageData []byte, mimeType string, prompt string) (string, error) {
	if c.analyzeFunc != nil {
		return c.analyzeFunc(ctx, imageData, mimeType, prompt)
	}
	return "ok", nil
}

func TestLLMVisionProviderRejectsMissingClient(t *testing.T) {
	t.Run("AnalyzeImage", func(t *testing.T) {
		provider := NewLLMVisionProvider(LLMVisionConfig{})
		_, err := provider.AnalyzeImage(context.Background(), []byte("img"), "image/png")
		assertMissingLLMClientError(t, err)
	})

	t.Run("OCR", func(t *testing.T) {
		provider := NewLLMVisionProvider(LLMVisionConfig{})
		_, err := provider.OCR(context.Background(), []byte("img"), "image/png")
		assertMissingLLMClientError(t, err)
	})

	t.Run("LabelImage", func(t *testing.T) {
		provider := NewLLMVisionProvider(LLMVisionConfig{})
		_, err := provider.LabelImage(context.Background(), []byte("img"), "image/png")
		assertMissingLLMClientError(t, err)
	})

	t.Run("DetectObjects", func(t *testing.T) {
		provider := NewLLMVisionProvider(LLMVisionConfig{})
		_, err := provider.DetectObjects(context.Background(), []byte("img"), "image/png")
		assertMissingLLMClientError(t, err)
	})
}

func TestLLMVisionProviderAnalyzeImageURLRejectsMissingClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("img"))
	}))
	defer server.Close()

	provider := NewLLMVisionProvider(LLMVisionConfig{})
	_, err := provider.AnalyzeImageURL(context.Background(), server.URL)
	assertMissingLLMClientError(t, err)
}

func TestLLMVisionProviderAnalyzeImageUsesConfiguredClient(t *testing.T) {
	provider := NewLLMVisionProvider(LLMVisionConfig{
		Client: &testLLMVisionClient{
			analyzeFunc: func(ctx context.Context, imageData []byte, mimeType string, prompt string) (string, error) {
				if mimeType != "image/png" {
					t.Fatalf("expected mime type image/png, got %s", mimeType)
				}
				if prompt == "" {
					t.Fatal("expected prompt to be populated")
				}
				return "analyzed", nil
			},
		},
	})

	result, err := provider.AnalyzeImage(context.Background(), []byte("img"), "image/png")
	if err != nil {
		t.Fatalf("AnalyzeImage: %v", err)
	}
	if result.Description != "analyzed" {
		t.Fatalf("expected description %q, got %q", "analyzed", result.Description)
	}
}

func assertMissingLLMClientError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected missing client error")
	}
	if !strings.Contains(err.Error(), "no LLM vision client configured") {
		t.Fatalf("expected missing client error, got %v", err)
	}
}
