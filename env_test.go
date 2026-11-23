package main

import (
	"errors"
	"strconv"
	"testing"
)

func TestReadIntEnv(t *testing.T) {
	testCases := []struct {
		name     string
		value    string
		expected int
		wantErr  bool
		// syntax says the parse error is kept.
		syntax bool
	}{
		{name: "unset", value: "", expected: 30},
		{name: "set", value: "500", expected: 500},
		{name: "zero", value: "0", wantErr: true},
		{name: "not a number", value: "many", wantErr: true, syntax: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("LIMIT", testCase.value)

			got, err := readIntEnv("LIMIT", 30)
			if testCase.wantErr {
				if !errors.Is(err, errInvalidEnv) || errors.Is(err, strconv.ErrSyntax) != testCase.syntax {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err != nil || got != testCase.expected {
				t.Errorf("unexpected: %d %v", got, err)
			}
		})
	}
}

func TestReadBoolEnv(t *testing.T) {
	testCases := []struct {
		name     string
		value    string
		expected bool
		wantErr  bool
	}{
		{name: "unset", value: ""},
		{name: "true", value: "true", expected: true},
		{name: "one", value: "1", expected: true},
		{name: "false", value: "false"},
		{name: "not a bool", value: "perhaps", wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("DRY_RUN", testCase.value)

			got, err := readBoolEnv("DRY_RUN")
			if (err != nil) != testCase.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if testCase.wantErr && (!errors.Is(err, errInvalidEnv) || !errors.Is(err, strconv.ErrSyntax)) {
				t.Errorf("the error does not keep its cause: %v", err)
			}
			if got != testCase.expected {
				t.Errorf("got %t, expected %t", got, testCase.expected)
			}
		})
	}
}

func TestFetchOptions(t *testing.T) {
	testCases := []struct {
		name        string
		since       string
		wantOptions int
		wantErr     bool
	}{
		{name: "no since", wantOptions: 1},
		{name: "a since", since: "2026-04-01", wantOptions: 2},
		{name: "a bad since", since: "April", wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("SINCE", testCase.since)

			options, err := fetchOptions(30)
			if testCase.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil || len(options) != testCase.wantOptions {
				t.Errorf("unexpected: %d options, %v", len(options), err)
			}
		})
	}
}
