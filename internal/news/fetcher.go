package news

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"xapp/internal/config"
)

const defaultGeminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/models"

// Fetcher fetches latest Irish news via Gemini API with Google Search grounding
// and returns a Japanese post text.
type Fetcher struct {
	apiKey     string
	model      string
	promptFile string
	baseURL    string
	httpClient *http.Client
	nowFunc    func() time.Time
}

// New creates a new news Fetcher from configuration.
func New(cfg config.Config) *Fetcher {
	return &Fetcher{
		apiKey:     cfg.GeminiAPIKey,
		model:      cfg.GeminiModel,
		promptFile: cfg.PromptFile,
		baseURL:    defaultGeminiBaseURL,
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

// FetchAndTranslate searches for the latest Irish news and returns a Japanese summary for X.
func (f *Fetcher) FetchAndTranslate(ctx context.Context) (string, error) {
	rawPrompt, err := os.ReadFile(f.promptFile)
	if err != nil {
		return "", fmt.Errorf("failed to read prompt file (%s): %w", f.promptFile, err)
	}
	today := f.nowFunc().UTC().Format("2006-01-02")
	prompt := strings.ReplaceAll(strings.TrimSpace(string(rawPrompt)), "{{TODAY}}", today)
	if prompt == "" {
		return "", fmt.Errorf("prompt file (%s) is empty", f.promptFile)
	}

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
		return "", fmt.Errorf("failed to marshal Gemini request JSON: %w", err)
	}

	endpoint := fmt.Sprintf("%s/%s:generateContent", strings.TrimRight(f.baseURL, "/"), f.model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("failed to create Gemini HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", f.apiKey)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call Gemini API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read Gemini response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Gemini API returned error (status=%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed generateContentResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("failed to decode Gemini response JSON: %w", err)
	}

	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("Gemini API returned no candidate text")
	}

	var b strings.Builder
	for _, p := range parsed.Candidates[0].Content.Parts {
		b.WriteString(p.Text)
	}

	text := strings.TrimSpace(b.String())
	if text == "" {
		return "", fmt.Errorf("Gemini API returned empty text")
	}

	return truncateToXLimit(text, 280), nil
}

// truncateToXLimit trims text to fit within X's weighted character limit
// (1 for ASCII, 2 for non-ASCII characters).
func truncateToXLimit(s string, maxWeight int) string {
	weight := 0
	for i, r := range s {
		w := 2
		if r < utf8.RuneSelf {
			w = 1
		}
		if weight+w > maxWeight {
			return strings.TrimSpace(s[:i])
		}
		weight += w
	}
	return s
}
