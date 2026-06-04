package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elazarl/goproxy"
	"github.com/mintryfabric/mintry-fabric-runtime/internal/cache"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// testInfra bundles all the moving parts of the integration test so individual
// test functions stay clean.
type testInfra struct {
	VendorServer *httptest.Server   // mock HTTPS vendor
	ProxyAddr    string             // "127.0.0.1:<port>"
	Client       *http.Client       // pre-configured to route through the proxy
	Store        *CacheStore        // the SQLite cache used by the proxy
	TargetURL    string             // full URL to the mock vendor endpoint
	VendorHits   *atomic.Int64      // counts how many times the vendor was actually called
	cleanup      func()
}

func setupTestInfra(t *testing.T) *testInfra {
	t.Helper()

	// Ensure global telemetry is initialised
	if telemetry == nil {
		telemetry = cache.NewTelemetry()
	}

	// ── 1. Mock Vendor (HTTPS) ─────────────────────────────────────────
	var vendorHits atomic.Int64
	vendorServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vendorHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Vendor", "TransUnion-Mock")
		json.NewEncoder(w).Encode(map[string]any{
			"score":  750,
			"status": "approved",
			"bureau": "TransUnion",
		})
	}))

	vendorURL, _ := url.Parse(vendorServer.URL)
	vendorAddr := vendorURL.Host // e.g. "127.0.0.1:45678"

	// ── 2. Cache Store (temp DB) ───────────────────────────────────────
	testDB := filepath.Join(t.TempDir(), "integration_cache.db")
	store, err := newCacheStore(testDB)
	if err != nil {
		t.Fatalf("failed to create test cache store: %v", err)
	}

	// ── 3. Load Mintry Root CA ─────────────────────────────────────────
	mintryCA, err := loadMintryCA("../mintry-root.crt", "../mintry-root.key")
	if err != nil {
		t.Fatalf("failed to load Mintry CA: %v", err)
	}

	// ── 4. Build Config ────────────────────────────────────────────────
	cfg := &Config{
		Routes: []RouteConfig{
			{Match: vendorAddr + "/v1/score", TTL: "1h", CacheErrors: false},
		},
	}

	vendorHosts := extractVendorHosts(cfg)

	mitmAction := &goproxy.ConnectAction{
		Action:    goproxy.ConnectMitm,
		TLSConfig: goproxy.TLSConfigFromCA(&mintryCA),
	}

	// ── 5. Build Proxy ─────────────────────────────────────────────────
	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = false

	// Trust the mock vendor's self-signed cert for outbound connections
	proxy.Tr = &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // only in tests — mock vendor uses httptest self-signed cert
		},
	}

	proxy.OnRequest(vendorHostCondition(vendorHosts)).HandleConnectFunc(
		func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
			return mitmAction, host
		},
	)
	proxy.OnRequest(vendorRouteCondition(cfg)).DoFunc(
		makeCacheRequestHandler(cfg, store),
	)
	proxy.OnResponse(vendorRouteCondition(cfg)).DoFunc(
		makeCacheResponseHandler(cfg, store),
	)

	// ── 6. Start Proxy on Random Port ──────────────────────────────────
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen for proxy: %v", err)
	}

	proxyServer := &http.Server{Handler: proxy}
	go proxyServer.Serve(listener)

	proxyAddr := listener.Addr().String()

	// ── 7. Build Client ────────────────────────────────────────────────
	// Client trusts the Mintry Root CA (for MITM-signed dynamic certs)
	caPool := x509.NewCertPool()
	caPool.AddCert(mintryCA.Leaf)

	proxyURL, _ := url.Parse("http://" + proxyAddr)
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				RootCAs: caPool,
			},
		},
	}

	targetURL := fmt.Sprintf("https://%s/v1/score", vendorAddr)

	return &testInfra{
		VendorServer: vendorServer,
		ProxyAddr:    proxyAddr,
		Client:       client,
		Store:        store,
		TargetURL:    targetURL,
		VendorHits:   &vendorHits,
		cleanup: func() {
			proxyServer.Close()
			listener.Close()
			vendorServer.Close()
			store.db.Close()
		},
	}
}

// ---------------------------------------------------------------------------
// Integration Tests
// ---------------------------------------------------------------------------

// TestMITMCacheHitFlow is the core "double-tap" test.
// Request 1 should be a cache MISS (vendor is called).
// Request 2 with the same payload should be a cache HIT (vendor is NOT called).
func TestMITMCacheHitFlow(t *testing.T) {
	infra := setupTestInfra(t)
	defer infra.cleanup()

	payload := `{"id_number":"9001015800086","product":"credit_score"}`

	// ── Request 1: Cache MISS ──────────────────────────────────────────
	resp1, err := infra.Client.Post(infra.TargetURL, "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("request 1 failed: %v", err)
	}
	defer resp1.Body.Close()

	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("request 1: expected status 200, got %d", resp1.StatusCode)
	}

	// Cache miss → no X-Mintry-Cache header
	if got := resp1.Header.Get("X-Mintry-Cache"); got != "" {
		t.Fatalf("request 1: expected no X-Mintry-Cache header (cache miss), got %q", got)
	}

	// Vendor header should be present (response came from real vendor)
	if got := resp1.Header.Get("X-Vendor"); got != "TransUnion-Mock" {
		t.Fatalf("request 1: expected X-Vendor header from mock vendor, got %q", got)
	}

	// Verify JSON payload
	body1, _ := io.ReadAll(resp1.Body)
	var result1 map[string]any
	if err := json.Unmarshal(body1, &result1); err != nil {
		t.Fatalf("request 1: failed to decode JSON: %v", err)
	}
	if result1["score"] != float64(750) {
		t.Fatalf("request 1: expected score 750, got %v", result1["score"])
	}

	if infra.VendorHits.Load() != 1 {
		t.Fatalf("expected vendor to be called exactly 1 time, got %d", infra.VendorHits.Load())
	}

	// ── Request 2: Cache HIT ───────────────────────────────────────────
	resp2, err := infra.Client.Post(infra.TargetURL, "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("request 2 failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("request 2: expected status 200, got %d", resp2.StatusCode)
	}

	// Cache hit → X-Mintry-Cache: HIT
	if got := resp2.Header.Get("X-Mintry-Cache"); got != "HIT" {
		t.Fatalf("request 2: expected X-Mintry-Cache=HIT, got %q", got)
	}

	// Verify identical JSON payload from cache
	body2, _ := io.ReadAll(resp2.Body)
	var result2 map[string]any
	if err := json.Unmarshal(body2, &result2); err != nil {
		t.Fatalf("request 2: failed to decode JSON: %v", err)
	}
	if result2["score"] != float64(750) {
		t.Fatalf("request 2: expected score 750, got %v", result2["score"])
	}

	// Vendor should STILL have been called exactly once — the second request was served from cache
	if infra.VendorHits.Load() != 1 {
		t.Fatalf("vendor was called %d times — expected exactly 1 (second request should be a cache hit)",
			infra.VendorHits.Load())
	}

	t.Logf("✅ Double-tap test passed: 2 requests, 1 vendor call, 1 cache hit")
}

// TestMITMCacheMissOnDifferentPayload verifies that different request bodies
// produce different cache keys and both result in vendor calls.
func TestMITMCacheMissOnDifferentPayload(t *testing.T) {
	infra := setupTestInfra(t)
	defer infra.cleanup()

	// Request A
	resp1, err := infra.Client.Post(infra.TargetURL, "application/json",
		strings.NewReader(`{"id_number":"9001015800086"}`))
	if err != nil {
		t.Fatalf("request A failed: %v", err)
	}
	resp1.Body.Close()

	// Request B — different ID number
	resp2, err := infra.Client.Post(infra.TargetURL, "application/json",
		strings.NewReader(`{"id_number":"8505225800083"}`))
	if err != nil {
		t.Fatalf("request B failed: %v", err)
	}
	resp2.Body.Close()

	// Both should hit the vendor (different cache keys)
	if infra.VendorHits.Load() != 2 {
		t.Fatalf("expected 2 vendor calls for different payloads, got %d", infra.VendorHits.Load())
	}

	// Neither should have cache hit headers
	if resp1.Header.Get("X-Mintry-Cache") != "" {
		t.Fatal("request A should be a cache miss")
	}
	if resp2.Header.Get("X-Mintry-Cache") != "" {
		t.Fatal("request B should be a cache miss")
	}

	t.Logf("✅ Different payloads correctly produced separate cache entries")
}

// TestMITMCacheKeyStableAcrossJSONReordering verifies that JSON key reordering
// and dynamic metadata stripping produce the same cache key, resulting in a
// cache hit even when the raw JSON bytes differ.
func TestMITMCacheKeyStableAcrossJSONReordering(t *testing.T) {
	infra := setupTestInfra(t)
	defer infra.cleanup()

	// Request 1 — keys in one order, with a timestamp
	resp1, err := infra.Client.Post(infra.TargetURL, "application/json",
		strings.NewReader(`{"id_number":"9001015800086","product":"score","timestamp":"2026-06-04T14:00:00Z"}`))
	if err != nil {
		t.Fatalf("request 1 failed: %v", err)
	}
	resp1.Body.Close()

	// Request 2 — keys reordered, different timestamp (should be stripped)
	resp2, err := infra.Client.Post(infra.TargetURL, "application/json",
		strings.NewReader(`{"timestamp":"2026-06-04T15:00:00Z","product":"score","id_number":"9001015800086"}`))
	if err != nil {
		t.Fatalf("request 2 failed: %v", err)
	}
	resp2.Body.Close()

	// Vendor should only be called once — second request matches the normalized cache key
	if infra.VendorHits.Load() != 1 {
		t.Fatalf("expected 1 vendor call (JSON normalization should deduplicate), got %d",
			infra.VendorHits.Load())
	}

	if resp2.Header.Get("X-Mintry-Cache") != "HIT" {
		t.Fatal("request 2 should be a cache hit after JSON normalization")
	}

	t.Logf("✅ JSON reordering + timestamp stripping correctly deduplicated")
}

// TestMITMNonCacheableStatusCode verifies that non-200 vendor responses
// are NOT cached when cache_errors is false.
func TestMITMNonCacheableStatusCode(t *testing.T) {
	// Ensure global telemetry is initialised
	if telemetry == nil {
		telemetry = cache.NewTelemetry()
	}

	// Custom vendor that returns 429
	var vendorHits atomic.Int64
	vendorServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vendorHits.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate_limited"}`))
	}))
	defer vendorServer.Close()

	vendorURL, _ := url.Parse(vendorServer.URL)
	vendorAddr := vendorURL.Host

	testDB := filepath.Join(t.TempDir(), "err_cache.db")
	store, err := newCacheStore(testDB)
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()

	mintryCA, err := loadMintryCA("../mintry-root.crt", "../mintry-root.key")
	if err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		Routes: []RouteConfig{
			{Match: vendorAddr + "/v1/score", TTL: "1h", CacheErrors: false},
		},
	}

	vendorHosts := extractVendorHosts(cfg)
	mitmAction := &goproxy.ConnectAction{
		Action:    goproxy.ConnectMitm,
		TLSConfig: goproxy.TLSConfigFromCA(&mintryCA),
	}

	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = false
	proxy.Tr = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	proxy.OnRequest(vendorHostCondition(vendorHosts)).HandleConnectFunc(
		func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
			return mitmAction, host
		},
	)
	proxy.OnRequest(vendorRouteCondition(cfg)).DoFunc(makeCacheRequestHandler(cfg, store))
	proxy.OnResponse(vendorRouteCondition(cfg)).DoFunc(makeCacheResponseHandler(cfg, store))

	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	defer listener.Close()
	proxyServer := &http.Server{Handler: proxy}
	go proxyServer.Serve(listener)
	defer proxyServer.Close()

	caPool := x509.NewCertPool()
	caPool.AddCert(mintryCA.Leaf)
	proxyURL, _ := url.Parse("http://" + listener.Addr().String())
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{RootCAs: caPool},
		},
	}

	targetURL := fmt.Sprintf("https://%s/v1/score", vendorAddr)
	payload := `{"id_number":"9001015800086"}`

	// Request 1 — vendor returns 429
	resp1, err := client.Post(targetURL, "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	resp1.Body.Close()

	// Request 2 — same payload, should NOT be cached (429 is not cacheable)
	resp2, err := client.Post(targetURL, "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()

	// Both requests should hit the vendor
	if vendorHits.Load() != 2 {
		t.Fatalf("expected 2 vendor calls (429s should not be cached), got %d", vendorHits.Load())
	}

	if resp2.Header.Get("X-Mintry-Cache") != "" {
		t.Fatal("429 responses should not produce cache hits")
	}

	t.Logf("✅ Non-200 responses correctly excluded from cache")
}
