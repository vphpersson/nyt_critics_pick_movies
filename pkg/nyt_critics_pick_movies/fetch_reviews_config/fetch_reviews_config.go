package fetch_reviews_config

import (
	"fmt"
	"net/http"
	"time"

	motmedelErrors "github.com/Motmedel/utils_go/pkg/errors"
)

const (
	DefaultLimit = 30
	// MaxPageSize is the most reviews the API answers one request with; it
	// fails outright when asked for 120 or more.
	MaxPageSize = 100
)

type Config struct {
	// Limit is how many of the newest reviews are read, a page at a time.
	Limit int
	// Since, when set, stops the reading at the first review published
	// before it.
	Since      time.Time
	HttpClient *http.Client
	Now        func() time.Time
}

type Option func(*Config)

func New(options ...Option) *Config {
	config := &Config{
		Limit:      DefaultLimit,
		HttpClient: http.DefaultClient,
		Now:        time.Now,
	}
	for _, option := range options {
		if option != nil {
			option(config)
		}
	}

	return config
}

func WithLimit(limit int) Option {
	return func(config *Config) {
		config.Limit = limit
	}
}

func WithHttpClient(client *http.Client) Option {
	return func(config *Config) {
		config.HttpClient = client
	}
}

func WithSince(since time.Time) Option {
	return func(config *Config) {
		config.Since = since
	}
}

// ParseSince reads a date as YYYY-MM-DD (midnight UTC) or an RFC 3339 time.
func ParseSince(value string) (time.Time, error) {
	if since, err := time.Parse(time.DateOnly, value); err == nil {
		return since, nil
	}
	since, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, motmedelErrors.NewWithTrace(
			fmt.Errorf("%w: time parse: %w", motmedelErrors.ErrParseError, err),
			value,
		)
	}
	return since, nil
}

func WithNow(now func() time.Time) Option {
	return func(config *Config) {
		config.Now = now
	}
}
