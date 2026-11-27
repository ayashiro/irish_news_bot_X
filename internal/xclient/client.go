package xclient

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"xapp/internal/config"
)

const defaultTweetsEndpoint = "https://api.x.com/2/tweets"

// Client posts tweets and threads via X API v2 using OAuth 1.0a.
type Client struct {
	apiKey            string
	apiKeySecret      string
	accessToken       string
	accessTokenSecret string
	dryRun            bool
	endpoint          string
	httpClient        *http.Client
	nowFunc           func() time.Time
}

// New creates a new X API client from configuration.
func New(cfg config.Config) *Client {
	return &Client{
		apiKey:            cfg.XAPIKey,
		apiKeySecret:      cfg.XAPIKeySecret,
		accessToken:       cfg.XAccessToken,
		accessTokenSecret: cfg.XAccessTokenSecret,
		dryRun:            cfg.DryRun,
		endpoint:          defaultTweetsEndpoint,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		nowFunc: time.Now,
	}
}

type tweetReply struct {
	InReplyToTweetID string `json:"in_reply_to_tweet_id"`
}

type createTweetRequest struct {
	Text  string      `json:"text"`
	Reply *tweetReply `json:"reply,omitempty"`
}

type createTweetResponse struct {
	Data struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	} `json:"data"`
}

// PostTweet posts a single tweet to X and returns the created tweet ID.
// When DryRun is true, it skips the API call and returns an empty string.
func (c *Client) PostTweet(ctx context.Context, text string) (string, error) {
	return c.post(ctx, text, "")
}

// PostThread posts multiple texts as a connected X thread by replying to each
// previous tweet ID. When DryRun is true, it skips all API calls and returns nil.
func (c *Client) PostThread(ctx context.Context, texts []string) ([]string, error) {
	if c.dryRun || len(texts) == 0 {
		return nil, nil
	}

	ids := make([]string, 0, len(texts))
	var replyToID string
	for i, text := range texts {
		id, err := c.post(ctx, text, replyToID)
		if err != nil {
			return ids, fmt.Errorf("failed to post tweet %d/%d in thread: %w", i+1, len(texts), err)
		}
		ids = append(ids, id)
		replyToID = id
	}
	return ids, nil
}

func (c *Client) post(ctx context.Context, text, inReplyToTweetID string) (string, error) {
	if c.dryRun {
		return "", nil
	}

	reqPayload := createTweetRequest{Text: text}
	if inReplyToTweetID != "" {
		reqPayload.Reply = &tweetReply{InReplyToTweetID: inReplyToTweetID}
	}

	payload, err := json.Marshal(reqPayload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal tweet request JSON: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("failed to create X API HTTP request: %w", err)
	}

	authHeader, err := c.buildOAuthHeader(http.MethodPost, c.endpoint)
	if err != nil {
		return "", fmt.Errorf("failed to generate OAuth signature: %w", err)
	}

	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call X API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read X API response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("X API returned error (status=%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed createTweetResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("failed to decode X API response JSON: %w", err)
	}

	return parsed.Data.ID, nil
}

func (c *Client) buildOAuthHeader(method, rawURL string) (string, error) {
	nonce, err := generateNonce()
	if err != nil {
		return "", err
	}
	timestamp := strconv.FormatInt(c.nowFunc().Unix(), 10)

	oauthParams := map[string]string{
		"oauth_consumer_key":     c.apiKey,
		"oauth_nonce":            nonce,
		"oauth_signature_method": "HMAC-SHA1",
		"oauth_timestamp":        timestamp,
		"oauth_token":            c.accessToken,
		"oauth_version":          "1.0",
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	baseURL := parsedURL.Scheme + "://" + parsedURL.Host + parsedURL.Path

	// Per RFC 5849, combine query parameters and oauth_* parameters for the signature base string.
	// JSON request bodies (Content-Type: application/json) are excluded from the signature.
	type kv struct{ k, v string }
	var pairs []kv
	for k, v := range oauthParams {
		pairs = append(pairs, kv{percentEncode(k), percentEncode(v)})
	}
	for k, vals := range parsedURL.Query() {
		for _, v := range vals {
			pairs = append(pairs, kv{percentEncode(k), percentEncode(v)})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].k == pairs[j].k {
			return pairs[i].v < pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})

	encodedParams := make([]string, 0, len(pairs))
	for _, p := range pairs {
		encodedParams = append(encodedParams, p.k+"="+p.v)
	}

	baseString := strings.Join([]string{
		strings.ToUpper(method),
		percentEncode(baseURL),
		percentEncode(strings.Join(encodedParams, "&")),
	}, "&")

	signingKey := percentEncode(c.apiKeySecret) + "&" + percentEncode(c.accessTokenSecret)
	mac := hmac.New(sha1.New, []byte(signingKey))
	mac.Write([]byte(baseString))
	oauthParams["oauth_signature"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))

	keys := make([]string, 0, len(oauthParams))
	for k := range oauthParams {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	headerParts := make([]string, 0, len(keys))
	for _, k := range keys {
		headerParts = append(headerParts, fmt.Sprintf(`%s="%s"`, percentEncode(k), percentEncode(oauthParams[k])))
	}

	return "OAuth " + strings.Join(headerParts, ", "), nil
}

func generateNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// percentEncode applies RFC 3986 / RFC 5849 percent-encoding.
func percentEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') ||
			(c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
