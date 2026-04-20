package nyt_critics_pick_movies

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/url"
	"regexp"
	"time"

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

func FetchReviews(ctx context.Context, options ...fetch_reviews_config.Option) ([]*types.Entry, error) {
	config := fetch_reviews_config.New(options...)

	variables, err := json.Marshal(map[string]any{
		"first":     config.Limit,
		"sortOrder": "newest",
	})
	if err != nil {
		return nil, fmt.Errorf("marshal variables: %w", err)
	}

	extensions, err := json.Marshal(map[string]any{
		"persistedQuery": map[string]any{
			"version":    1,
			"sha256Hash": PersistedQueryHash,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal extensions: %w", err)
	}

	params := url.Values{}
	params.Set("operationName", OperationName)
	params.Set("variables", string(variables))
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

	_, parsed, err := motmedelHttpUtils.FetchJson[types.GraphQLResponse](
		ctx,
		GraphQLEndpoint+"?"+params.Encode(),
		fetchOptions...,
	)
	if err != nil {
		return nil, fmt.Errorf("fetch json: %w", err)
	}
	if len(parsed.Errors) > 0 {
		return nil, fmt.Errorf("graphql error: %s", parsed.Errors[0].Message)
	}

	now := config.Now()
	entries := make([]*types.Entry, 0, len(parsed.Data.ContentSearch.Hits.Edges))
	for _, edge := range parsed.Data.ContentSearch.Hits.Edges {
		if entry := ToEntry(edge, now); entry != nil {
			entries = append(entries, entry)
		}
	}

	return entries, nil
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
