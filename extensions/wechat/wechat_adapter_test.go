package main

import (
	"crypto/sha1"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

func TestHandleWebhookRejectsUnsignedPost(t *testing.T) {
	ext := NewWeChatExtension(Config{Token: "secret"})
	req := httptest.NewRequest(http.MethodPost, "/wechat", strings.NewReader(`<xml></xml>`))
	rec := httptest.NewRecorder()

	ext.handleWebhook(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected unsigned POST to be forbidden, got %d", rec.Code)
	}
}

func TestHandleWebhookAcceptsSignedPost(t *testing.T) {
	token := "secret"
	timestamp := "1710000000"
	nonce := "nonce"
	signature := wechatTestSignature(token, timestamp, nonce)
	ext := NewWeChatExtension(Config{Token: token})
	req := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/wechat?signature=%s&timestamp=%s&nonce=%s", signature, timestamp, nonce),
		strings.NewReader(`<xml><MsgType>image</MsgType></xml>`),
	)
	rec := httptest.NewRecorder()

	ext.handleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected signed POST to be accepted, got %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "success" {
		t.Fatalf("expected success response, got %q", rec.Body.String())
	}
}

func wechatTestSignature(token, timestamp, nonce string) string {
	parts := []string{token, timestamp, nonce}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return fmt.Sprintf("%x", sum)
}
