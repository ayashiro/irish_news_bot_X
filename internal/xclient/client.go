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

// Client は X API v2 を使って投稿を行うクライアント（OAuth 1.0a）。
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

// New は設定から X API クライアントを生成する。
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

type createTweetRequest struct {
	Text string `json:"text"`
}

type createTweetResponse struct {
	Data struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	} `json:"data"`
}

// PostTweet はテキストを X に投稿し、成功時はツイート ID を返す。
// DryRun が true の場合は実際には投稿せず空文字を返す。
func (c *Client) PostTweet(ctx context.Context, text string) (string, error) {
	if c.dryRun {
		return "", nil
	}

	payload, err := json.Marshal(createTweetRequest{Text: text})
	if err != nil {
		return "", fmt.Errorf("リクエスト JSON の生成に失敗しました: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("HTTP リクエストの作成に失敗しました: %w", err)
	}

	authHeader, err := c.buildOAuthHeader(http.MethodPost, c.endpoint)
	if err != nil {
		return "", fmt.Errorf("OAuth 署名の生成に失敗しました: %w", err)
	}

	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("X API へのリクエストに失敗しました: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("レスポンスの読み込みに失敗しました: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("X API がエラーを返しました (status=%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed createTweetResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("レスポンス JSON の解析に失敗しました: %w", err)
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

	// RFC 5849 に従い、クエリパラメータと oauth_* パラメータを結合して署名ベース文字列を作る
	// （Content-Type: application/json のリクエストボディは署名対象に含めない）
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

// percentEncode は RFC 3986 / RFC 5849 準拠のパーセントエンコードを行う。
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
