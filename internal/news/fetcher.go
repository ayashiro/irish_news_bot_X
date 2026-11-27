package news

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"xapp/internal/config"
)

const defaultGeminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/models"

// Fetcher は Gemini API（Google 検索グラウンディング）を使って
// アイルランドの最新ニュースを取得し、日本語の投稿文を生成する。
type Fetcher struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
	nowFunc    func() time.Time
}

// New は設定からニュース取得クライアントを生成する。
func New(cfg config.Config) *Fetcher {
	return &Fetcher{
		apiKey:  cfg.GeminiAPIKey,
		model:   cfg.GeminiModel,
		baseURL: defaultGeminiBaseURL,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
		nowFunc: time.Now,
	}
}

type generateContentRequest struct {
	Contents         []content         `json:"contents"`
	Tools            []tool            `json:"tools,omitempty"`
	GenerationConfig *generationConfig `json:"generationConfig,omitempty"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type part struct {
	Text string `json:"text,omitempty"`
}

type tool struct {
	GoogleSearch *struct{} `json:"googleSearch,omitempty"`
}

type generationConfig struct {
	Temperature float64 `json:"temperature,omitempty"`
}

type generateContentResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// FetchAndTranslate はアイルランドの最新ニュースを検索・要約し、
// X 投稿用の日本語テキストを返す。
func (f *Fetcher) FetchAndTranslate(ctx context.Context) (string, error) {
	today := f.nowFunc().UTC().Format("2006-01-02")
	prompt := fmt.Sprintf(`Google 検索を使って、今日（%s）または直近24時間以内のアイルランドの最新ニュース（RTÉ News、The Irish Times、Irish Independent、TheJournal.ie などの主要メディア）を1件選んでください。

選んだニュースを日本の読者向けに自然な日本語で要約し、そのまま X（旧Twitter）に投稿できるテキストを作成してください。

厳守事項:
- 全体を日本語の全角130文字以内に収めること（ハッシュタグ・出典名を含む）
- 記事の要点をわかりやすく簡潔にまとめること
- 末尾に出典メディア名（例: 出典: RTÉ News）とハッシュタグ（#アイルランド）を入れること
- 「以下が要約です」などの前置き、引用符、Markdown記法（**や*など）、URLは一切出力しないこと
- 投稿本文のみを出力すること`, today)

	reqBody := generateContentRequest{
		Contents: []content{
			{
				Role:  "user",
				Parts: []part{{Text: prompt}},
			},
		},
		Tools: []tool{
			{GoogleSearch: &struct{}{}},
		},
		GenerationConfig: &generationConfig{
			Temperature: 0.4,
		},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("Gemini リクエスト JSON の生成に失敗しました: %w", err)
	}

	endpoint := fmt.Sprintf("%s/%s:generateContent", strings.TrimRight(f.baseURL, "/"), f.model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("Gemini HTTP リクエストの作成に失敗しました: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", f.apiKey)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Gemini API へのリクエストに失敗しました: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("Gemini レスポンスの読み込みに失敗しました: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Gemini API がエラーを返しました (status=%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed generateContentResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("Gemini レスポンス JSON の解析に失敗しました: %w", err)
	}

	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("Gemini から有効なテキストが返されませんでした")
	}

	var b strings.Builder
	for _, p := range parsed.Candidates[0].Content.Parts {
		b.WriteString(p.Text)
	}

	text := strings.TrimSpace(b.String())
	if text == "" {
		return "", fmt.Errorf("Gemini から空のテキストが返されませんでした")
	}

	return truncateToXLimit(text, 280), nil
}

// truncateToXLimit は X の文字数ウェイト（ASCII は 1、日本語などの非 ASCII は 2）で
// maxWeight 以内に安全に収める。
func truncateToXLimit(s string, maxWeight int) string {
	weight := 0
	for i, r := range s {
		w := 2
		if r < utf8.RuneSelf {
			w = 1
		}
		if weight+w > maxWeight {
			// 末尾に「…」を付ける余裕を残して切り詰める
			return strings.TrimSpace(s[:i])
		}
		weight += w
	}
	return s
}
