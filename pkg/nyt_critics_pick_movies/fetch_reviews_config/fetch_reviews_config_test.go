package fetch_reviews_config

import (
	"errors"
	"net/http"
	"testing"
	"time"

	altshiftErrors "github.com/altshiftab/utils_go/pkg/errors"
)

func TestParseSince(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		value    string
		expected time.Time
		wantErr  bool
	}{
		{name: "a date", value: "2026-04-01", expected: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)},
		{name: "a time", value: "2026-04-01T12:30:00+02:00", expected: time.Date(2026, 4, 1, 10, 30, 0, 0, time.UTC)},
		{name: "neither", value: "April", wantErr: true},
		{name: "empty", value: "", wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			since, err := ParseSince(testCase.value)
			if testCase.wantErr {
				if !errors.Is(err, altshiftErrors.ErrParseError) {
					t.Fatalf("expected a parse error, got %v", err)
				}
				return
			}
			if err != nil || !since.Equal(testCase.expected) {
				t.Errorf("unexpected: %v %v", since, err)
			}
		})
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	defaults := New()
	if defaults.Limit != DefaultLimit || !defaults.Since.IsZero() || defaults.HttpClient != http.DefaultClient || defaults.Now == nil {
		t.Errorf("unexpected defaults: %+v", defaults)
	}

	since := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	client := &http.Client{}
	now := func() time.Time { return since }
	config := New(WithLimit(500), WithSince(since), WithHttpClient(client), WithNow(now), nil)
	if config.Limit != 500 || !config.Since.Equal(since) || config.HttpClient != client || !config.Now().Equal(since) {
		t.Errorf("unexpected config: %+v", config)
	}
}
