package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

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
		"stop_after_date", cfg.StopAfterDate,
	)

	expired, err := isPastStopDate(time.Now(), cfg.StopAfterDate, cfg.Timezone)
	if err != nil {
		return err
	}
	if expired {
		logger.Info("past stop_after_date; skipping execution",
			"stop_after_date", cfg.StopAfterDate,
			"timezone", cfg.Timezone,
		)
		return nil
	}

	fetcher := news.New(cfg)
	posts, err := fetcher.FetchAndTranslate(ctx)
	if err != nil {
		return err
	}
	if len(posts) == 0 {
		logger.Info("no news to post")
		return nil
	}

	if cfg.DryRun {
		for i, text := range posts {
			logger.Info("dry run enabled; skipped posting to X",
				"index", i+1,
				"total", len(posts),
				"text", text,
			)
		}
		logger.Info("job completed successfully")
		return nil
	}

	client := xclient.New(cfg)
	tweetIDs := make([]string, 0, len(posts))
	for i, text := range posts {
		tweetID, err := client.PostTweet(ctx, text)
		if err != nil {
			return err
		}
		tweetIDs = append(tweetIDs, tweetID)
		logger.Info("posted to X successfully",
			"index", i+1,
			"total", len(posts),
			"tweet_id", tweetID,
		)
	}

	logger.Info("job completed successfully",
		"count", len(tweetIDs),
		"tweet_ids", tweetIDs,
	)
	return nil
}

// isPastStopDate returns true when now's local date in tzName is strictly after
// stopAfterDate (YYYY-MM-DD). An empty stopAfterDate disables the cutoff.
func isPastStopDate(now time.Time, stopAfterDate, tzName string) (bool, error) {
	if stopAfterDate == "" {
		return false, nil
	}
	if _, err := time.Parse("2006-01-02", stopAfterDate); err != nil {
		return false, fmt.Errorf("invalid STOP_AFTER_DATE (%q), expected YYYY-MM-DD: %w", stopAfterDate, err)
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return false, fmt.Errorf("invalid TIMEZONE (%q): %w", tzName, err)
	}
	today := now.In(loc).Format("2006-01-02")
	return today > stopAfterDate, nil
}
