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
	// Cloud Logging で構造化ログとして扱える JSON ハンドラを使用
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, logger); err != nil {
		logger.Error("ジョブ実行中にエラーが発生しました", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger.Info("ジョブを開始します",
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
		logger.Info("投稿対象のニュースがありませんでした")
		return nil
	}

	client := xclient.New(cfg)
	tweetID, err := client.PostTweet(ctx, postText)
	if err != nil {
		return err
	}

	if cfg.DryRun {
		logger.Info("[DRY_RUN] 投稿をスキップしました", "text", postText)
	} else {
		logger.Info("X への投稿に成功しました", "tweet_id", tweetID, "text", postText)
	}

	logger.Info("ジョブが正常に完了しました")
	return nil
}
