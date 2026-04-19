package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"time"
)

const (
	graphQLEndpoint    = "https://samizdat-graphql.nytimes.com/graphql/v2"
	operationName      = "MovieReviewsQuery"
	persistedQueryHash = "01cc23ab7df18d924f28da523768f46bedfb202f4dc2ca085f23b748f598ad2c"
	nytAppType         = "project-vi"
	nytAppVersion      = "0.0.5"
	// Public token embedded in nytimes.com's JS bundle; required by the GraphQL gateway.
	nytToken = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAs+/oUCTBmD/cLdmcecrnBMHiU/pxQCn2DDyaPKUOXxi4p0uUSZQzsuq1pJ1m5z1i0YGPd1U1OeGHAChWtqoxC7bFMCXcwnE1oyui9G1uobgpm1GdhtwkR7ta7akVTcsF8zxiXx7DNXIPd2nIJFH83rmkZueKrC4JVaNzjvD+Z03piLn5bHWU6+w+rA+kyJtGgZNTXKyPh6EC6o5N+rknNMG5+CdTq35p8f99WjFawSvYgP9V64kgckbTbtdJ6YhVP58TnuYgr12urtwnIqWP9KSJ1e5vmgf3tunMqWNm6+AnsqNj8mCLdCuc5cEB74CwUeQcP2HQQmbCddBy2y0mEwIDAQAB"

	defaultLimit = 30
)

var imdbIDPattern = regexp.MustCompile(`/title/(tt\d+)`)

type Subject struct {
	Title     string   `json:"title"`
	Directors []string `json:"directors"`
	TicketURL string   `json:"ticketUrl"`
}

type ReviewItem struct {
	IsCriticsPick bool    `json:"isCriticsPick"`
	Subject       Subject `json:"subject"`
}

type Node struct {
	FirstPublished string       `json:"firstPublished"`
	Summary        string       `json:"summary"`
	URL            string       `json:"url"`
	ReviewItems    []ReviewItem `json:"reviewItems"`
}

type Edge struct {
	Node Node `json:"node"`
}

type GraphQLResponse struct {
	Data struct {
		ContentSearch struct {
			Hits struct {
				Edges []Edge `json:"edges"`
			} `json:"hits"`
		} `json:"contentSearch"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type Event struct {
	Timestamp           string   `json:"timestamp"`
	Source              string   `json:"source"`
	Title               string   `json:"title"`
	Directors           []string `json:"directors"`
	IMDBID              *string  `json:"imdb_id"`
	ReviewPublishDate   string   `json:"review_publish_date"`
	ReviewLeadParagraph string   `json:"review_lead_paragraph"`
	ReviewLink          string   `json:"review_link"`
}

func fetchReviews(ctx context.Context, client *http.Client, limit int) ([]Edge, error) {
	variables, err := json.Marshal(map[string]any{
		"first":     limit,
		"sortOrder": "newest",
	})
	if err != nil {
		return nil, fmt.Errorf("marshal variables: %w", err)
	}

	extensions, err := json.Marshal(map[string]any{
		"persistedQuery": map[string]any{
			"version":    1,
			"sha256Hash": persistedQueryHash,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal extensions: %w", err)
	}

	params := url.Values{}
	params.Set("operationName", operationName)
	params.Set("variables", string(variables))
	params.Set("extensions", string(extensions))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, graphQLEndpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("nyt-app-type", nytAppType)
	req.Header.Set("nyt-app-version", nytAppVersion)
	req.Header.Set("nyt-token", nytToken)
	req.Header.Set("origin", "https://www.nytimes.com")
	req.Header.Set("referer", "https://www.nytimes.com/")
	req.Header.Set("accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("graphql returned %d: %s", resp.StatusCode, string(body))
	}

	var parsed GraphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if len(parsed.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", parsed.Errors[0].Message)
	}

	return parsed.Data.ContentSearch.Hits.Edges, nil
}

func toEvent(edge Edge, now time.Time) (Event, bool) {
	if len(edge.Node.ReviewItems) == 0 {
		return Event{}, false
	}
	item := edge.Node.ReviewItems[0]
	if !item.IsCriticsPick {
		return Event{}, false
	}

	directors := item.Subject.Directors
	if directors == nil {
		directors = []string{}
	}

	var imdbID *string
	if match := imdbIDPattern.FindStringSubmatch(item.Subject.TicketURL); len(match) == 2 {
		id := match[1]
		imdbID = &id
	}

	return Event{
		Timestamp:           now.UTC().Format(time.RFC3339Nano),
		Source:              "new_york_times",
		Title:               item.Subject.Title,
		Directors:           directors,
		IMDBID:              imdbID,
		ReviewPublishDate:   edge.Node.FirstPublished,
		ReviewLeadParagraph: edge.Node.Summary,
		ReviewLink:          edge.Node.URL,
	}, true
}

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
	limit, err := readIntEnv("LIMIT", defaultLimit)
	if err != nil {
		return err
	}

	edges, err := fetchReviews(ctx, http.DefaultClient, limit)
	if err != nil {
		return fmt.Errorf("fetch reviews: %w", err)
	}

	slog.InfoContext(ctx, "Fetched reviews.", "count", len(edges), "limit", limit)

	encoder := json.NewEncoder(os.Stdout)
	now := time.Now()
	var picks int

	for _, edge := range edges {
		event, ok := toEvent(edge, now)
		if !ok {
			continue
		}
		if err := encoder.Encode(event); err != nil {
			return fmt.Errorf("encode event: %w", err)
		}
		picks++
	}

	slog.InfoContext(ctx, "Emitted critics picks.", "count", picks)
	return nil
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	if err := run(context.Background()); err != nil {
		slog.Error("Run failed.", "error", err.Error())
		os.Exit(1)
	}
}
