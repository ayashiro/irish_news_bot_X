package news

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xapp/internal/config"
)

func TestFetchAndTranslate_Success(t *testing.T) {
	promptPath := filepath.Join(t.TempDir(), "news_prompt.txt")
	if err := os.WriteFile(promptPath, []byte("Summarize Ireland news for {{TODAY}}."), 0o600); err != nil {
		t.Fatalf("failed to write prompt file: %v", err)
	}

	var gotAPIKey string
	var gotPath string
	var gotReq generateContentRequest

	geminiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("x-goog-api-key")
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"candidates": [{
				"content": {
					"parts": [{"text": "ダブリン空港で強風による遅延が発生しています。出典: RTÉ News #アイルランド\n---\nLuas の運賃改定が発表されました。出典: The Irish Times #アイルランド"}]
				}
			}]
		}`))
	}))
	defer geminiSrv.Close()

	f := New(config.Config{
		GeminiAPIKey: "test-gemini-key",
		GeminiModel:  "gemini-4-hydrogen",
		PromptFile:   promptPath,
	})
	f.baseURL = geminiSrv.URL
	f.nowFunc = func() time.Time { return time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC) }

	got, err := f.FetchAndTranslate(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAPIKey != "test-gemini-key" {
		t.Fatalf("expected x-goog-api-key header, got %q", gotAPIKey)
	}
	if gotPath != "/gemini-4-hydrogen:generateContent" {
		t.Fatalf("unexpected path: %q", gotPath)
	}
	if len(gotReq.Tools) != 1 || gotReq.Tools[0].GoogleSearch == nil {
		t.Fatalf("expected googleSearch tool in request: %+v", gotReq.Tools)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 posts, got %d: %#v", len(got), got)
	}
	if !strings.Contains(got[0], "ダブリン空港") || !strings.Contains(got[1], "Luas") {
		t.Fatalf("unexpected posts: %#v", got)
	}
}
