package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/altshiftab/authentication_go/pkg/api_key/client"
	altshiftEnv "github.com/altshiftab/utils_go/pkg/env"
	httpContextExtractor "github.com/altshiftab/utils_go/pkg/http/types/http_context_extractor"
	altshiftLog "github.com/altshiftab/utils_go/pkg/log"
	errorLogger "github.com/altshiftab/utils_go/pkg/log/error_logger"
	"github.com/vphpersson/nyt_critics_pick_movies/pkg/nyt_critics_pick_movies"
	"github.com/vphpersson/nyt_critics_pick_movies/pkg/nyt_critics_pick_movies/fetch_reviews_config"
	"github.com/vphpersson/nyt_critics_pick_movies/pkg/types"
	"github.com/vphpersson/tpm_signer/pkg/keyfile"
	"github.com/vphpersson/tpm_signer/pkg/signer"
)

var errInvalidEnv = errors.New("invalid environment value")

// fetchOptions are the fetch's options: LIMIT reviews read, and with SINCE
// set (YYYY-MM-DD or RFC 3339) none published before it.
func fetchOptions(limit int) ([]fetch_reviews_config.Option, error) {
	options := []fetch_reviews_config.Option{fetch_reviews_config.WithLimit(limit)}
	if value := os.Getenv("SINCE"); value != "" {
		since, err := fetch_reviews_config.ParseSince(value)
		if err != nil {
			return nil, fmt.Errorf("parse since: %w", err)
		}
		options = append(options, fetch_reviews_config.WithSince(since))
	}
	return options, nil
}

func readIntEnv(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%w: %s=%q", errInvalidEnv, name, value)
	}
	return parsed, nil
}

func readBoolEnv(name string) (bool, error) {
	value := os.Getenv(name)
	if value == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%w: %s=%q", errInvalidEnv, name, value)
	}
	return parsed, nil
}

// toPicks are the critics' picks as the films service takes them, the newest first: each review's day,
// the film's title, its IMDb id if the review gives one, its directors, and the review's summary.
func toPicks(entries []*types.Entry) []*filmsPick {
	var picks []*filmsPick
	for _, entry := range entries {
		if entry == nil || entry.ReviewLink == "" || len(entry.ReviewPublishDate) < len("2006-01-02") {
			continue
		}
		pick := &filmsPick{
			Url: entry.ReviewLink, Published: entry.ReviewPublishDate[:len("2006-01-02")], Title: entry.Title, Directors: entry.Directors,
			Summary: entry.ReviewLeadParagraph,
		}
		if entry.IMDBID != nil {
			pick.ImdbId = *entry.IMDBID
		}
		picks = append(picks, pick)
	}
	slices.SortStableFunc(picks, func(a *filmsPick, b *filmsPick) int { return strings.Compare(b.Published, a.Published) })
	return picks
}

// signIn is the source of sessions the job's API key mints, the key inside the TPM.
func signIn(keyFile string, tpmDevice string, loginUrl *url.URL) (tokens, error) {
	keyData, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	key, err := keyfile.Parse(keyData)
	if err != nil {
		return nil, fmt.Errorf("keyfile parse: %w", err)
	}
	tpmSigner, err := signer.New(key, tpmDevice, nil)
	if err != nil {
		return nil, fmt.Errorf("signer new: %w", err)
	}
	return &client.Source{Signer: tpmSigner, Service: loginUrl, HttpClient: &http.Client{Timeout: time.Minute}}, nil
}

// logFates says what became of each pick: what went on the list, what was left out and why.
func logFates(ctx context.Context, fates []*pickFate) {
	counts := map[string]int{}
	for _, fate := range fates {
		if fate == nil {
			continue
		}
		counts[fate.Outcome]++
		switch fate.Outcome {
		case "unmatched":
			slog.WarnContext(ctx, "A pick matched no film; left out.", "url", fate.Url, "reason", fate.Reason)
		case "added":
			slog.InfoContext(ctx, "A pick was added.", "url", fate.Url, "film", fate.Film, "title", fate.Title, "year", fate.Year, "by", fate.By)
		}
	}
	slog.InfoContext(ctx, "Fed the picks.", "added", counts["added"], "listed", counts["listed"], "unmatched", counts["unmatched"],
		"deferred", counts["deferred"])
}

func run(ctx context.Context) error {
	limit, err := readIntEnv("LIMIT", fetch_reviews_config.DefaultLimit)
	if err != nil {
		return err
	}
	dryRun, err := readBoolEnv("DRY_RUN")
	if err != nil {
		return err
	}
	options, err := fetchOptions(limit)
	if err != nil {
		return err
	}
	filmsUrl, err := url.Parse(altshiftEnv.GetEnvWithDefault("FILMS_URL", "https://films.vvvp.se"))
	if err != nil {
		return fmt.Errorf("parse FILMS_URL: %w", err)
	}
	loginUrl, err := url.Parse(altshiftEnv.GetEnvWithDefault("LOGIN_URL", "https://login.vvvp.se"))
	if err != nil {
		return fmt.Errorf("parse LOGIN_URL: %w", err)
	}
	source, err := signIn(altshiftEnv.GetEnvWithDefault("KEY_FILE", "/etc/tpm-signer/key.tpm"), altshiftEnv.GetEnvWithDefault("TPM_DEVICE", "/dev/tpmrm0"), loginUrl)
	if err != nil {
		return err
	}

	entries, err := nyt_critics_pick_movies.FetchReviews(ctx, options...)
	if err != nil {
		return fmt.Errorf("fetch reviews: %w", err)
	}
	picks := toPicks(entries)
	slog.InfoContext(ctx, "Fetched reviews.", "count", len(entries), "picks", len(picks), "limit", limit)
	if len(picks) == 0 {
		return nil
	}

	fates, err := feedAll(ctx, &http.Client{Timeout: 2 * time.Minute}, filmsUrl, source, "nyt", picks, dryRun)
	logFates(ctx, fates)
	if err != nil {
		return fmt.Errorf("feed all: %w", err)
	}
	return nil
}

func main() {
	logger := errorLogger.NewWithErrorContextExtractor(
		slog.NewJSONHandler(os.Stdout, nil),
		&altshiftLog.ErrorContextExtractor{
			ContextExtractors: []altshiftLog.ContextExtractor{httpContextExtractor.New()},
		},
	)
	slog.SetDefault(logger.Logger)

	if err := run(context.Background()); err != nil {
		logger.Fatal("Run failed.", err)
	}
}
