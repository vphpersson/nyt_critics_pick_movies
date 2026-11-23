package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	altshiftErrors "github.com/altshiftab/utils_go/pkg/errors"
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

func TestFeedOnceErrors(t *testing.T) {
	t.Parallel()

	const token = "a-secret-token"
	answering := func(status int, answer string) func(t *testing.T) *url.URL {
		return func(t *testing.T) *url.URL {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(status)
				_, _ = writer.Write([]byte(answer))
			}))
			t.Cleanup(server.Close)
			base, _ := url.Parse(server.URL)
			return base
		}
	}
	testCases := []struct {
		name string
		base func(t *testing.T) *url.URL
		is   error
		// input is what the error carries, as text, of the address the server was given.
		input func(address string) string
	}{
		{
			name:  "a request that cannot be made",
			base:  func(*testing.T) *url.URL { return &url.URL{Scheme: "http", Host: "bad host"} },
			input: func(address string) string { return address },
		},
		{
			name: "a feeding that cannot be sent",
			base: func(*testing.T) *url.URL {
				server := httptest.NewServer(http.NotFoundHandler())
				server.Close()
				base, _ := url.Parse(server.URL)
				return base
			},
			input: func(address string) string { return address },
		},
		{
			name: "a refusal", base: answering(http.StatusUnauthorized, "no"), is: errRefused,
			input: func(address string) string { return fmt.Sprint([]any{address, http.StatusUnauthorized}) },
		},
		{
			name: "an answer that is not JSON", base: answering(http.StatusOK, "<html>"),
			input: func(string) string { return "<html>" },
		},
		{
			name: "an answer of too few results", base: answering(http.StatusOK, `{"results":[]}`), is: errRefused,
			input: func(string) string { return `{"results":[]}` },
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			base := testCase.base(t)
			_, err := feedOnce(t.Context(), http.DefaultClient, base, &fixedToken{token: token}, &feeding{Feed: "nyt", Picks: newestFirst(1)})
			if err == nil || testCase.is != nil && !errors.Is(err, testCase.is) {
				t.Fatalf("err %v", err)
			}
			traced, ok := errors.AsType[*altshiftErrors.ExtendedError](err)
			if !ok || traced.GetStackTrace() == "" {
				t.Fatalf("no trace: %v", err)
			}
			input := fmt.Sprint(traced.GetInput())
			if answer, isBytes := traced.GetInput().([]byte); isBytes {
				input = string(answer)
			}
			if want := testCase.input(base.JoinPath(feedPath).String()); input != want {
				t.Errorf("input %q, want %q", input, want)
			}
			if strings.Contains(err.Error(), token) || strings.Contains(input, token) {
				t.Errorf("the token in the error: %v, %q", err, input)
			}
		})
	}
}
