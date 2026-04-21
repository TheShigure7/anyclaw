package channels

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/anyclaw/pkg/config"
)

func TestSignalFindAudioAttachmentMatchesByMIMEWithoutURL(t *testing.T) {
	adapter := &SignalAdapter{}
	attachments := []struct {
		ContentType string `json:"contentType"`
		Filename    string `json:"filename"`
	}{
		{ContentType: "audio/ogg", Filename: ""},
	}

	audioURL, audioMIME, hasAudio := adapter.findAudioAttachment(attachments)
	if !hasAudio {
		t.Fatal("expected audio attachment to be detected")
	}
	if audioMIME != "audio/ogg" {
		t.Fatalf("expected audio MIME to be preserved, got %q", audioMIME)
	}
	if audioURL != "" {
		t.Fatalf("expected empty audio URL when attachment has no filename, got %q", audioURL)
	}
}

func TestTelegramSendMessageReturnsAPIErrorWhenOKFalse(t *testing.T) {
	adapter := &TelegramAdapter{
		baseURL: "https://telegram.example/bot-token",
		client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if !strings.HasSuffix(req.URL.String(), "/sendMessage") {
					t.Fatalf("unexpected request URL: %s", req.URL.String())
				}
				return jsonResponse(http.StatusOK, map[string]any{"ok": false, "description": "chat not found"}), nil
			}),
		},
	}

	err := adapter.sendMessage(context.Background(), "42", "hello")
	if err == nil {
		t.Fatal("expected telegram API error")
	}
	if !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("expected chat not found error, got %v", err)
	}
}

func TestTelegramPollOncePreservesOffsetOnHandlerFailure(t *testing.T) {
	adapter := NewTelegramAdapter(config.TelegramChannelConfig{
		Enabled:   true,
		BotToken:  "token",
		PollEvery: 1,
	}, nil)

	adapter.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case strings.Contains(req.URL.String(), "/getUpdates"):
				return jsonResponse(http.StatusOK, map[string]any{
					"ok": true,
					"result": []map[string]any{
						{
							"update_id": 7,
							"message": map[string]any{
								"text": "hello",
								"chat": map[string]any{"id": 42, "type": "private"},
								"from": map[string]any{"id": 99, "username": "alice"},
							},
						},
					},
				}), nil
			case strings.Contains(req.URL.String(), "/sendMessage"):
				return jsonResponse(http.StatusOK, map[string]any{"ok": true}), nil
			default:
				return nil, fmt.Errorf("unexpected request URL: %s", req.URL.String())
			}
		}),
	}

	calls := 0
	wantErr := fmt.Errorf("boom")
	err := adapter.pollOnce(context.Background(), func(ctx context.Context, sessionID string, message string, meta map[string]string) (string, string, error) {
		calls++
		return "", "", wantErr
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected handler error, got %v", err)
	}
	if adapter.offset != 0 {
		t.Fatalf("expected offset to remain unchanged after failure, got %d", adapter.offset)
	}

	err = adapter.pollOnce(context.Background(), func(ctx context.Context, sessionID string, message string, meta map[string]string) (string, string, error) {
		calls++
		return "session-1", "ok", nil
	})
	if err != nil {
		t.Fatalf("expected second poll to succeed, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected failed update to be retried, got %d calls", calls)
	}
	if adapter.offset != 8 {
		t.Fatalf("expected offset to advance after success, got %d", adapter.offset)
	}
}

func TestTelegramPollOnceStreamProcessesVoiceMessages(t *testing.T) {
	var outbound []string

	adapter := NewTelegramAdapter(config.TelegramChannelConfig{
		Enabled:   true,
		BotToken:  "token",
		PollEvery: 1,
	}, nil)

	adapter.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case strings.Contains(req.URL.String(), "/getUpdates"):
				return jsonResponse(http.StatusOK, map[string]any{
					"ok": true,
					"result": []map[string]any{
						{
							"update_id": 8,
							"message": map[string]any{
								"caption": "voice caption",
								"chat":    map[string]any{"id": 42, "type": "private"},
								"from":    map[string]any{"id": 99, "username": "alice"},
								"voice":   map[string]any{"file_id": "voice-1", "mime_type": "audio/ogg"},
							},
						},
					},
				}), nil
			case strings.Contains(req.URL.String(), "/getFile"):
				return jsonResponse(http.StatusOK, map[string]any{
					"ok":     true,
					"result": map[string]any{"file_id": "voice-1", "file_path": "voice/path.ogg"},
				}), nil
			case strings.Contains(req.URL.String(), "/sendMessage"):
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("read request body: %v", err)
				}
				outbound = append(outbound, string(body))
				return jsonResponse(http.StatusOK, map[string]any{"ok": true}), nil
			default:
				return nil, fmt.Errorf("unexpected request URL: %s", req.URL.String())
			}
		}),
	}

	err := adapter.pollOnceStream(context.Background(), func(ctx context.Context, sessionID string, message string, meta map[string]string, onChunk func(chunk string) error) (string, error) {
		if meta["message_type"] != "voice_note" {
			t.Fatalf("expected voice_note meta, got %+v", meta)
		}
		if meta["audio_url"] == "" {
			t.Fatalf("expected audio URL in meta, got %+v", meta)
		}
		if meta["caption"] != "voice caption" {
			t.Fatalf("expected caption to be preserved, got %+v", meta)
		}
		if err := onChunk("voice reply"); err != nil {
			return "", err
		}
		return "session-1", nil
	})
	if err != nil {
		t.Fatalf("pollOnceStream failed: %v", err)
	}
	if len(outbound) != 1 || !strings.Contains(outbound[0], "text=voice+reply") {
		t.Fatalf("expected voice reply to be sent once, got %+v", outbound)
	}
	if adapter.offset != 9 {
		t.Fatalf("expected offset to advance after streaming voice success, got %d", adapter.offset)
	}
}

func TestSignalPollOnceRetriesMessageUntilSendSucceeds(t *testing.T) {
	sendCalls := 0
	handleCalls := 0

	adapter := NewSignalAdapter(config.SignalChannelConfig{
		Enabled:   true,
		BaseURL:   "https://signal.example",
		Number:    "+1000",
		PollEvery: 1,
	}, nil)

	adapter.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case strings.Contains(req.URL.String(), "/v1/receive/"):
				return jsonResponse(http.StatusOK, []map[string]any{
					{
						"envelope": map[string]any{
							"timestamp":  123,
							"source":     "+2000",
							"sourceName": "bob",
							"dataMessage": map[string]any{
								"message": "hello",
							},
						},
					},
				}), nil
			case strings.Contains(req.URL.String(), "/v2/send"):
				sendCalls++
				if sendCalls == 1 {
					return jsonResponse(http.StatusBadGateway, map[string]any{"error": "fail"}), nil
				}
				return jsonResponse(http.StatusOK, map[string]any{"timestamp": 124}), nil
			default:
				return nil, fmt.Errorf("unexpected request URL: %s", req.URL.String())
			}
		}),
	}

	err := adapter.pollOnce(context.Background(), func(ctx context.Context, sessionID string, message string, meta map[string]string) (string, string, error) {
		handleCalls++
		return "session-1", "ok", nil
	})
	if err == nil {
		t.Fatal("expected first send failure")
	}
	if adapter.latestTS != 0 {
		t.Fatalf("expected latestTS to remain unchanged after send failure, got %d", adapter.latestTS)
	}

	err = adapter.pollOnce(context.Background(), func(ctx context.Context, sessionID string, message string, meta map[string]string) (string, string, error) {
		handleCalls++
		return "session-1", "ok", nil
	})
	if err != nil {
		t.Fatalf("expected second poll to succeed, got %v", err)
	}
	if handleCalls != 2 {
		t.Fatalf("expected message to be retried, got %d handler calls", handleCalls)
	}
	if adapter.latestTS != 123 {
		t.Fatalf("expected latestTS to advance after success, got %d", adapter.latestTS)
	}
}

func TestWhatsAppHandleInboundRetriesMessageUntilSendSucceeds(t *testing.T) {
	sendCalls := 0
	handleCalls := 0

	adapter := NewWhatsAppAdapter(config.WhatsAppChannelConfig{
		Enabled:       true,
		AccessToken:   "token",
		PhoneNumberID: "12345",
	}, nil)

	adapter.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			sendCalls++
			if sendCalls == 1 {
				return jsonResponse(http.StatusBadGateway, map[string]any{"error": "fail"}), nil
			}
			return jsonResponse(http.StatusOK, map[string]any{"messages": []map[string]any{{"id": "wamid.1"}}}), nil
		}),
	}

	_, _, err := adapter.HandleInbound(context.Background(), "+8613800000000", "hello", "wamid.1", "alice", func(ctx context.Context, sessionID string, message string, meta map[string]string) (string, string, error) {
		handleCalls++
		return "session-1", "ok", nil
	})
	if err == nil {
		t.Fatal("expected first send failure")
	}

	_, _, err = adapter.HandleInbound(context.Background(), "+8613800000000", "hello", "wamid.1", "alice", func(ctx context.Context, sessionID string, message string, meta map[string]string) (string, string, error) {
		handleCalls++
		return "session-1", "ok", nil
	})
	if err != nil {
		t.Fatalf("expected second inbound handling to succeed, got %v", err)
	}
	if handleCalls != 2 {
		t.Fatalf("expected inbound message to retry after failure, got %d handler calls", handleCalls)
	}
	if !adapter.hasSeen("wamid.1") {
		t.Fatal("expected message to be marked seen after successful send")
	}
}

func TestDiscordPollOnceRepliesToChannelInsteadOfParentMessageID(t *testing.T) {
	var postedURL string
	var postedBody string
	adapter := &DiscordAdapter{
		config: config.DiscordChannelConfig{
			DefaultChannel: "c1",
		},
		apiBaseURL: "https://discord.example/api/v10",
		client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.Method {
				case http.MethodGet:
					return jsonResponse(http.StatusOK, []map[string]any{
						{
							"id":         "m1",
							"channel_id": "c1",
							"content":    "hello",
							"guild_id":   "g1",
							"author":     map[string]any{"id": "u1", "username": "alice", "bot": false},
							"message_reference": map[string]any{
								"message_id": "parent-123",
								"channel_id": "c1",
							},
						},
					}), nil
				case http.MethodPost:
					body, err := io.ReadAll(req.Body)
					if err != nil {
						t.Fatalf("read request body: %v", err)
					}
					postedURL = req.URL.String()
					postedBody = string(body)
					return jsonResponse(http.StatusOK, map[string]any{"id": "reply-1"}), nil
				default:
					t.Fatalf("unexpected method: %s", req.Method)
					return nil, nil
				}
			}),
		},
		processed: make(map[string]time.Time),
	}

	err := adapter.pollOnce(context.Background(), func(ctx context.Context, sessionID string, message string, meta map[string]string) (string, string, error) {
		return "", "reply", nil
	})
	if err != nil {
		t.Fatalf("pollOnce failed: %v", err)
	}
	if !strings.Contains(postedURL, "/channels/c1/messages") {
		t.Fatalf("expected reply to post to channel c1, got %s", postedURL)
	}
	if !strings.Contains(postedBody, `"message_reference":{"message_id":"parent-123"}`) {
		t.Fatalf("expected reply to preserve parent message reference, got %s", postedBody)
	}
}
