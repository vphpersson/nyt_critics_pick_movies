package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type fixedToken struct{ token string }

func (f *fixedToken) Token(context.Context) (string, error) { return f.token, nil }

// films is the films service as the job feeds it: it records each feeding, and answers each pick as
// added, but deferred from the feeding of the number given (from 1), and refuses a token not the one given.
func films(t *testing.T, deferFrom int) (*url.URL, func() []*feeding) {
	t.Helper()
	var (
		mutex    sync.Mutex
		received []*feeding
	)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer good" || request.URL.Path != feedPath || request.Method != http.MethodPost {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(request.Body)
		var input feeding
		if err := json.Unmarshal(body, &input); err != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		mutex.Lock()
		received = append(received, &input)
		number := len(received)
		mutex.Unlock()
		out := fed{}
		for _, pick := range input.Picks {
			outcome := "added"
			if deferFrom > 0 && number >= deferFrom {
				outcome = outcomeDeferred
			}
			out.Results = append(out.Results, &pickFate{Url: pick.Url, Outcome: outcome})
		}
		answer, _ := json.Marshal(out)
		_, _ = writer.Write(answer)
	}))
	t.Cleanup(server.Close)
	base, _ := url.Parse(server.URL)
	return base, func() []*feeding {
		mutex.Lock()
		defer mutex.Unlock()
		return slices.Clone(received)
	}
}

// newestFirst are n picks, the newest first, their addresses numbered from the newest.
func newestFirst(n int) []*filmsPick {
	out := make([]*filmsPick, n)
	for i := range out {
		out[i] = &filmsPick{Url: "https://www.nytimes.com/movies/" + strconv.Itoa(i), Title: "Film " + strconv.Itoa(i)}
	}
	return out
}

func TestFeedAll(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		picks     int
		deferFrom int
		token     string
		feedings  [][2]int
		fates     int
		err       error
	}{
		{
			name: "in chunks, the oldest first, each the newest first", picks: 45, token: "good", fates: 45,
			feedings: [][2]int{{25, 44}, {5, 24}, {0, 4}},
		},
		{name: "one chunk", picks: 3, token: "good", fates: 3, feedings: [][2]int{{0, 2}}},
		{name: "stopped at the first chunk deferred", picks: 45, deferFrom: 2, token: "good", fates: 40, feedings: [][2]int{{25, 44}, {5, 24}}, err: errDeferred},
		{name: "refused", picks: 3, token: "bad", feedings: [][2]int{}, err: errRefused},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			base, sent := films(t, testCase.deferFrom)
			fates, err := feedAll(t.Context(), http.DefaultClient, base, &fixedToken{token: testCase.token}, "nyt", newestFirst(testCase.picks), false)
			if !errors.Is(err, testCase.err) || testCase.err == nil && err != nil {
				t.Fatalf("err %v", err)
			}
			if len(fates) != testCase.fates {
				t.Errorf("%d fates", len(fates))
			}
			number := func(pick *filmsPick) int {
				n, _ := strconv.Atoi(pick.Url[strings.LastIndex(pick.Url, "/")+1:])
				return n
			}
			got := [][2]int{}
			for _, each := range sent() {
				if each.Feed != "nyt" {
					t.Errorf("feed %q", each.Feed)
				}
				// The newest of the chunk first, its oldest last: the numbers counted from the newest.
				got = append(got, [2]int{number(each.Picks[0]), number(each.Picks[len(each.Picks)-1])})
			}
			want := testCase.feedings
			if !slices.Equal(got, want) {
				t.Errorf("feedings %v, want %v", got, want)
			}
		})
	}
}
