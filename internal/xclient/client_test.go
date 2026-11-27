package xclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xapp/internal/config"
)

func TestPostTweet_DryRun(t *testing.T) {
	c := New(config.Config{DryRun: true})
	id, err := c.PostTweet(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "" {
		t.Fatalf("expected empty tweet id in dry run, got %q", id)
	}
}

func TestPostTweet_Success(t *testing.T) {
	var gotAuth string
	var gotContentType string
	var gotBody createTweetRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"id":"1234567890","text":"hello"}}`))
	}))
	defer srv.Close()

	c := New(config.Config{
		XAPIKey:            "test_key",
		XAPIKeySecret:      "test_key_secret",
		XAccessToken:       "test_token",
		XAccessTokenSecret: "test_token_secret",
		DryRun:             false,
	})
	c.endpoint = srv.URL
	c.nowFunc = func() time.Time { return time.Unix(1700000000, 0) }

	id, err := c.PostTweet(context.Background(), "hello ireland")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "1234567890" {
		t.Fatalf("expected tweet id 1234567890, got %q", id)
	}
	if gotContentType != "application/json" {
		t.Fatalf("expected application/json, got %q", gotContentType)
	}
	if gotBody.Text != "hello ireland" {
		t.Fatalf("expected text 'hello ireland', got %q", gotBody.Text)
	}
	if !strings.HasPrefix(gotAuth, "OAuth ") {
		t.Fatalf("expected OAuth Authorization header, got %q", gotAuth)
	}
	for _, want := range []string{
		`oauth_consumer_key="test_key"`,
		`oauth_token="test_token"`,
		`oauth_signature_method="HMAC-SHA1"`,
		`oauth_timestamp="1700000000"`,
		`oauth_version="1.0"`,
		`oauth_signature="`,
	} {
		if !strings.Contains(gotAuth, want) {
			t.Errorf("Authorization header missing %s: %s", want, gotAuth)
		}
	}
}
