package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	motmedelEnv "github.com/Motmedel/utils_go/pkg/env"
	"github.com/Motmedel/utils_go/pkg/http/types/fetch_config"
	motmedelHttpUtils "github.com/Motmedel/utils_go/pkg/http/utils"
	letterboxdTypes "github.com/vphpersson/letterboxd_list_updater/api/types"
	"github.com/vphpersson/letterboxd_list_updater/api/types/endpoint/update_list_endpoint"
	letterboxdUtils "github.com/vphpersson/letterboxd_list_updater/api/utils"
	"github.com/vphpersson/nyt_critics_pick_movies/pkg/nyt_critics_pick_movies"
	"github.com/vphpersson/nyt_critics_pick_movies/pkg/nyt_critics_pick_movies/fetch_reviews_config"
	"github.com/vphpersson/nyt_critics_pick_movies/pkg/types"
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

func eventToImportEntry(event *types.Entry) *letterboxdTypes.ImportEntry {
	if event == nil {
		return nil
	}

	review := "# NYT\n\n" + event.ReviewLeadParagraph + "\n\n" + event.ReviewLink

	entry := &letterboxdTypes.ImportEntry{Review: review}

	if event.IMDBID != nil && *event.IMDBID != "" {
		entry.ImdbID = *event.IMDBID
		return entry
	}

	entry.Title = event.Title
	entry.Directors = strings.Join(event.Directors, ", ")

	return entry
}

func run(ctx context.Context) error {
	limit, err := readIntEnv("LIMIT", fetch_reviews_config.DefaultLimit)
	if err != nil {
		return err
	}

	letterboxdDomain := motmedelEnv.GetEnvWithDefault("LETTERBOXD_DOMAIN", "letterboxd-list-updater.home.arpa")
	listName := motmedelEnv.GetEnvWithDefault("LETTERBOXD_LIST", "vph/collected")

	nytEntry, err := nyt_critics_pick_movies.FetchReviews(ctx, fetch_reviews_config.WithLimit(limit))
	if err != nil {
		return fmt.Errorf("fetch reviews: %w", err)
	}

	slog.InfoContext(ctx, "Fetched reviews.", "count", len(nytEntry), "limit", limit)

	if len(nytEntry) == 0 {
		slog.InfoContext(ctx, "No critics picks to submit.")
		return nil
	}

	entries := make([]*letterboxdTypes.ImportEntry, 0, len(nytEntry))
	for _, event := range nytEntry {
		entries = append(entries, eventToImportEntry(event))
	}

	response, _, err := motmedelHttpUtils.FetchJsonWithBody[any](
		ctx,
		new(url.URL{Scheme: "https", Host: letterboxdDomain, Path: update_list_endpoint.DefaultPath}).String(),
		letterboxdTypes.UpdateList{
			List: listName,
			Data: string(letterboxdUtils.ImportEntriesToCSV(entries)),
		},
		fetch_config.WithMethod(http.MethodPatch),
	)
	if err != nil {
		return fmt.Errorf("fetch json with body: %w", err)
	}

	slog.InfoContext(
		ctx,
		"Submitted critics picks.",
		"count", len(entries),
		"status", response.StatusCode,
	)
	return nil
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	if err := run(context.Background()); err != nil {
		slog.Error("Run failed.", "error", err.Error())
		os.Exit(1)
	}
}
