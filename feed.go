package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	altshiftErrors "github.com/altshiftab/utils_go/pkg/errors"
)

// The films service's feeding of a list, as this job reaches it: vvvp_films' api.Feeding and api.Fed,
// written out here, the service's module being private.

const (
	// feedPath is the films service's endpoint picks are fed to.
	feedPath = "/api/list/feed"
	// chunk is the most picks one feeding sends, within the service's 25.
	chunk = 20
	// outcomeDeferred is a pick the service left for the next feeding: TMDB failing, or time running out.
	outcomeDeferred = "deferred"
)

var (
	errRefused  = errors.New("the films service refused the feeding")
	errDeferred = errors.New("the films service deferred picks to the next feeding")
)

// filmsPick is a review as the films service takes it.
type filmsPick struct {
	Url       string   `json:"url"`
	Published string   `json:"published"`
	Title     string   `json:"title"`
	ImdbId    string   `json:"imdb_id,omitzero"`
	Directors []string `json:"directors,omitzero"`
	Summary   string   `json:"summary,omitzero"`
	Text      string   `json:"text,omitzero"`
}

type feeding struct {
	Feed   string       `json:"feed"`
	DryRun bool         `json:"dry_run,omitzero"`
	Picks  []*filmsPick `json:"picks"`
}

// pickFate is what became of a pick: added, listed already, a re-release left out, unmatched, deferred.
type pickFate struct {
	Url     string `json:"url"`
	Outcome string `json:"outcome"`
	By      string `json:"by"`
	Film    string `json:"film"`
	Title   string `json:"title"`
	Year    int    `json:"year"`
	Reason  string `json:"reason"`
}

type fed struct {
	Added   int         `json:"added"`
	Results []*pickFate `json:"results"`
}

// tokens are sessions the job's API key mints.
type tokens interface {
	Token(ctx context.Context) (string, error)
}

// feedOnce sends the feeding, signed in with a session of the source's, and is what the service did.
func feedOnce(ctx context.Context, httpClient *http.Client, filmsUrl *url.URL, source tokens, input *feeding) (*fed, error) {
	token, err := source.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("token: %w", err)
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("json marshal: %w", err)
	}
	// An error carries the address, the status and the answer, never the request: its header holds the token.
	address := filmsUrl.JoinPath(feedPath).String()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(body))
	if err != nil {
		return nil, altshiftErrors.NewWithTrace(fmt.Errorf("http new request: %w", err), address)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, altshiftErrors.NewWithTrace(fmt.Errorf("http do: %w", err), address)
	}
	if response == nil {
		return nil, altshiftErrors.NewWithTrace(fmt.Errorf("%w: no response", errRefused), address)
	}
	defer func() { _ = response.Body.Close() }()
	answer, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
	if err != nil {
		return nil, altshiftErrors.NewWithTrace(fmt.Errorf("read body: %w", err), address, response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		return nil, altshiftErrors.NewWithTrace(
			fmt.Errorf("%w: status %d: %s", errRefused, response.StatusCode, strings.TrimSpace(string(answer))), address, response.StatusCode,
		)
	}
	var out fed
	if err := json.Unmarshal(answer, &out); err != nil {
		return nil, altshiftErrors.NewWithTrace(fmt.Errorf("json unmarshal: %w", err), answer)
	}
	if len(out.Results) != len(input.Picks) {
		return nil, altshiftErrors.NewWithTrace(fmt.Errorf("%w: %d results of %d picks", errRefused, len(out.Results), len(input.Picks)), answer)
	}
	return &out, nil
}

// feedAll feeds the picks, the newest first, in chunks: the oldest chunk first, each the newest first, so
// that the list, which puts what it lacks at its top, ends the newest first. It stops at the first chunk
// the service deferred any pick of, so that no newer film goes on above one left for later, and is what
// became of each pick sent.
func feedAll(ctx context.Context, httpClient *http.Client, filmsUrl *url.URL, source tokens, feed string, picks []*filmsPick, dryRun bool) ([]*pickFate, error) {
	var fates []*pickFate
	for end := len(picks); end > 0; end -= chunk {
		batch := picks[max(0, end-chunk):end]
		result, err := feedOnce(ctx, httpClient, filmsUrl, source, &feeding{Feed: feed, DryRun: dryRun, Picks: batch})
		if err != nil {
			return fates, err
		}
		fates = append(fates, result.Results...)
		for _, fate := range result.Results {
			if fate != nil && fate.Outcome == outcomeDeferred {
				return fates, errDeferred
			}
		}
	}
	return fates, nil
}
