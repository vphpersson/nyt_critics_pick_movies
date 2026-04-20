package main

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/vphpersson/nyt_critics_pick_movies/pkg/nyt_critics_pick_movies"
	"github.com/vphpersson/nyt_critics_pick_movies/pkg/nyt_critics_pick_movies/fetch_reviews_config"
)

func readIntEnv(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s: %q", name, value)
	}
	return parsed, nil
}

func run(ctx context.Context) error {
	limit, err := readIntEnv("LIMIT", fetch_reviews_config.DefaultLimit)
	if err != nil {
		return err
	}

	events, err := nyt_critics_pick_movies.FetchReviews(ctx, fetch_reviews_config.WithLimit(limit))
	if err != nil {
		return fmt.Errorf("fetch reviews: %w", err)
	}

	for _, event := range events {
		if err := json.MarshalWrite(os.Stdout, event); err != nil {
			return fmt.Errorf("marshal event: %w", err)
		}
		if _, err := os.Stdout.Write([]byte{'\n'}); err != nil {
			return fmt.Errorf("write newline: %w", err)
		}
	}

	slog.InfoContext(ctx, "Emitted critics picks.", "count", len(events), "limit", limit)
	return nil
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	if err := run(context.Background()); err != nil {
		slog.Error("Run failed.", "error", err.Error())
		os.Exit(1)
	}
}
