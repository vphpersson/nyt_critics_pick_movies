package main

import (
	"encoding/json/v2"
	"reflect"
	"testing"

	"github.com/vphpersson/nyt_critics_pick_movies/pkg/types"
)

func TestToPicks(t *testing.T) {
	t.Parallel()

	const (
		older = "https://www.nytimes.com/2026/11/12/movies/older.html"
		newer = "https://www.nytimes.com/2026/11/19/movies/newer.html"
	)
	testCases := []struct {
		name    string
		entries []*types.Entry
		want    []*filmsPick
	}{
		{
			name: "a review is its link and day, the film's title and directors, and its summary",
			entries: []*types.Entry{{
				Title: "Earlier", Directors: []string{"A Director"}, ReviewPublishDate: "2026-11-12T10:00:05.000Z", ReviewLeadParagraph: "Older summary.",
				ReviewLink: older,
			}},
			want: []*filmsPick{{Url: older, Published: "2026-11-12", Title: "Earlier", Directors: []string{"A Director"}, Summary: "Older summary."}},
		},
		{
			name:    "an IMDb id the review gives",
			entries: []*types.Entry{{Title: "Newer", IMDBID: new("tt0133093"), ReviewPublishDate: "2026-11-19T09:00:00.000Z", ReviewLink: newer}},
			want:    []*filmsPick{{Url: newer, Published: "2026-11-19", Title: "Newer", ImdbId: "tt0133093"}},
		},
		{name: "no entry", entries: []*types.Entry{nil}},
		{name: "no link", entries: []*types.Entry{{Title: "No link", ReviewPublishDate: "2026-11-20T10:00:00.000Z"}}},
		{name: "a date shorter than a day", entries: []*types.Entry{{Title: "No day", ReviewLink: "https://www.nytimes.com/x.html", ReviewPublishDate: "2026"}}},
		{
			name: "the newest first",
			entries: []*types.Entry{
				{Title: "Earlier", ReviewPublishDate: "2026-11-12T10:00:05.000Z", ReviewLink: older},
				{Title: "Newer", ReviewPublishDate: "2026-11-19T09:00:00.000Z", ReviewLink: newer},
			},
			want: []*filmsPick{{Url: newer, Published: "2026-11-19", Title: "Newer"}, {Url: older, Published: "2026-11-12", Title: "Earlier"}},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := toPicks(testCase.entries); !reflect.DeepEqual(got, testCase.want) {
				gotJson, _ := json.Marshal(got)
				wantJson, _ := json.Marshal(testCase.want)
				t.Errorf("picks %s, want %s", gotJson, wantJson)
			}
		})
	}
}
