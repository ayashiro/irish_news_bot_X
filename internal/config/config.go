package config

import (
	"fmt"
	"os"
	"strings"
)

const (
	defaultGeminiModel = "gemini-4-hydrogen"
	defaultPromptFile  = "prompts/news_prompt.txt"
)

// Config は Cloud Run Job の実行設定を保持する。
type Config struct {
	XAPIKey            string
	XAPIKeySecret      string
	XAccessToken       string
	XAccessTokenSecret string
	GeminiAPIKey       string
	GeminiModel        string
	PromptFile         string
	DryRun             bool
	TaskIndex          string
	ExecutionName      string
}

// Load は環境変数から設定を読み込み、必須項目を検証する。
func Load() (Config, error) {
	dryRunVal := strings.ToLower(strings.TrimSpace(os.Getenv("DRY_RUN")))
	dryRun := dryRunVal == "1" || dryRunVal == "true" || dryRunVal == "yes"

	cfg := Config{
		XAPIKey:            strings.TrimSpace(os.Getenv("X_API_KEY")),
		XAPIKeySecret:      strings.TrimSpace(os.Getenv("X_API_KEY_SECRET")),
		XAccessToken:       strings.TrimSpace(os.Getenv("X_ACCESS_TOKEN")),
		XAccessTokenSecret: strings.TrimSpace(os.Getenv("X_ACCESS_TOKEN_SECRET")),
		GeminiAPIKey:       strings.TrimSpace(os.Getenv("GEMINI_API_KEY")),
		GeminiModel:        envOrDefault("GEMINI_MODEL", defaultGeminiModel),
		PromptFile:         envOrDefault("PROMPT_FILE", defaultPromptFile),
		DryRun:             dryRun,
		TaskIndex:          envOrDefault("CLOUD_RUN_TASK_INDEX", "0"),
		ExecutionName:      envOrDefault("CLOUD_RUN_EXECUTION", "local"),
	}

	var missing []string
	if cfg.GeminiAPIKey == "" {
		missing = append(missing, "GEMINI_API_KEY")
	}

	if !cfg.DryRun {
		for _, pair := range []struct {
			name string
			val  string
		}{
			{"X_API_KEY", cfg.XAPIKey},
			{"X_API_KEY_SECRET", cfg.XAPIKeySecret},
			{"X_ACCESS_TOKEN", cfg.XAccessToken},
			{"X_ACCESS_TOKEN_SECRET", cfg.XAccessTokenSecret},
		} {
			if pair.val == "" {
				missing = append(missing, pair.name)
			}
		}
	}

	if len(missing) > 0 {
		return Config{}, fmt.Errorf("必須の環境変数が設定されていません: %s", strings.Join(missing, ", "))
	}

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
