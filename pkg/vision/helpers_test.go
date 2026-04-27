package vision

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	llm "github.com/1024XEngineer/anyclaw/pkg/capability/models"
)

type testVisionProvider struct {
	analyzeImageFunc    func(ctx context.Context, imageData []byte, mimeType string) (*AnalysisResult, error)
	analyzeImageURLFunc func(ctx context.Context, imageURL string) (*AnalysisResult, error)
	ocrFunc             func(ctx context.Context, imageData []byte, mimeType string) ([]DetectedText, error)
	analyzeImageHit     bool
	analyzeImageURLHit  bool
	ocrHit              bool
}

func (p *testVisionProvider) Name() string {
	return "test"
}

func (p *testVisionProvider) AnalyzeImage(ctx context.Context, imageData []byte, mimeType string) (*AnalysisResult, error) {
	p.analyzeImageHit = true
	if p.analyzeImageFunc != nil {
		return p.analyzeImageFunc(ctx, imageData, mimeType)
	}
	return &AnalysisResult{}, nil
}

func (p *testVisionProvider) AnalyzeImageURL(ctx context.Context, imageURL string) (*AnalysisResult, error) {
	p.analyzeImageURLHit = true
	if p.analyzeImageURLFunc != nil {
		return p.analyzeImageURLFunc(ctx, imageURL)
	}
	return nil, errors.New("not implemented")
}

func (p *testVisionProvider) OCR(ctx context.Context, imageData []byte, mimeType string) ([]DetectedText, error) {
	p.ocrHit = true
	if p.ocrFunc != nil {
		return p.ocrFunc(ctx, imageData, mimeType)
	}
	return nil, errors.New("not implemented")
}

func (p *testVisionProvider) LabelImage(ctx context.Context, imageData []byte, mimeType string) ([]Label, error) {
	return nil, errors.New("not implemented")
}

func (p *testVisionProvider) DetectObjects(ctx context.Context, imageData []byte, mimeType string) ([]DetectedObject, error) {
	return nil, errors.New("not implemented")
}

type testModelClient struct {
	chatFunc func(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (*llm.Response, error)
}

func (c *testModelClient) Chat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (*llm.Response, error) {
	if c.chatFunc != nil {
		return c.chatFunc(ctx, messages, tools)
	}
	return &llm.Response{Content: "described"}, nil
}

func (c *testModelClient) StreamChat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition, onChunk func(string)) error {
	return nil
}

func (c *testModelClient) Name() string {
	return "test-model"
}

func TestImageDataURLRoundTrip(t *testing.T) {
	data := []byte("image-bytes")
	mimeType := "image/png"

	dataURL := ImageToDataURL(data, mimeType)
	if !strings.HasPrefix(dataURL, "data:image/png;base64,") {
		t.Fatalf("unexpected data URL prefix: %s", dataURL)
	}

	decoded, gotMimeType, err := ImageFromDataURL(dataURL)
	if err != nil {
		t.Fatalf("ImageFromDataURL: %v", err)
	}
	if gotMimeType != mimeType {
		t.Fatalf("expected mime type %s, got %s", mimeType, gotMimeType)
	}
	if string(decoded) != string(data) {
		t.Fatalf("expected decoded data %q, got %q", string(data), string(decoded))
	}
}

func TestImageFromDataURLRejectsInvalidInput(t *testing.T) {
	if _, _, err := ImageFromDataURL("not-a-data-url"); err == nil {
		t.Fatal("expected non-data URL to fail")
	}
	if _, _, err := ImageFromDataURL("data:image/png;base64"); err == nil {
		t.Fatal("expected malformed data URL to fail")
	}
}

func TestDetectMediaType(t *testing.T) {
	cases := []struct {
		mimeType string
		want     string
	}{
		{mimeType: "image/jpeg", want: "image"},
		{mimeType: "video/mp4", want: "video"},
		{mimeType: "audio/mpeg", want: "audio"},
		{mimeType: "application/json", want: ""},
	}

	for _, tc := range cases {
		if got := detectMediaType(tc.mimeType); got != tc.want {
			t.Fatalf("detectMediaType(%q) = %q, want %q", tc.mimeType, got, tc.want)
		}
	}
}

func TestAnalyzeMediaRejectsUnsupportedType(t *testing.T) {
	pipeline := NewMediaUnderstandingPipeline(DefaultMediaUnderstandingConfig())
	if _, err := AnalyzeMedia(context.Background(), pipeline, []byte("x"), "application/json"); err == nil {
		t.Fatal("expected unsupported media type error")
	}
}

func TestUnderstandImageRejectsOversizedEncodedImage(t *testing.T) {
	provider := &testVisionProvider{}
	pipeline := NewMediaUnderstandingPipeline(MediaUnderstandingConfig{
		VisionProvider: provider,
		MaxImageSize:   4,
		Timeout:        DefaultMediaUnderstandingConfig().Timeout,
	})

	_, err := pipeline.UnderstandImage(context.Background(), []byte("12345"), "image/png")
	if err == nil {
		t.Fatal("expected oversized image to fail")
	}
	if !strings.Contains(err.Error(), "image too large") {
		t.Fatalf("expected image size error, got %v", err)
	}
	if provider.analyzeImageHit {
		t.Fatal("expected provider not to be called for oversized image")
	}
}

func TestUnderstandImageCallsProviderForValidSizedImage(t *testing.T) {
	provider := &testVisionProvider{
		analyzeImageFunc: func(ctx context.Context, imageData []byte, mimeType string) (*AnalysisResult, error) {
			return &AnalysisResult{
				Description: "a cat",
				Labels:      []Label{{Name: "cat"}, {Name: "pet"}},
			}, nil
		},
	}
	pipeline := NewMediaUnderstandingPipeline(MediaUnderstandingConfig{
		VisionProvider: provider,
		MaxImageSize:   16,
		Timeout:        DefaultMediaUnderstandingConfig().Timeout,
	})

	result, err := pipeline.UnderstandImage(context.Background(), []byte("12345"), "image/png")
	if err != nil {
		t.Fatalf("UnderstandImage: %v", err)
	}
	if !provider.analyzeImageHit {
		t.Fatal("expected provider to be called")
	}
	if result.Description != "a cat" {
		t.Fatalf("expected description %q, got %q", "a cat", result.Description)
	}
	if result.Summary != "cat, pet" {
		t.Fatalf("expected summary %q, got %q", "cat, pet", result.Summary)
	}
}

func TestUnderstandImageWithoutProvider(t *testing.T) {
	pipeline := NewMediaUnderstandingPipeline(DefaultMediaUnderstandingConfig())

	result, err := pipeline.UnderstandImage(context.Background(), []byte("image"), "image/png")
	if err != nil {
		t.Fatalf("UnderstandImage: %v", err)
	}
	if result.Description != "No vision provider configured" {
		t.Fatalf("expected no provider description, got %q", result.Description)
	}
}

func TestUnderstandImageFile(t *testing.T) {
	pipeline := NewMediaUnderstandingPipeline(DefaultMediaUnderstandingConfig())
	path := t.TempDir() + "/photo.png"
	if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
		t.Fatalf("write image fixture: %v", err)
	}

	result, err := pipeline.UnderstandImageFile(context.Background(), path)
	if err != nil {
		t.Fatalf("UnderstandImageFile: %v", err)
	}
	if result.Type != "image" {
		t.Fatalf("expected image result, got %q", result.Type)
	}

	if _, err := pipeline.UnderstandImageFile(context.Background(), path+".missing"); err == nil {
		t.Fatal("expected missing file error")
	}
}

func TestUnderstandImageURL(t *testing.T) {
	t.Run("missing provider", func(t *testing.T) {
		pipeline := NewMediaUnderstandingPipeline(DefaultMediaUnderstandingConfig())
		if _, err := pipeline.UnderstandImageURL(context.Background(), "https://example.com/image.png"); err == nil {
			t.Fatal("expected missing provider error")
		}
	})

	t.Run("success", func(t *testing.T) {
		provider := &testVisionProvider{
			analyzeImageURLFunc: func(ctx context.Context, imageURL string) (*AnalysisResult, error) {
				if imageURL != "https://example.com/image.png" {
					t.Fatalf("unexpected image URL %s", imageURL)
				}
				return &AnalysisResult{
					Description: "remote",
					Labels:      []Label{{Name: "remote"}, {Name: "image"}},
				}, nil
			},
		}
		pipeline := NewMediaUnderstandingPipeline(MediaUnderstandingConfig{
			VisionProvider: provider,
			Timeout:        DefaultMediaUnderstandingConfig().Timeout,
		})

		result, err := pipeline.UnderstandImageURL(context.Background(), "https://example.com/image.png")
		if err != nil {
			t.Fatalf("UnderstandImageURL: %v", err)
		}
		if result.Summary != "remote, image" {
			t.Fatalf("expected summary, got %q", result.Summary)
		}
		if !provider.analyzeImageURLHit {
			t.Fatal("expected provider AnalyzeImageURL to be called")
		}
	})

	t.Run("provider error", func(t *testing.T) {
		provider := &testVisionProvider{
			analyzeImageURLFunc: func(ctx context.Context, imageURL string) (*AnalysisResult, error) {
				return nil, errors.New("fetch failed")
			},
		}
		pipeline := NewMediaUnderstandingPipeline(MediaUnderstandingConfig{
			VisionProvider: provider,
			Timeout:        DefaultMediaUnderstandingConfig().Timeout,
		})

		if _, err := pipeline.UnderstandImageURL(context.Background(), "https://example.com/image.png"); err == nil {
			t.Fatal("expected provider URL error")
		}
	})
}

func TestOCRImage(t *testing.T) {
	t.Run("missing provider", func(t *testing.T) {
		pipeline := NewMediaUnderstandingPipeline(DefaultMediaUnderstandingConfig())
		if _, err := pipeline.OCRImage(context.Background(), []byte("image"), "image/png"); err == nil {
			t.Fatal("expected missing provider error")
		}
	})

	t.Run("success", func(t *testing.T) {
		provider := &testVisionProvider{
			ocrFunc: func(ctx context.Context, imageData []byte, mimeType string) ([]DetectedText, error) {
				return []DetectedText{{Text: "hello"}, {Text: "world"}}, nil
			},
		}
		pipeline := NewMediaUnderstandingPipeline(MediaUnderstandingConfig{
			VisionProvider: provider,
			Timeout:        DefaultMediaUnderstandingConfig().Timeout,
		})

		text, err := pipeline.OCRImage(context.Background(), []byte("image"), "image/png")
		if err != nil {
			t.Fatalf("OCRImage: %v", err)
		}
		if text != "hello\nworld" {
			t.Fatalf("expected OCR text, got %q", text)
		}
	})

	t.Run("provider error", func(t *testing.T) {
		provider := &testVisionProvider{
			ocrFunc: func(ctx context.Context, imageData []byte, mimeType string) ([]DetectedText, error) {
				return nil, errors.New("ocr failed")
			},
		}
		pipeline := NewMediaUnderstandingPipeline(MediaUnderstandingConfig{
			VisionProvider: provider,
			Timeout:        DefaultMediaUnderstandingConfig().Timeout,
		})

		if _, err := pipeline.OCRImage(context.Background(), []byte("image"), "image/png"); err == nil {
			t.Fatal("expected OCR provider error")
		}
	})
}

func TestDescribeImageWithLLM(t *testing.T) {
	pipeline := NewMediaUnderstandingPipeline(DefaultMediaUnderstandingConfig())

	if _, err := pipeline.DescribeImageWithLLM(context.Background(), []byte("image"), "image/png", nil, ""); err == nil {
		t.Fatal("expected missing LLM client error")
	}

	client := &testModelClient{
		chatFunc: func(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (*llm.Response, error) {
			if len(messages) != 1 {
				t.Fatalf("expected one message, got %d", len(messages))
			}
			return &llm.Response{Content: "a detailed image"}, nil
		},
	}

	description, err := pipeline.DescribeImageWithLLM(context.Background(), []byte("image"), "image/png", client, "")
	if err != nil {
		t.Fatalf("DescribeImageWithLLM: %v", err)
	}
	if description != "a detailed image" {
		t.Fatalf("expected description, got %q", description)
	}

	errorClient := &testModelClient{
		chatFunc: func(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (*llm.Response, error) {
			return nil, errors.New("chat failed")
		},
	}
	if _, err := pipeline.DescribeImageWithLLM(context.Background(), []byte("image"), "image/png", errorClient, "prompt"); err == nil {
		t.Fatal("expected chat error")
	}
}

func TestAnalyzeMediaRoutesSupportedTypes(t *testing.T) {
	provider := &testVisionProvider{
		analyzeImageFunc: func(ctx context.Context, imageData []byte, mimeType string) (*AnalysisResult, error) {
			return &AnalysisResult{Description: "image"}, nil
		},
	}
	pipeline := NewMediaUnderstandingPipeline(MediaUnderstandingConfig{
		VisionProvider: provider,
		MaxImageSize:   100,
		Timeout:        DefaultMediaUnderstandingConfig().Timeout,
	})

	result, err := AnalyzeMedia(context.Background(), pipeline, []byte("image"), "image/png")
	if err != nil {
		t.Fatalf("AnalyzeMedia image: %v", err)
	}
	if result.Type != "image" {
		t.Fatalf("expected image result, got %q", result.Type)
	}

	pipeline.cfg.KeyFrameExtractor = NewKeyFrameExtractor()
	pipeline.cfg.KeyFrameExtractor.SetFFmpegPath("definitely-missing-ffmpeg-binary")
	if _, err := AnalyzeMedia(context.Background(), pipeline, []byte("video"), "video/mp4"); err == nil {
		t.Fatal("expected video analysis error")
	}

	pipeline.cfg.AudioAnalyzer = NewAudioAnalyzer()
	pipeline.cfg.AudioAnalyzer.SetFFmpegPath("definitely-missing-ffmpeg-binary")
	if _, err := AnalyzeMedia(context.Background(), pipeline, []byte("audio"), "audio/mpeg"); err == nil {
		t.Fatal("expected audio analysis error")
	}
}

func TestUnderstandVideoAndAudioWithCommandOutput(t *testing.T) {
	withFakeVisionCommands(t)

	provider := &testVisionProvider{
		analyzeImageFunc: func(ctx context.Context, imageData []byte, mimeType string) (*AnalysisResult, error) {
			return &AnalysisResult{Description: "scene frame"}, nil
		},
	}

	keyFrames := NewKeyFrameExtractor()
	keyFrames.SetFFmpegPath("fake-ffmpeg")
	keyFrames.SetFFprobePath("fake-ffprobe")
	keyFrames.SetMaxKeyFrames(1)

	audio := NewAudioAnalyzer()
	audio.SetFFmpegPath("fake-ffmpeg")
	audio.SetFFprobePath("fake-ffprobe")

	pipeline := NewMediaUnderstandingPipeline(MediaUnderstandingConfig{
		VisionProvider:    provider,
		KeyFrameExtractor: keyFrames,
		AudioAnalyzer:     audio,
		Timeout:           DefaultMediaUnderstandingConfig().Timeout,
	})

	videoResult, err := pipeline.UnderstandVideo(context.Background(), []byte("video"))
	if err != nil {
		t.Fatalf("UnderstandVideo: %v", err)
	}
	if videoResult.Type != "video" {
		t.Fatalf("expected video result, got %q", videoResult.Type)
	}
	if !strings.Contains(videoResult.Summary, "scene frame") {
		t.Fatalf("expected scene summary, got %q", videoResult.Summary)
	}

	audioResult, err := pipeline.UnderstandAudio(context.Background(), []byte("audio"))
	if err != nil {
		t.Fatalf("UnderstandAudio: %v", err)
	}
	if audioResult.Type != "audio" {
		t.Fatalf("expected audio result, got %q", audioResult.Type)
	}
	if audioResult.Metadata["duration"] != 12.5 {
		t.Fatalf("expected audio duration metadata, got %#v", audioResult.Metadata["duration"])
	}
}

func TestAnalysisResultToJSON(t *testing.T) {
	result := &MediaUnderstandingResult{
		Type:        "image",
		Description: "desc",
	}
	encoded := AnalysisResultToJSON(result)
	if !strings.Contains(encoded, `"description": "desc"`) {
		t.Fatalf("expected JSON description, got %s", encoded)
	}
}

func TestMimeTypeFromPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{path: "photo.jpg", want: "image/jpeg"},
		{path: "photo.JPEG", want: "image/jpeg"},
		{path: "photo.png", want: "image/png"},
		{path: "photo.gif", want: "image/gif"},
		{path: "photo.webp", want: "image/webp"},
		{path: "photo.bmp", want: "image/bmp"},
		{path: "photo.txt", want: ""},
	}

	for _, tc := range cases {
		if got := mimeTypeFromPath(tc.path); got != tc.want {
			t.Fatalf("mimeTypeFromPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestLikelihoodToFloat(t *testing.T) {
	cases := []struct {
		input string
		want  float64
	}{
		{input: "VERY_UNLIKELY", want: 0.1},
		{input: "UNLIKELY", want: 0.3},
		{input: "POSSIBLE", want: 0.5},
		{input: "LIKELY", want: 0.7},
		{input: "VERY_LIKELY", want: 0.9},
		{input: "UNKNOWN", want: 0},
	}

	for _, tc := range cases {
		if got := likelihoodToFloat(tc.input); got != tc.want {
			t.Fatalf("likelihoodToFloat(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestParseFPS(t *testing.T) {
	if got := parseFPS("30000/1001"); got < 29.9 || got > 30.0 {
		t.Fatalf("expected NTSC-ish fps, got %v", got)
	}
	if got := parseFPS("24"); got != 24 {
		t.Fatalf("expected 24 fps, got %v", got)
	}
	if got := parseFPS("invalid"); got != 0 {
		t.Fatalf("expected invalid input to parse as 0, got %v", got)
	}
}

func TestAudioAnalyzerHelpers(t *testing.T) {
	analyzer := NewAudioAnalyzer()

	if got := analyzer.calcSilenceRatio([]SilenceSegment{{Duration: 2}, {Duration: 3}}, 10); got != 0.5 {
		t.Fatalf("expected silence ratio 0.5, got %v", got)
	}
	if got := analyzer.calcSilenceRatio(nil, 0); got != 0 {
		t.Fatalf("expected zero silence ratio for zero duration, got %v", got)
	}

	if got := analyzer.calcEnergyVariance([]float64{1, 2, 3}); got <= 0 {
		t.Fatalf("expected positive variance, got %v", got)
	}
	if got := analyzer.calcEnergyVariance([]float64{1}); got != 0 {
		t.Fatalf("expected zero variance for one sample, got %v", got)
	}
}

func TestJSONUnmarshal(t *testing.T) {
	var payload struct {
		Name string `json:"name"`
	}
	if err := jsonUnmarshal([]byte(`{"name":"vision"}`), &payload); err != nil {
		t.Fatalf("jsonUnmarshal: %v", err)
	}
	if payload.Name != "vision" {
		t.Fatalf("expected parsed name, got %q", payload.Name)
	}

	if err := jsonUnmarshal([]byte(`{invalid}`), &payload); err == nil {
		t.Fatal("expected invalid JSON to fail")
	}

	encoded, err := json.Marshal(payload)
	if err != nil || len(encoded) == 0 {
		t.Fatalf("expected payload to remain JSON-marshalable, err=%v", err)
	}
}
