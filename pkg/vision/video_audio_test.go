package vision

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestAppendKeyFrameToSceneMutatesOriginalScene(t *testing.T) {
	scenes := []SceneInfo{
		{ID: 7, Start: 0, End: 5, Duration: 5},
		{ID: 8, Start: 5, End: 10, Duration: 5},
	}

	first := KeyFrame{Index: 0, SceneID: 7, Timestamp: 2.5}
	second := KeyFrame{Index: 1, SceneID: 8, Timestamp: 7.5}

	appendKeyFrameToScene(scenes, 0, first)
	appendKeyFrameToScene(scenes, 1, second)

	if len(scenes[0].KeyFrames) != 1 {
		t.Fatalf("expected first scene to keep 1 key frame, got %d", len(scenes[0].KeyFrames))
	}
	if len(scenes[1].KeyFrames) != 1 {
		t.Fatalf("expected second scene to keep 1 key frame, got %d", len(scenes[1].KeyFrames))
	}
	if scenes[0].KeyFrames[0].SceneID != 7 {
		t.Fatalf("expected first scene key frame scene id 7, got %d", scenes[0].KeyFrames[0].SceneID)
	}
	if scenes[1].KeyFrames[0].SceneID != 8 {
		t.Fatalf("expected second scene key frame scene id 8, got %d", scenes[1].KeyFrames[0].SceneID)
	}
}

func TestAppendKeyFrameToSceneIgnoresOutOfRangeIndex(t *testing.T) {
	scenes := []SceneInfo{{ID: 1}}
	appendKeyFrameToScene(scenes, -1, KeyFrame{SceneID: 1})
	appendKeyFrameToScene(scenes, 1, KeyFrame{SceneID: 1})

	if len(scenes[0].KeyFrames) != 0 {
		t.Fatalf("expected no key frames to be appended for invalid indices, got %d", len(scenes[0].KeyFrames))
	}
}

func TestValidateFrameIntervalSecondsRejectsNonPositiveValues(t *testing.T) {
	cases := []float64{0, -1, -0.5}
	for _, interval := range cases {
		err := validateFrameIntervalSeconds(interval)
		if err == nil {
			t.Fatalf("expected interval %v to be rejected", interval)
		}
		if !strings.Contains(err.Error(), "intervalSeconds must be > 0") {
			t.Fatalf("expected interval validation error, got %v", err)
		}
	}
}

func TestValidateFrameIntervalSecondsAcceptsPositiveValue(t *testing.T) {
	if err := validateFrameIntervalSeconds(0.25); err != nil {
		t.Fatalf("expected positive interval to be accepted, got %v", err)
	}
}

func TestKeyFrameExtractorUsesCommandOutput(t *testing.T) {
	withFakeVisionCommands(t)

	extractor := NewKeyFrameExtractor()
	extractor.SetFFmpegPath("fake-ffmpeg")
	extractor.SetFFprobePath("fake-ffprobe")
	extractor.SetSceneThreshold(25)
	extractor.SetMaxKeyFrames(2)

	result, err := extractor.ExtractKeyFrames(context.Background(), []byte("video"))
	if err != nil {
		t.Fatalf("ExtractKeyFrames: %v", err)
	}
	if result.Duration != 10 {
		t.Fatalf("expected duration 10, got %v", result.Duration)
	}
	if len(result.Scenes) != 3 {
		t.Fatalf("expected 3 scenes, got %d", len(result.Scenes))
	}
	if len(result.KeyFrames) != 2 {
		t.Fatalf("expected max 2 key frames, got %d", len(result.KeyFrames))
	}
	if len(result.Scenes[0].KeyFrames) != 1 {
		t.Fatalf("expected first scene to keep a key frame, got %d", len(result.Scenes[0].KeyFrames))
	}
}

func TestKeyFrameExtractorExtractsFramesWithCommandOutput(t *testing.T) {
	withFakeVisionCommands(t)

	extractor := NewKeyFrameExtractor()
	extractor.SetFFmpegPath("fake-ffmpeg")
	extractor.SetFFprobePath("fake-ffprobe")

	frame, err := extractor.ExtractFrameAt(context.Background(), []byte("video"), 3.5)
	if err != nil {
		t.Fatalf("ExtractFrameAt: %v", err)
	}
	if string(frame) != "JPEG-FRAME" {
		t.Fatalf("expected fake frame bytes, got %q", string(frame))
	}

	frames, err := extractor.ExtractFramesAtIntervals(context.Background(), []byte("video"), 4)
	if err != nil {
		t.Fatalf("ExtractFramesAtIntervals: %v", err)
	}
	if len(frames) != 3 {
		t.Fatalf("expected 3 interval frames, got %d", len(frames))
	}
}

func TestKeyFrameExtractorReportsMissingFFmpeg(t *testing.T) {
	extractor := NewKeyFrameExtractor()
	extractor.SetFFmpegPath("definitely-missing-ffmpeg-binary")

	if _, err := extractor.ExtractKeyFrames(context.Background(), []byte("video")); err == nil {
		t.Fatal("expected missing ffmpeg error")
	}
	if _, err := extractor.ExtractFrameAt(context.Background(), []byte("video"), 1); err == nil {
		t.Fatal("expected missing ffmpeg error")
	}
	if _, err := extractor.ExtractFramesAtIntervals(context.Background(), []byte("video"), 1); err == nil {
		t.Fatal("expected missing ffmpeg error")
	}
}

func TestAudioAnalyzerUsesCommandOutput(t *testing.T) {
	withFakeVisionCommands(t)

	analyzer := NewAudioAnalyzer()
	analyzer.SetFFmpegPath("fake-ffmpeg")
	analyzer.SetFFprobePath("fake-ffprobe")

	result, err := analyzer.Analyze(context.Background(), []byte("audio"))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if result.Duration != 12.5 {
		t.Fatalf("expected duration 12.5, got %v", result.Duration)
	}
	if result.SampleRate != 44100 {
		t.Fatalf("expected sample rate 44100, got %d", result.SampleRate)
	}
	if result.Channels != 2 {
		t.Fatalf("expected 2 channels, got %d", result.Channels)
	}
	if result.Metadata == nil {
		t.Fatal("expected metadata to be populated")
	}
}

func TestAudioAnalyzerReportsMissingFFmpeg(t *testing.T) {
	analyzer := NewAudioAnalyzer()
	analyzer.SetFFmpegPath("definitely-missing-ffmpeg-binary")

	if analyzer.isFFmpegAvailable(context.Background()) {
		t.Fatal("expected missing ffmpeg to be unavailable")
	}
	if _, err := analyzer.Analyze(context.Background(), []byte("audio")); err == nil {
		t.Fatal("expected missing ffmpeg error")
	}
}

func TestVisionTempFileWriters(t *testing.T) {
	keyExtractor := NewKeyFrameExtractor()
	videoPath, err := keyExtractor.writeTempFile([]byte("video-data"), "unit")
	if err != nil {
		t.Fatalf("video writeTempFile: %v", err)
	}
	defer os.Remove(videoPath)

	videoData, err := os.ReadFile(videoPath)
	if err != nil {
		t.Fatalf("read video temp file: %v", err)
	}
	if string(videoData) != "video-data" {
		t.Fatalf("expected video temp contents, got %q", string(videoData))
	}

	audioAnalyzer := NewAudioAnalyzer()
	audioPath, err := audioAnalyzer.writeTempFile([]byte("audio-data"), "unit")
	if err != nil {
		t.Fatalf("audio writeTempFile: %v", err)
	}
	defer os.Remove(audioPath)

	audioData, err := os.ReadFile(audioPath)
	if err != nil {
		t.Fatalf("read audio temp file: %v", err)
	}
	if string(audioData) != "audio-data" {
		t.Fatalf("expected audio temp contents, got %q", string(audioData))
	}
}

func withFakeVisionCommands(t *testing.T) {
	t.Helper()

	previous := execCommandContext
	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		helperArgs := []string{"-test.run=TestVisionCommandHelperProcess", "--", name}
		helperArgs = append(helperArgs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], helperArgs...)
		cmd.Env = append(os.Environ(), "GO_WANT_VISION_HELPER_PROCESS=1")
		return cmd
	}

	t.Cleanup(func() {
		execCommandContext = previous
	})
}

func TestVisionCommandHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_VISION_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	separator := -1
	for i, arg := range args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator == -1 || separator+1 >= len(args) {
		os.Exit(2)
	}

	commandName := args[separator+1]
	commandArgs := args[separator+2:]
	joinedArgs := strings.Join(commandArgs, " ")

	switch {
	case strings.Contains(commandName, "ffprobe"):
		if strings.Contains(joinedArgs, "audio") {
			fmt.Fprint(os.Stdout, fakeAudioProbeJSON)
		} else {
			fmt.Fprint(os.Stdout, fakeVideoProbeJSON)
		}
		os.Exit(0)
	case strings.Contains(commandName, "ffmpeg"):
		switch {
		case strings.Contains(joinedArgs, "-version"):
			os.Exit(0)
		case strings.Contains(joinedArgs, "select='gt(scene"):
			fmt.Fprint(os.Stdout, "pts_time:2.000\npts_time:5.000\n")
			os.Exit(0)
		case strings.Contains(joinedArgs, "-vframes"):
			fmt.Fprint(os.Stdout, "JPEG-FRAME")
			os.Exit(0)
		case strings.Contains(joinedArgs, "silencedetect"):
			fmt.Fprint(os.Stdout, "silence_start: 1.000\nsilence_end: 3.000 | silence_duration: 2.000\n")
			os.Exit(0)
		case strings.Contains(joinedArgs, "highpass=f=300"):
			fmt.Fprint(os.Stdout, "RMS level dB = -20\nRMS level dB = -18\n")
			os.Exit(0)
		case strings.Contains(joinedArgs, "highpass=f=20"):
			fmt.Fprint(os.Stdout, "RMS level dB = -12\nRMS level dB = -10\n")
			os.Exit(0)
		case strings.Contains(joinedArgs, "astats=metadata=1:reset=1"):
			fmt.Fprint(os.Stdout, "RMS level dB = -20\nRMS level dB = -10\n")
			os.Exit(0)
		}
	}

	os.Exit(2)
}

const fakeVideoProbeJSON = `{
  "streams": [
    {
      "codec_type": "video",
      "codec_name": "h264",
      "width": 1920,
      "height": 1080,
      "r_frame_rate": "30000/1001",
      "nb_frames": "300"
    }
  ],
  "format": {
    "duration": "10.0",
    "size": "1024"
  }
}`

const fakeAudioProbeJSON = `{
  "streams": [
    {
      "codec_type": "audio",
      "codec_name": "aac",
      "sample_rate": "44100",
      "channels": 2,
      "bit_rate": "128000"
    }
  ],
  "format": {
    "duration": "12.5",
    "bit_rate": "128000"
  }
}`
