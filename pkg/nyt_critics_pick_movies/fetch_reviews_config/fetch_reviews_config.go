package fetch_reviews_config

import (
	"net/http"
	"time"
)

const DefaultLimit = 30

type Config struct {
	Limit      int
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

func WithNow(now func() time.Time) Option {
	return func(config *Config) {
		config.Now = now
	}
}
