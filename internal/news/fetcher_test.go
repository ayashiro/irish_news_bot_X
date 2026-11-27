package news

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xapp/internal/config"
)

func TestFetchAndTranslate_WithGroundingURLs(t *testing.T) {
	promptPath := filepath.Join(t.TempDir(), "news_prompt.txt")
	if err := os.WriteFile(promptPath, []byte("Summarize Ireland news for {{TODAY}}."), 0o600); err != nil {
		t.Fatalf("failed to write prompt file: %v", err)
	}

	// Mock redirect server simulating Vertex AI grounding redirect -> canonical article URL.
	redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/r1":
			http.Redirect(w, r, "https://www.rte.ie/news/article-1", http.StatusFound)
		case "/r2":
			http.Redirect(w, r, "https://www.irishtimes.com/news/article-2", http.StatusFound)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer redirectSrv.Close()

	post1 := "ダブリン空港で強風による遅延が発生しています。出典: RTÉ News #アイルランド"
	post2 := "Luas の運賃改定が発表されました。出典: The Irish Times #アイルランド"
	fullText := post1 + "\n---\n" + post2
	post2Start := len(post1) + len("\n---\n")

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
		respJSON := fmt.Sprintf(`{
			"candidates": [{
				"content": {
					"parts": [{"text": %q}]
				},
				"groundingMetadata": {
					"groundingChunks": [
						{"web": {"uri": %q, "title": "rte.ie"}},
						{"web": {"uri": %q, "title": "irishtimes.com"}}
					],
					"groundingSupports": [
						{"segment": {"startIndex": 0, "endIndex": %d}, "groundingChunkIndices": [0]},
						{"segment": {"startIndex": %d, "endIndex": %d}, "groundingChunkIndices": [1]}
					]
				}
			}]
		}`, fullText, redirectSrv.URL+"/r1", redirectSrv.URL+"/r2", len(post1), post2Start, len(fullText))
		_, _ = w.Write([]byte(respJSON))
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
	if !strings.HasSuffix(got[0], "\nhttps://www.rte.ie/news/article-1") {
		t.Fatalf("expected post 1 to end with rte.ie URL, got %q", got[0])
	}
	if !strings.HasSuffix(got[1], "\nhttps://www.irishtimes.com/news/article-2") {
		t.Fatalf("expected post 2 to end with irishtimes.com URL, got %q", got[1])
	}
}
