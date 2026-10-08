package nyt_critics_pick_movies

import (
	"bytes"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vphpersson/nyt_critics_pick_movies/pkg/nyt_critics_pick_movies/fetch_reviews_config"
	"github.com/vphpersson/nyt_critics_pick_movies/pkg/types"
)

var (
	errBadCursor   = errors.New("bad cursor")
	errBadVariable = errors.New("bad variables")
	testNow        = time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
)

// fakeApi answers MovieReviewsQuery as the NYT gateway does: the reviews
// newest first, a page of "first" after the "cursor" (base64 of
// "arrayconnection:<index of the last item seen>"), and a page info with the
// page's end cursor. Every third review is a critics pick; review i is
// published i days before testNow.
type fakeApi struct {
	total     int
	failAfter int

	mutex    sync.Mutex
	requests []map[string]any
}

func (f *fakeApi) RoundTrip(request *http.Request) (*http.Response, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	var variables map[string]any
	if err := json.Unmarshal([]byte(request.URL.Query().Get("variables")), &variables); err != nil {
		return nil, fmt.Errorf("%w: %w", errBadVariable, err)
	}
	f.requests = append(f.requests, variables)

	start := 0
	if cursor, ok := variables["cursor"].(string); ok {
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errBadCursor, err)
		}
		last, err := strconv.Atoi(strings.TrimPrefix(string(decoded), "arrayconnection:"))
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errBadCursor, err)
		}
		start = last + 1
	}
	first := int(variables["first"].(float64))

	var body any
	if f.failAfter > 0 && len(f.requests) > f.failAfter {
		body = map[string]any{"errors": []any{map[string]any{"message": "Internal server error"}}}
	} else {
		var edges []any
		end := min(start+first, f.total)
		for index := start; index < end; index++ {
			edges = append(edges, map[string]any{"node": map[string]any{
				"firstPublished": testNow.AddDate(0, 0, -index).Format("2006-01-02T15:04:05.000Z"),
				"summary":        "Review " + strconv.Itoa(index),
				"url":            "https://www.nytimes.com/review-" + strconv.Itoa(index) + ".html",
				"reviewItems": []any{map[string]any{
					"isCriticsPick": index%3 == 0,
					"subject": map[string]any{
						"title":     "Film " + strconv.Itoa(index),
						"directors": []string{"Director"},
						"ticketUrl": "https://www.imdb.com/title/tt" + strconv.Itoa(1000000+index) + "/",
					},
				}},
			}})
		}
		body = map[string]any{"data": map[string]any{"contentSearch": map[string]any{"hits": map[string]any{
			"edges": edges,
			"pageInfo": map[string]any{
				"hasNextPage": end < f.total,
				"endCursor":   base64.StdEncoding.EncodeToString([]byte("arrayconnection:" + strconv.Itoa(end-1))),
			},
		}}}}
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(data)),
		Request:    request,
	}, nil
}

func TestFetchReviews(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		total       int
		limit       int
		since       time.Time
		failAfter   int
		wantFirsts  []float64
		wantPicks   int
		wantOldest  string
		wantErrorIs error
	}{
		{name: "one page", total: 2000, limit: 30, wantFirsts: []float64{30}, wantPicks: 10, wantOldest: "Film 27"},
		{name: "pages of a hundred", total: 2000, limit: 250, wantFirsts: []float64{100, 100, 50}, wantPicks: 84, wantOldest: "Film 249"},
		{name: "the end of the results", total: 120, limit: 1000, wantFirsts: []float64{100, 100}, wantPicks: 40, wantOldest: "Film 117"},
		{
			name: "stopped at since",
			// Reviews 0 to 150 are published on or after it.
			total: 2000, limit: 1000, since: testNow.AddDate(0, 0, -150),
			wantFirsts: []float64{100, 100}, wantPicks: 51, wantOldest: "Film 150",
		},
		{name: "a page refused", total: 2000, limit: 300, failAfter: 1, wantErrorIs: ErrGraphQL},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			api := &fakeApi{total: testCase.total, failAfter: testCase.failAfter}
			entries, err := FetchReviews(
				t.Context(),
				fetch_reviews_config.WithLimit(testCase.limit),
				fetch_reviews_config.WithSince(testCase.since),
				fetch_reviews_config.WithHttpClient(&http.Client{Transport: api}),
				fetch_reviews_config.WithNow(func() time.Time { return testNow }),
			)
			if testCase.wantErrorIs != nil {
				if !errors.Is(err, testCase.wantErrorIs) {
					t.Fatalf("expected %v, got %v", testCase.wantErrorIs, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("fetch reviews: %v", err)
			}

			var firsts []float64
			for index, variables := range api.requests {
				firsts = append(firsts, variables["first"].(float64))
				// Each page after the first continues from the one before.
				_, hasCursor := variables["cursor"]
				if hasCursor != (index > 0) {
					t.Errorf("request %d: unexpected cursor presence: %v", index, variables)
				}
			}
			if fmt.Sprint(firsts) != fmt.Sprint(testCase.wantFirsts) {
				t.Errorf("unexpected page sizes: %v", firsts)
			}
			if len(entries) != testCase.wantPicks {
				t.Fatalf("unexpected picks: %d", len(entries))
			}
			if oldest := entries[len(entries)-1]; oldest.Title != testCase.wantOldest {
				t.Errorf("unexpected oldest pick: %q", oldest.Title)
			}
			// Every pick once, newest first.
			seen := map[string]bool{}
			for _, entry := range entries {
				if seen[entry.ReviewLink] {
					t.Fatalf("a pick twice: %s", entry.ReviewLink)
				}
				seen[entry.ReviewLink] = true
			}
		})
	}
}

func TestToEntry(t *testing.T) {
	t.Parallel()

	pick := func(ticketUrl string, directors []string) types.Edge {
		return types.Edge{Node: types.Node{
			FirstPublished: "2026-10-08T16:39:21.000Z",
			Summary:        "A summary.",
			URL:            "https://www.nytimes.com/2026/10/08/movies/x.html",
			ReviewItems: []types.ReviewItem{{
				IsCriticsPick: true,
				Subject:       types.Subject{Title: "Fjord", Directors: directors, TicketURL: ticketUrl},
			}},
		}}
	}

	testCases := []struct {
		name          string
		edge          types.Edge
		wantNil       bool
		wantImdbId    string
		wantDirectors []string
	}{
		{name: "a pick", edge: pick("https://www.imdb.com/title/tt39317981/?ref=nyt", []string{"Cristian Mungiu"}), wantImdbId: "tt39317981", wantDirectors: []string{"Cristian Mungiu"}},
		{name: "no ticket", edge: pick("", nil), wantDirectors: []string{}},
		{name: "not a pick", edge: types.Edge{Node: types.Node{ReviewItems: []types.ReviewItem{{IsCriticsPick: false}}}}, wantNil: true},
		{name: "no review item", edge: types.Edge{}, wantNil: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			entry := ToEntry(testCase.edge, testNow)
			if testCase.wantNil {
				if entry != nil {
					t.Fatalf("expected no entry, got %+v", entry)
				}
				return
			}
			if entry == nil {
				t.Fatal("no entry")
			}
			imdbId := ""
			if entry.IMDBID != nil {
				imdbId = *entry.IMDBID
			}
			if imdbId != testCase.wantImdbId || fmt.Sprint(entry.Directors) != fmt.Sprint(testCase.wantDirectors) || entry.Directors == nil {
				t.Errorf("unexpected entry: %+v (imdb %q)", entry, imdbId)
			}
			if entry.Source != "new_york_times" || entry.ReviewPublishDate != "2026-10-08T16:39:21.000Z" || entry.Timestamp != "2026-10-09T00:00:00Z" {
				t.Errorf("unexpected entry: %+v", entry)
			}
		})
	}
}
