package nyt_critics_pick_movies

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"time"

	motmedelErrors "github.com/Motmedel/utils_go/pkg/errors"
	"github.com/Motmedel/utils_go/pkg/errors/types/nil_error"
	"github.com/Motmedel/utils_go/pkg/http/types/fetch_config"
	motmedelHttpUtils "github.com/Motmedel/utils_go/pkg/http/utils"
	"github.com/vphpersson/nyt_critics_pick_movies/pkg/nyt_critics_pick_movies/fetch_reviews_config"
	"github.com/vphpersson/nyt_critics_pick_movies/pkg/types"
)

const (
	GraphQLEndpoint    = "https://samizdat-graphql.nytimes.com/graphql/v2"
	OperationName      = "MovieReviewsQuery"
	PersistedQueryHash = "01cc23ab7df18d924f28da523768f46bedfb202f4dc2ca085f23b748f598ad2c"
	NytAppType         = "project-vi"
	NytAppVersion      = "0.0.5"
	// Public token embedded in nytimes.com's JS bundle; required by the GraphQL gateway.
	NytToken = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAs+/oUCTBmD/cLdmcecrnBMHiU/pxQCn2DDyaPKUOXxi4p0uUSZQzsuq1pJ1m5z1i0YGPd1U1OeGHAChWtqoxC7bFMCXcwnE1oyui9G1uobgpm1GdhtwkR7ta7akVTcsF8zxiXx7DNXIPd2nIJFH83rmkZueKrC4JVaNzjvD+Z03piLn5bHWU6+w+rA+kyJtGgZNTXKyPh6EC6o5N+rknNMG5+CdTq35p8f99WjFawSvYgP9V64kgckbTbtdJ6YhVP58TnuYgr12urtwnIqWP9KSJ1e5vmgf3tunMqWNm6+AnsqNj8mCLdCuc5cEB74CwUeQcP2HQQmbCddBy2y0mEwIDAQAB"
)

var imdbIDPattern = regexp.MustCompile(`/title/(tt\d+)`)

// ErrGraphQL is the API answering with an error rather than results.
var ErrGraphQL = errors.New("graphql error")

// FetchReviews reads the newest reviews, Limit of them a page at a time
// following each page's end cursor, and returns the critics picks among them.
// With Since set it stops at the first review published before it.
func FetchReviews(ctx context.Context, options ...fetch_reviews_config.Option) ([]*types.Entry, error) {
	config := fetch_reviews_config.New(options...)
	now := config.Now()

	var entries []*types.Entry
	cursor := ""
	for read := 0; read < config.Limit; {
		page, err := fetchPage(ctx, config, min(fetch_reviews_config.MaxPageSize, config.Limit-read), cursor)
		if err != nil {
			return nil, fmt.Errorf("fetch page: %w", err)
		}
		hits := page.Data.ContentSearch.Hits

		reachedSince := false
		for _, edge := range hits.Edges {
			if !config.Since.IsZero() {
				published, err := time.Parse(time.RFC3339, edge.Node.FirstPublished)
				if err == nil && published.Before(config.Since) {
					reachedSince = true
					break
				}
			}
			if entry := ToEntry(edge, now); entry != nil {
				entries = append(entries, entry)
			}
		}
		read += len(hits.Edges)

		pageInfo := hits.PageInfo
		if reachedSince || len(hits.Edges) == 0 || pageInfo == nil || !pageInfo.HasNextPage || pageInfo.EndCursor == "" {
			break
		}
		cursor = pageInfo.EndCursor
	}

	return entries, nil
}

// fetchPage asks for the first reviews after the cursor, or the newest when
// there is none.
func fetchPage(ctx context.Context, config *fetch_reviews_config.Config, first int, cursor string) (*types.GraphQLResponse, error) {
	variables := map[string]any{"first": first, "sortOrder": "newest"}
	// The query pages with "cursor"; "after" is silently ignored.
	if cursor != "" {
		variables["cursor"] = cursor
	}
	variablesJson, err := json.Marshal(variables)
	if err != nil {
		return nil, motmedelErrors.NewWithTrace(fmt.Errorf("json marshal: %w", err), variables)
	}

	extensions, err := json.Marshal(map[string]any{
		"persistedQuery": map[string]any{
			"version":    1,
			"sha256Hash": PersistedQueryHash,
		},
	})
	if err != nil {
		return nil, motmedelErrors.NewWithTrace(fmt.Errorf("json marshal: %w", err))
	}

	params := url.Values{}
	params.Set("operationName", OperationName)
	params.Set("variables", string(variablesJson))
	params.Set("extensions", string(extensions))

	fetchOptions := []fetch_config.Option{
		fetch_config.WithHeaders(map[string]string{
			"nyt-app-type":    NytAppType,
			"nyt-app-version": NytAppVersion,
			"nyt-token":       NytToken,
			"origin":          "https://www.nytimes.com",
			"referer":         "https://www.nytimes.com/",
		}),
	}
	if config.HttpClient != nil {
		fetchOptions = append(fetchOptions, fetch_config.WithHttpClient(config.HttpClient))
	}

	_, parsed, err := motmedelHttpUtils.FetchJson[*types.GraphQLResponse](
		ctx,
		GraphQLEndpoint+"?"+params.Encode(),
		fetchOptions...,
	)
	if err != nil {
		return nil, fmt.Errorf("fetch json: %w", err)
	}
	if parsed == nil {
		return nil, motmedelErrors.NewWithTrace(nil_error.New("graphql response"))
	}
	if len(parsed.Errors) > 0 {
		return nil, motmedelErrors.NewWithTrace(fmt.Errorf("%w: %s", ErrGraphQL, parsed.Errors[0].Message), variables)
	}

	return parsed, nil
}

func ToEntry(edge types.Edge, now time.Time) *types.Entry {
	if len(edge.Node.ReviewItems) == 0 {
		return nil
	}

	item := edge.Node.ReviewItems[0]
	if !item.IsCriticsPick {
		return nil
	}

	directors := item.Subject.Directors
	if directors == nil {
		directors = []string{}
	}

	var imdbID *string
	if match := imdbIDPattern.FindStringSubmatch(item.Subject.TicketURL); len(match) == 2 {
		imdbID = new(match[1])
	}

	return &types.Entry{
		Timestamp:           now.UTC().Format(time.RFC3339Nano),
		Source:              "new_york_times",
		Title:               item.Subject.Title,
		Directors:           directors,
		IMDBID:              imdbID,
		ReviewPublishDate:   edge.Node.FirstPublished,
		ReviewLeadParagraph: edge.Node.Summary,
		ReviewLink:          edge.Node.URL,
	}
}
