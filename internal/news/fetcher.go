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

const (
	defaultGeminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/models"
	xMaxWeight           = 280
	xURLWeight           = 23
)

// Fetcher fetches latest Irish news via Gemini API with Google Search grounding
// and returns standalone Japanese posts with resolved article URLs.
type Fetcher struct {
	apiKey         string
	model          string
	promptFile     string
	baseURL        string
	httpClient     *http.Client
	redirectClient *http.Client
	nowFunc        func() time.Time
}

// New creates a new news Fetcher from configuration.
func New(cfg config.Config) *Fetcher {
	return &Fetcher{
		apiKey:     cfg.GeminiAPIKey,
		model:      cfg.GeminiModel,
		promptFile: cfg.PromptFile,
		baseURL:    defaultGeminiBaseURL,
		httpClient: &http.Client{
			Timeout: 240 * time.Second,
		},
		redirectClient: &http.Client{
			Timeout: 8 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
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

type groundingChunk struct {
	Web *struct {
		URI   string `json:"uri"`
		Title string `json:"title"`
	} `json:"web,omitempty"`
}

type groundingSupport struct {
	Segment struct {
		StartIndex int    `json:"startIndex"`
		EndIndex   int    `json:"endIndex"`
		Text       string `json:"text"`
	} `json:"segment"`
	GroundingChunkIndices []int `json:"groundingChunkIndices"`
}

type groundingMetadata struct {
	GroundingChunks   []groundingChunk   `json:"groundingChunks"`
	GroundingSupports []groundingSupport `json:"groundingSupports"`
}

type generateContentResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		GroundingMetadata *groundingMetadata `json:"groundingMetadata,omitempty"`
	} `json:"candidates"`
}

type postSpan struct {
	text       string
	startIndex int
	endIndex   int
}

// FetchAndTranslate searches for the latest Irish news and returns standalone
// Japanese posts with resolved article URLs appended when available.
func (f *Fetcher) FetchAndTranslate(ctx context.Context) ([]string, error) {
	rawPrompt, err := os.ReadFile(f.promptFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read prompt file (%s): %w", f.promptFile, err)
	}
	today := f.nowFunc().UTC().Format("2006-01-02")
	prompt := strings.ReplaceAll(strings.TrimSpace(string(rawPrompt)), "{{TODAY}}", today)
	if prompt == "" {
		return nil, fmt.Errorf("prompt file (%s) is empty", f.promptFile)
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
		return nil, fmt.Errorf("failed to marshal Gemini request JSON: %w", err)
	}

	endpoint := fmt.Sprintf("%s/%s:generateContent", strings.TrimRight(f.baseURL, "/"), f.model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", f.apiKey)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Gemini API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Gemini response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Gemini API returned error (status=%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed generateContentResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode Gemini response JSON: %w", err)
	}

	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("Gemini API returned no candidate text")
	}

	var b strings.Builder
	for _, p := range parsed.Candidates[0].Content.Parts {
		b.WriteString(p.Text)
	}

	fullText := b.String()
	spans := splitPostSpans(fullText)
	if len(spans) == 0 {
		return nil, fmt.Errorf("Gemini API returned empty text")
	}

	meta := parsed.Candidates[0].GroundingMetadata
	usedChunk := make(map[int]bool)
	posts := make([]string, 0, len(spans))

	for _, sp := range spans {
		rawURI := findGroundingURI(sp, meta, usedChunk)
		resolvedURL := ""
		if rawURI != "" {
			resolvedURL = f.resolveRedirectURL(ctx, rawURI)
		}
		posts = append(posts, formatPostWithURL(sp.text, resolvedURL))
	}

	return posts, nil
}

// splitPostSpans splits the model response on lines containing only "---"
// while tracking each post's UTF-8 byte offsets in the original string.
func splitPostSpans(raw string) []postSpan {
	var spans []postSpan
	pos := 0
	blockStart := 0
	var current []string

	flush := func(end int) {
		text := strings.TrimSpace(strings.Join(current, "\n"))
		current = nil
		if text != "" {
			spans = append(spans, postSpan{
				text:       text,
				startIndex: blockStart,
				endIndex:   end,
			})
		}
	}

	for pos <= len(raw) {
		nl := strings.IndexByte(raw[pos:], '\n')
		var line string
		var nextPos int
		if nl == -1 {
			line = raw[pos:]
			nextPos = len(raw) + 1
		} else {
			line = raw[pos : pos+nl]
			nextPos = pos + nl + 1
		}

		trimmedLine := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if trimmedLine == "---" {
			flush(pos)
			blockStart = nextPos
		} else {
			if len(current) == 0 {
				blockStart = pos
			}
			current = append(current, strings.TrimSuffix(line, "\r"))
		}
		pos = nextPos
	}
	flush(len(raw))

	return spans
}

// findGroundingURI finds the best matching grounding chunk URI for a post span.
func findGroundingURI(sp postSpan, meta *groundingMetadata, usedChunk map[int]bool) string {
	if meta == nil || len(meta.GroundingChunks) == 0 {
		return ""
	}

	// First pass: match groundingSupports whose byte range overlaps this post span.
	for _, sup := range meta.GroundingSupports {
		if sup.Segment.EndIndex <= sp.startIndex || sup.Segment.StartIndex >= sp.endIndex {
			continue
		}
		for _, idx := range sup.GroundingChunkIndices {
			if idx >= 0 && idx < len(meta.GroundingChunks) && !usedChunk[idx] {
				if chunk := meta.GroundingChunks[idx]; chunk.Web != nil && chunk.Web.URI != "" {
					usedChunk[idx] = true
					return chunk.Web.URI
				}
			}
		}
	}

	// Second pass: allow reusing an overlapping chunk if all overlapping chunks were already used.
	for _, sup := range meta.GroundingSupports {
		if sup.Segment.EndIndex <= sp.startIndex || sup.Segment.StartIndex >= sp.endIndex {
			continue
		}
		for _, idx := range sup.GroundingChunkIndices {
			if idx >= 0 && idx < len(meta.GroundingChunks) {
				if chunk := meta.GroundingChunks[idx]; chunk.Web != nil && chunk.Web.URI != "" {
					return chunk.Web.URI
				}
			}
		}
	}

	// Fallback: pick the next unused chunk in order.
	for idx, chunk := range meta.GroundingChunks {
		if !usedChunk[idx] && chunk.Web != nil && chunk.Web.URI != "" {
			usedChunk[idx] = true
			return chunk.Web.URI
		}
	}

	return ""
}

// resolveRedirectURL follows Vertex AI grounding redirect headers to obtain
// the canonical article URL without fetching the target news site.
func (f *Fetcher) resolveRedirectURL(ctx context.Context, rawURI string) string {
	current := rawURI
	for range 3 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			break
		}
		resp, err := f.redirectClient.Do(req)
		if err != nil {
			break
		}
		_ = resp.Body.Close()

		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			loc := strings.TrimSpace(resp.Header.Get("Location"))
			if loc == "" {
				break
			}
			nextURL, err := req.URL.Parse(loc)
			if err != nil {
				break
			}
			current = nextURL.String()
			if !strings.Contains(current, "vertexaisearch.cloud.google.com/grounding-api-redirect/") {
				return current
			}
			continue
		}
		break
	}

	// Do not attach unresolved Google grounding redirect URLs.
	if strings.Contains(current, "vertexaisearch.cloud.google.com/grounding-api-redirect/") {
		return ""
	}
	return current
}

// formatPostWithURL appends the article URL on a new line while keeping the
// total X weighted character count within xMaxWeight (280).
func formatPostWithURL(body, articleURL string) string {
	if articleURL == "" {
		return truncateToXLimit(body, xMaxWeight)
	}
	// Reserve 1 weight for "\n" and 23 weights for X's t.co URL length.
	maxBodyWeight := xMaxWeight - 1 - xURLWeight
	return truncateToXLimit(body, maxBodyWeight) + "\n" + articleURL
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
