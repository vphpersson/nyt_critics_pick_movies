package main

import (
	"slices"
	"testing"

	"github.com/vphpersson/nyt_critics_pick_movies/pkg/types"
)

func TestToPicks(t *testing.T) {
	t.Parallel()

	entries := []*types.Entry{
		{
			Title: "Earlier", Directors: []string{"A Director"}, ReviewPublishDate: "2026-11-12T10:00:05.000Z", ReviewLeadParagraph: "Older summary.",
			ReviewLink: "https://www.nytimes.com/2026/11/12/movies/older.html",
		},
		nil,
		{Title: "No link", ReviewPublishDate: "2026-11-20T10:00:00.000Z"},
		{Title: "No day", ReviewLink: "https://www.nytimes.com/x.html", ReviewPublishDate: "2026"},
		{
			Title: "Newer", IMDBID: new("tt0133093"), Directors: []string{}, ReviewPublishDate: "2026-11-19T09:00:00.000Z", ReviewLeadParagraph: "Newer summary.",
			ReviewLink: "https://www.nytimes.com/2026/11/19/movies/newer.html",
		},
	}
	picks := toPicks(entries)
	got := []string{}
	for _, pick := range picks {
		got = append(got, pick.Published+" "+pick.Title+" "+pick.ImdbId+" "+pick.Summary)
	}
	want := []string{"2026-11-19 Newer tt0133093 Newer summary.", "2026-11-12 Earlier  Older summary."}
	if !slices.Equal(got, want) {
		t.Errorf("picks %q", got)
	}
	if len(picks) == 2 && (!slices.Equal(picks[1].Directors, []string{"A Director"}) || picks[0].Url != "https://www.nytimes.com/2026/11/19/movies/newer.html") {
		t.Errorf("picks %+v %+v", picks[0], picks[1])
	}
}
