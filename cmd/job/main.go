package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"xapp/internal/config"
	"xapp/internal/news"
	"xapp/internal/xclient"
)

func main() {
	// Use JSON handler for structured logs in Cloud Logging.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, logger); err != nil {
		logger.Error("job execution failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger.Info("starting job",
		"execution", cfg.ExecutionName,
		"task_index", cfg.TaskIndex,
		"gemini_model", cfg.GeminiModel,
		"dry_run", cfg.DryRun,
	)

	fetcher := news.New(cfg)
	postText, err := fetcher.FetchAndTranslate(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(postText) == "" {
		logger.Info("no news to post")
		return nil
	}

	client := xclient.New(cfg)
	tweetID, err := client.PostTweet(ctx, postText)
	if err != nil {
		return err
	}

	if cfg.DryRun {
		logger.Info("dry run enabled; skipped posting to X", "text", postText)
	} else {
		logger.Info("posted to X successfully", "tweet_id", tweetID, "text", postText)
	}

	logger.Info("job completed successfully")
	return nil
}
