package vision

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestAnalyzeImageURLRejectsUnsafeURL(t *testing.T) {
	t.Run("GoogleVisionProvider", func(t *testing.T) {
		provider := NewGoogleVisionProvider(DefaultGoogleVisionConfig())
		_, err := provider.AnalyzeImageURL(context.Background(), "http://127.0.0.1/image.png")
		assertUnsafeImageURLError(t, err)
	})

	t.Run("LLMVisionProvider", func(t *testing.T) {
		provider := NewLLMVisionProvider(LLMVisionConfig{Client: &testLLMVisionClient{}})
		_, err := provider.AnalyzeImageURL(context.Background(), "http://127.0.0.1/image.png")
		assertUnsafeImageURLError(t, err)
	})
}

func TestValidateImageFetchURLRejectsUnsafeInputs(t *testing.T) {
	cases := []string{
		"ftp://example.com/image.png",
		"http:///image.png",
		"https://user:pass@example.com/image.png",
		"http://localhost/image.png",
		"http://service.localhost/image.png",
		"http://127.0.0.1/image.png",
		"http://10.0.0.2/image.png",
		"http://169.254.169.254/latest/meta-data",
		"http://[::1]/image.png",
		"http://[fe80::1]/image.png",
	}

	for _, rawURL := range cases {
		err := validateImageFetchURL(context.Background(), rawURL)
		if err == nil {
			t.Fatalf("expected unsafe URL %q to be rejected", rawURL)
		}
	}
}

func TestValidateImageFetchURLAllowsPublicHTTPURLs(t *testing.T) {
	if err := validateImageFetchURL(context.Background(), "https://8.8.8.8/image.png"); err != nil {
		t.Fatalf("expected public URL to be accepted, got %v", err)
	}
}

func TestImageFetchHTTPClientRejectsUnsafeRedirect(t *testing.T) {
	client := imageFetchHTTPClient(&http.Client{})
	req, err := http.NewRequest(http.MethodGet, "http://169.254.169.254/latest/meta-data", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	err = client.CheckRedirect(req, nil)
	assertUnsafeImageURLError(t, err)
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

func TestVisionProviderNamesAndDefaults(t *testing.T) {
	if got := NewGoogleVisionProvider(DefaultGoogleVisionConfig()).Name(); got != "google-vision" {
		t.Fatalf("expected google provider name, got %q", got)
	}
	if got := NewLLMVisionProvider(DefaultLLMVisionConfig()).Name(); got != "llm-vision" {
		t.Fatalf("expected LLM provider name, got %q", got)
	}
	if DefaultLLMVisionConfig().Prompt == "" {
		t.Fatal("expected default LLM vision prompt")
	}
}

func TestGoogleVisionProviderAnalyzeImageParsesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.RawQuery, "key=test-key") {
			t.Fatalf("expected API key query, got %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(googleVisionSuccessResponse))
	}))
	defer server.Close()

	cfg := DefaultGoogleVisionConfig()
	cfg.Endpoint = server.URL
	cfg.APIKey = "test-key"
	provider := NewGoogleVisionProvider(cfg)

	result, err := provider.AnalyzeImage(context.Background(), []byte("image"), "image/png")
	if err != nil {
		t.Fatalf("AnalyzeImage: %v", err)
	}
	if result.Description != "cat" {
		t.Fatalf("expected description cat, got %q", result.Description)
	}
	if len(result.Labels) != 1 || result.Labels[0].Name != "cat" {
		t.Fatalf("expected parsed label, got %#v", result.Labels)
	}
	if len(result.Text) != 1 || result.Text[0].Text != "hello" {
		t.Fatalf("expected parsed text, got %#v", result.Text)
	}
	if len(result.Objects) != 1 || result.Objects[0].Name != "cup" {
		t.Fatalf("expected parsed object, got %#v", result.Objects)
	}
	if len(result.Faces) != 1 || result.Faces[0].Emotions["joy"] == 0 {
		t.Fatalf("expected parsed face emotions, got %#v", result.Faces)
	}
	if len(result.ImageProps.DominantColors) != 1 || result.ImageProps.DominantColors[0] != "#010203" {
		t.Fatalf("expected dominant color #010203, got %#v", result.ImageProps.DominantColors)
	}
}

func TestGoogleVisionProviderAnalyzeImageErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{name: "status error", status: http.StatusBadRequest, body: "bad request"},
		{name: "malformed json", status: http.StatusOK, body: `{invalid}`},
		{name: "empty response", status: http.StatusOK, body: `{"responses":[]}`},
		{name: "api response error", status: http.StatusOK, body: `{"responses":[{"error":{"code":3,"message":"bad image"}}]}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			cfg := DefaultGoogleVisionConfig()
			cfg.Endpoint = server.URL
			cfg.APIKey = "test-key"
			provider := NewGoogleVisionProvider(cfg)

			if _, err := provider.AnalyzeImage(context.Background(), []byte("image"), "image/png"); err == nil {
				t.Fatal("expected AnalyzeImage to fail")
			}
		})
	}
}

func TestGoogleVisionProviderDerivedMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(googleVisionSuccessResponse))
	}))
	defer server.Close()

	cfg := DefaultGoogleVisionConfig()
	cfg.Endpoint = server.URL
	cfg.APIKey = "test-key"
	provider := NewGoogleVisionProvider(cfg)

	texts, err := provider.OCR(context.Background(), []byte("image"), "image/png")
	if err != nil {
		t.Fatalf("OCR: %v", err)
	}
	if len(texts) != 1 || texts[0].Text != "hello" {
		t.Fatalf("expected OCR text, got %#v", texts)
	}

	labels, err := provider.LabelImage(context.Background(), []byte("image"), "image/png")
	if err != nil {
		t.Fatalf("LabelImage: %v", err)
	}
	if len(labels) != 1 || labels[0].Name != "cat" {
		t.Fatalf("expected label, got %#v", labels)
	}

	objects, err := provider.DetectObjects(context.Background(), []byte("image"), "image/png")
	if err != nil {
		t.Fatalf("DetectObjects: %v", err)
	}
	if len(objects) != 1 || objects[0].Name != "cup" {
		t.Fatalf("expected object, got %#v", objects)
	}
}

func TestLLMVisionProviderOCRLabelAndDetectObjects(t *testing.T) {
	provider := NewLLMVisionProvider(LLMVisionConfig{
		Client: &testLLMVisionClient{
			analyzeFunc: func(ctx context.Context, imageData []byte, mimeType string, prompt string) (string, error) {
				switch {
				case strings.Contains(prompt, "Extract all text"):
					return "invoice\n123", nil
				case strings.Contains(prompt, "comma-separated"):
					return "cat:0.91\ndog", nil
				default:
					return "objects", nil
				}
			},
		},
	})

	texts, err := provider.OCR(context.Background(), []byte("image"), "image/png")
	if err != nil {
		t.Fatalf("OCR: %v", err)
	}
	if len(texts) != 1 || texts[0].Text != "invoice\n123" {
		t.Fatalf("expected OCR output, got %#v", texts)
	}

	labels, err := provider.LabelImage(context.Background(), []byte("image"), "image/png")
	if err != nil {
		t.Fatalf("LabelImage: %v", err)
	}
	if len(labels) != 2 || labels[0].Name != "cat" || labels[1].Name != "dog" {
		t.Fatalf("expected parsed labels, got %#v", labels)
	}

	if _, err := provider.DetectObjects(context.Background(), []byte("image"), "image/png"); err == nil {
		t.Fatal("expected DetectObjects unsupported error")
	}
}

func TestLLMVisionProviderReturnsClientErrors(t *testing.T) {
	provider := NewLLMVisionProvider(LLMVisionConfig{
		Client: &testLLMVisionClient{
			analyzeFunc: func(ctx context.Context, imageData []byte, mimeType string, prompt string) (string, error) {
				return "", errors.New("provider failed")
			},
		},
	})

	if _, err := provider.AnalyzeImage(context.Background(), []byte("image"), "image/png"); err == nil {
		t.Fatal("expected AnalyzeImage client error")
	}
	if _, err := provider.OCR(context.Background(), []byte("image"), "image/png"); err == nil {
		t.Fatal("expected OCR client error")
	}
	if _, err := provider.LabelImage(context.Background(), []byte("image"), "image/png"); err == nil {
		t.Fatal("expected LabelImage client error")
	}
	if _, err := provider.DetectObjects(context.Background(), []byte("image"), "image/png"); err == nil {
		t.Fatal("expected DetectObjects client error")
	}
}

func TestAnalyzeImageFile(t *testing.T) {
	provider := &testVisionProvider{
		analyzeImageFunc: func(ctx context.Context, imageData []byte, mimeType string) (*AnalysisResult, error) {
			if string(imageData) != "image" {
				t.Fatalf("expected image bytes, got %q", string(imageData))
			}
			if mimeType != "image/png" {
				t.Fatalf("expected image/png, got %s", mimeType)
			}
			return &AnalysisResult{Description: "file"}, nil
		},
	}

	path := t.TempDir() + "/sample.png"
	if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	result, err := AnalyzeImageFile(context.Background(), provider, path)
	if err != nil {
		t.Fatalf("AnalyzeImageFile: %v", err)
	}
	if result.Description != "file" {
		t.Fatalf("expected file description, got %q", result.Description)
	}

	if _, err := AnalyzeImageFile(context.Background(), provider, path+".missing"); err == nil {
		t.Fatal("expected read error")
	}
}

func TestUploadImageForAnalysis(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("expected bearer token, got %q", r.Header.Get("Authorization"))
		}
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		_, _ = w.Write([]byte(`{"description":"uploaded","metadata":{"source":"test"}}`))
	}))
	defer server.Close()

	result, err := UploadImageForAnalysis(context.Background(), server.URL, "token", []byte("image"), "image/png")
	if err != nil {
		t.Fatalf("UploadImageForAnalysis: %v", err)
	}
	if result.Description != "uploaded" {
		t.Fatalf("expected uploaded description, got %q", result.Description)
	}
}

func TestUploadImageForAnalysisErrors(t *testing.T) {
	t.Run("status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("bad gateway"))
		}))
		defer server.Close()

		if _, err := UploadImageForAnalysis(context.Background(), server.URL, "", []byte("image"), ""); err == nil {
			t.Fatal("expected upload status error")
		}
	})

	t.Run("json", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{invalid}`))
		}))
		defer server.Close()

		if _, err := UploadImageForAnalysis(context.Background(), server.URL, "", []byte("image"), ""); err == nil {
			t.Fatal("expected upload parse error")
		}
	})
}

func TestFetchImageURLUsesProvidedClient(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", req.Method)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("image")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}

	resp, err := fetchImageURL(context.Background(), client, "https://8.8.8.8/image.png")
	if err != nil {
		t.Fatalf("fetchImageURL: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if string(body) != "image" {
		t.Fatalf("expected image body, got %q", string(body))
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

func assertUnsafeImageURLError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected unsafe image URL error")
	}
	if !strings.Contains(err.Error(), "unsafe image URL") && !strings.Contains(err.Error(), "image URL host is required") {
		t.Fatalf("expected unsafe image URL error, got %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

const googleVisionSuccessResponse = `{
  "responses": [
    {
      "labelAnnotations": [
        {"description": "cat", "score": 0.95}
      ],
      "textAnnotations": [
        {
          "description": "hello",
          "score": 0.80,
          "boundingPoly": {
            "vertices": [
              {"x": 1, "y": 2},
              {"x": 5, "y": 2},
              {"x": 5, "y": 8}
            ]
          }
        }
      ],
      "objectLocalizations": [
        {
          "name": "cup",
          "score": 0.70,
          "boundingPoly": {
            "vertices": [
              {"x": 2, "y": 3},
              {"x": 7, "y": 3},
              {"x": 7, "y": 11}
            ]
          }
        }
      ],
      "faceAnnotations": [
        {
          "detectionConfidence": 0.88,
          "joyLikelihood": "LIKELY",
          "sorrowLikelihood": "VERY_UNLIKELY",
          "angerLikelihood": "UNLIKELY",
          "surpriseLikelihood": "POSSIBLE",
          "boundingPoly": {
            "vertices": [
              {"x": 0, "y": 1},
              {"x": 10, "y": 1},
              {"x": 10, "y": 12}
            ]
          }
        }
      ],
      "safeSearchAnnotation": {
        "adult": "VERY_UNLIKELY",
        "violence": "UNLIKELY",
        "medical": "POSSIBLE",
        "spoof": "LIKELY"
      },
      "imagePropertiesAnnotation": {
        "dominantColors": {
          "colors": [
            {"color": {"red": 1, "green": 2, "blue": 3}, "score": 0.50}
          ]
        }
      }
    }
  ]
}`
