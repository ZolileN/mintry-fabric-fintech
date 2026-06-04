package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestParseTTL(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"5m", "5m0s"},
		{"24h", "24h0m0s"},
		{"2d", "48h0m0s"},
	}

	for _, tc := range cases {
		d, err := parseTTL(tc.input)
		if err != nil {
			t.Fatalf("parseTTL(%q) returned error: %v", tc.input, err)
		}
		if d.String() != tc.expected {
			t.Fatalf("parseTTL(%q) = %q, want %q", tc.input, d.String(), tc.expected)
		}
	}
}

func TestNormalizeJSON(t *testing.T) {
	input := `{"z": 1, "timestamp": "ignored", "a": {"b": 2, "requestId": "drop"}}`
	expected := `{"a":{"b":2},"z":1}`

	normalized, err := normalizeJSON([]byte(input))
	if err != nil {
		t.Fatalf("normalizeJSON returned error: %v", err)
	}
	if normalized != expected {
		t.Fatalf("normalizeJSON = %q, want %q", normalized, expected)
	}
}

func TestBuildCacheKeyStableForJSONBody(t *testing.T) {
	req1, err := http.NewRequest(http.MethodPost, "https://api.example.com/v1/verify?foo=1", strings.NewReader(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	req1.Header.Set("Content-Type", "application/json")

	req2, err := http.NewRequest(http.MethodPost, "https://api.example.com/v1/verify?foo=1", strings.NewReader(`{"a":1,"b":2}`))
	if err != nil {
		t.Fatal(err)
	}
	req2.Header.Set("Content-Type", "application/json")

	key1, err := buildCacheKey(req1, []byte(`{"b":2,"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	key2, err := buildCacheKey(req2, []byte(`{"a":1,"b":2}`))
	if err != nil {
		t.Fatal(err)
	}

	if key1 != key2 {
		t.Fatalf("expected stable cache key for reordered JSON bodies, got %q and %q", key1, key2)
	}
}

func TestBuildCacheKeyDiffersForQueryOrMethod(t *testing.T) {
	reqGet, err := http.NewRequest(http.MethodGet, "https://api.example.com/v1/verify?foo=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	keyGet, err := buildCacheKey(reqGet, nil)
	if err != nil {
		t.Fatal(err)
	}

	reqPost, err := http.NewRequest(http.MethodPost, "https://api.example.com/v1/verify?foo=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	keyPost, err := buildCacheKey(reqPost, nil)
	if err != nil {
		t.Fatal(err)
	}

	if keyGet == keyPost {
		t.Fatalf("expected different cache keys for different methods, got %q", keyGet)
	}

	reqDifferentQuery, err := http.NewRequest(http.MethodGet, "https://api.example.com/v1/verify?foo=2", nil)
	if err != nil {
		t.Fatal(err)
	}
	keyDifferentQuery, err := buildCacheKey(reqDifferentQuery, nil)
	if err != nil {
		t.Fatal(err)
	}

	if keyGet == keyDifferentQuery {
		t.Fatalf("expected different cache keys for different queries, got %q", keyGet)
	}
}

func TestExtractVendorHosts(t *testing.T) {
	cfg := &Config{
		Routes: []RouteConfig{
			{Match: "api.transunion.co.za/v1/score", TTL: "30d"},
			{Match: "api.transunion.co.za/v1/consumer", TTL: "14d"},
			{Match: "api.smileid.com/v1/async/verify", TTL: "7d"},
		},
	}

	hosts := extractVendorHosts(cfg)
	if len(hosts) != 2 {
		t.Fatalf("expected 2 unique hosts, got %d: %v", len(hosts), hosts)
	}

	hostSet := make(map[string]bool)
	for _, h := range hosts {
		hostSet[h] = true
	}

	if !hostSet["api.transunion.co.za"] {
		t.Fatal("expected api.transunion.co.za in extracted hosts")
	}
	if !hostSet["api.smileid.com"] {
		t.Fatal("expected api.smileid.com in extracted hosts")
	}
}

func TestFindRoute(t *testing.T) {
	cfg := &Config{
		Routes: []RouteConfig{
			{Match: "api.transunion.co.za/v1/score", TTL: "30d"},
			{Match: "api.smileid.com/v1/async/verify", TTL: "7d"},
		},
	}

	// Should match
	req1, _ := http.NewRequest(http.MethodPost, "https://api.transunion.co.za/v1/score", nil)
	req1.Host = "api.transunion.co.za"
	route := findRoute(req1, cfg)
	if route == nil {
		t.Fatal("expected route match for api.transunion.co.za/v1/score")
	}
	if route.TTL != "30d" {
		t.Fatalf("expected TTL 30d, got %s", route.TTL)
	}

	// Should not match
	req2, _ := http.NewRequest(http.MethodGet, "https://api.google.com/maps", nil)
	req2.Host = "api.google.com"
	route2 := findRoute(req2, cfg)
	if route2 != nil {
		t.Fatal("expected no route match for api.google.com")
	}
}
