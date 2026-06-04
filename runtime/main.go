package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/elazarl/goproxy"
	_ "github.com/mattn/go-sqlite3"
	"github.com/mintryfabric/mintry-fabric-runtime/internal/cache"
	"gopkg.in/yaml.v3"
)

const (
	defaultListenAddr            = ":8080"
	defaultTelemetryAddr         = ":8081"
	defaultDBPath                = "cache.db"
	encryptionKeyEnvVar          = "MINTRY_SQLCIPHER_KEY"
	costPerCallZAR       float64 = 46.50 // Estimated cost per vendor call in ZAR
)

var dynamicMetadataKeys = []string{"timestamp", "requestId", "clientNonce", "nonce", "correlationId", "x-request-id"}

// Global telemetry tracker
var telemetry *cache.Telemetry

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// RouteConfig defines a caching rule for a matched vendor endpoint.
type RouteConfig struct {
	Match       string `yaml:"match"`
	TTL         string `yaml:"ttl"`
	CacheErrors bool   `yaml:"cache_errors"`
}

// Config defines the runtime rule engine.
type Config struct {
	CACert string        `yaml:"ca_cert"`
	CAKey  string        `yaml:"ca_key"`
	Routes []RouteConfig `yaml:"routes"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// ---------------------------------------------------------------------------
// Entry Point
// ---------------------------------------------------------------------------

func main() {
	telemetry = cache.NewTelemetry()

	config, err := loadConfig("config.yaml")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	store, err := newCacheStore(defaultDBPath)
	if err != nil {
		log.Fatalf("failed to initialize cache store: %v", err)
	}
	defer store.db.Close()

	go store.pruneExpiredLoop()
	go startTelemetryServer()

	// Load CA certificate for MITM interception
	mintryCA, err := loadMintryCA(config.CACert, config.CAKey)
	if err != nil {
		log.Fatalf("failed to load Mintry CA: %v", err)
	}

	// Extract unique vendor hosts from configured routes
	vendorHosts := extractVendorHosts(config)

	// Build the MITM ConnectAction using our custom CA
	mitmAction := &goproxy.ConnectAction{
		Action:    goproxy.ConnectMitm,
		TLSConfig: goproxy.TLSConfigFromCA(&mintryCA),
	}

	// Create goproxy instance
	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = false

	// MITM: intercept CONNECT only for configured vendor hosts.
	// All other HTTPS traffic tunnels through untouched.
	proxy.OnRequest(vendorHostCondition(vendorHosts)).HandleConnectFunc(
		func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
			log.Printf("MITM INTERCEPT: %s", host)
			return mitmAction, host
		},
	)

	// After TLS decryption: check cache before forwarding to vendor.
	proxy.OnRequest(vendorRouteCondition(config)).DoFunc(
		makeCacheRequestHandler(config, store),
	)

	// After vendor response: cache the result for future hits.
	proxy.OnResponse(vendorRouteCondition(config)).DoFunc(
		makeCacheResponseHandler(config, store),
	)

	log.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Printf("  Mintry Fabric Runtime v0.2.0")
	log.Printf("  Proxy listening on         %s", defaultListenAddr)
	log.Printf("  Telemetry endpoint         http://localhost%s/metrics", defaultTelemetryAddr)
	log.Printf("  MITM enabled for %d vendor host(s):", len(vendorHosts))
	for _, h := range vendorHosts {
		log.Printf("    → %s", h)
	}
	log.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	log.Fatal(http.ListenAndServe(defaultListenAddr, proxy))
}

// ---------------------------------------------------------------------------
// TLS MITM — CA Loading
// ---------------------------------------------------------------------------

func loadMintryCA(certPath, keyPath string) (tls.Certificate, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("reading CA cert %q: %w", certPath, err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("reading CA key %q: %w", keyPath, err)
	}

	ca, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("parsing CA key pair: %w", err)
	}

	// Parse the leaf certificate so goproxy can sign dynamic certs
	if ca.Leaf == nil {
		block, _ := pem.Decode(certPEM)
		if block == nil {
			return tls.Certificate{}, fmt.Errorf("failed to decode CA cert PEM block")
		}
		ca.Leaf, err = x509.ParseCertificate(block.Bytes)
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("parsing CA leaf certificate: %w", err)
		}
	}

	log.Printf("Loaded Mintry Root CA: CN=%s (expires %s)",
		ca.Leaf.Subject.CommonName,
		ca.Leaf.NotAfter.Format("2006-01-02"),
	)

	return ca, nil
}

// ---------------------------------------------------------------------------
// TLS MITM — Condition Matching
// ---------------------------------------------------------------------------

// extractVendorHosts pulls unique hostnames from the route config.
// e.g. "api.transunion.co.za/v1/score" → "api.transunion.co.za"
func extractVendorHosts(cfg *Config) []string {
	seen := make(map[string]struct{})
	var hosts []string
	for _, route := range cfg.Routes {
		parts := strings.SplitN(route.Match, "/", 2)
		host := strings.ToLower(parts[0])
		if _, ok := seen[host]; !ok {
			seen[host] = struct{}{}
			hosts = append(hosts, host)
		}
	}
	return hosts
}

// vendorHostCondition matches CONNECT requests destined for configured vendor hosts.
// Ports are stripped from both sides so "127.0.0.1:45678" matches config host "127.0.0.1:45678".
func vendorHostCondition(hosts []string) goproxy.ReqConditionFunc {
	// Pre-normalize config hosts to bare hostnames
	cleanHosts := make([]string, len(hosts))
	for i, h := range hosts {
		cleanHosts[i] = stripPort(h)
	}

	return func(req *http.Request, ctx *goproxy.ProxyCtx) bool {
		reqHost := stripPort(strings.ToLower(req.URL.Host))
		for _, h := range cleanHosts {
			if reqHost == h {
				return true
			}
		}
		return false
	}
}

// stripPort removes the :port suffix from a host string.
func stripPort(host string) string {
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		return host[:idx]
	}
	return host
}

// vendorRouteCondition matches decrypted requests against full host+path route patterns.
func vendorRouteCondition(cfg *Config) goproxy.ReqConditionFunc {
	return func(req *http.Request, ctx *goproxy.ProxyCtx) bool {
		return findRoute(req, cfg) != nil
	}
}

// findRoute returns the first matching route config for a request.
func findRoute(r *http.Request, cfg *Config) *RouteConfig {
	target := strings.ToLower(r.Host + r.URL.Path)
	for _, route := range cfg.Routes {
		if strings.Contains(target, strings.ToLower(route.Match)) {
			return &route
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// TLS MITM — Request & Response Handlers
// ---------------------------------------------------------------------------

// proxyContext carries state between the OnRequest and OnResponse handlers
// via goproxy's ctx.UserData field.
type proxyContext struct {
	cacheKey string
	route    *RouteConfig
	reqStart time.Time
}

// makeCacheRequestHandler returns a goproxy OnRequest handler that checks
// the local cache before allowing the request to reach the vendor.
func makeCacheRequestHandler(cfg *Config, store *CacheStore) func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	return func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		route := findRoute(req, cfg)
		if route == nil {
			return req, nil
		}

		// Read and buffer the request body for cache key generation
		body, err := io.ReadAll(req.Body)
		if err != nil {
			log.Printf("[WARN] failed reading request body: %v", err)
			return req, nil // forward to vendor
		}
		req.Body = io.NopCloser(bytes.NewReader(body))

		cacheKey, err := buildCacheKey(req, body)
		if err != nil {
			log.Printf("[WARN] failed building cache key: %v", err)
			return req, nil
		}

		// Attempt cache lookup
		cached, found, err := store.get(cacheKey)
		if err != nil {
			log.Printf("[WARN] cache get error: %v", err)
		}

		if found {
			// ── CACHE HIT ──────────────────────────────────────────
			start := time.Now()
			resp := buildCachedHTTPResponse(req, cached)
			latencyMS := float64(time.Since(start).Microseconds()) / 1000.0
			telemetry.RecordCacheHit(req.Host+req.URL.Path, latencyMS)
			log.Printf("⚡ CACHE HIT  %s%s  (%.2fms)", req.Host, req.URL.Path, latencyMS)
			return req, resp // short-circuit — vendor is never called
		}

		// ── CACHE MISS ─────────────────────────────────────────
		// Stash context for the OnResponse handler
		ctx.UserData = &proxyContext{
			cacheKey: cacheKey,
			route:    route,
			reqStart: time.Now(),
		}
		return req, nil // forward to vendor
	}
}

// makeCacheResponseHandler returns a goproxy OnResponse handler that caches
// successful vendor responses for future deduplication.
func makeCacheResponseHandler(cfg *Config, store *CacheStore) func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
	return func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		pctx, ok := ctx.UserData.(*proxyContext)
		if !ok || pctx == nil {
			return resp // this was a cache hit — nothing to store
		}

		endpoint := ctx.Req.Host + ctx.Req.URL.Path
		latencyMS := float64(time.Since(pctx.reqStart).Milliseconds())

		// Only cache 200 OK unless cache_errors is enabled
		if resp.StatusCode != http.StatusOK && !pctx.route.CacheErrors {
			telemetry.RecordVendorCall(endpoint, latencyMS)
			log.Printf("🌐 VENDOR CALL %s  (%dms, status %d — not cached)",
				endpoint, int(latencyMS), resp.StatusCode)
			return resp
		}

		// Read the vendor response body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("[WARN] failed reading vendor response body: %v", err)
			telemetry.RecordVendorCall(endpoint, latencyMS)
			return resp
		}
		// Put it back for the client
		resp.Body = io.NopCloser(bytes.NewReader(body))

		// Parse TTL and commit to cache
		ttl, err := parseTTL(pctx.route.TTL)
		if err != nil {
			log.Printf("[WARN] invalid TTL for route %s: %v", pctx.route.Match, err)
			telemetry.RecordVendorCall(endpoint, latencyMS)
			return resp
		}

		cachedResp := &cachedResponse{
			Status:  resp.StatusCode,
			Headers: resp.Header.Clone(),
			Body:    body,
		}

		if err := store.put(pctx.cacheKey, cachedResp, ttl); err != nil {
			log.Printf("[WARN] failed to write cache entry: %v", err)
		} else {
			log.Printf("💾 CACHED     %s  (TTL: %s, %d bytes)", endpoint, pctx.route.TTL, len(body))
		}

		telemetry.RecordVendorCall(endpoint, latencyMS)
		return resp
	}
}

// buildCachedHTTPResponse constructs a full HTTP response from a cached entry,
// ready to be returned by goproxy without ever contacting the vendor.
func buildCachedHTTPResponse(req *http.Request, cached *cachedResponse) *http.Response {
	header := cached.Headers.Clone()
	header.Set("X-Mintry-Cache", "HIT")

	return &http.Response{
		StatusCode:    cached.Status,
		Status:        fmt.Sprintf("%d %s", cached.Status, http.StatusText(cached.Status)),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader(cached.Body)),
		ContentLength: int64(len(cached.Body)),
		Request:       req,
	}
}

// ---------------------------------------------------------------------------
// Telemetry Server
// ---------------------------------------------------------------------------

func startTelemetryServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", handleTelemetry)

	server := &http.Server{
		Addr:    defaultTelemetryAddr,
		Handler: mux,
	}

	log.Printf("Telemetry server started on %s", defaultTelemetryAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("telemetry server error: %v", err)
	}
}

func handleTelemetry(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Get memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	memoryUsageMB := float64(m.Alloc) / 1024 / 1024

	// Get current stats from telemetry tracker
	stats := telemetry.GetStats(memoryUsageMB, 0)

	json.NewEncoder(w).Encode(stats)
}

// ---------------------------------------------------------------------------
// SQLite WAL Cache Store
// ---------------------------------------------------------------------------

// CacheStore manages the local SQLite cache backed by WAL.
type CacheStore struct {
	db *sql.DB
}

type cachedResponse struct {
	Status  int
	Headers http.Header
	Body    []byte
}

func newCacheStore(path string) (*CacheStore, error) {
	key := os.Getenv(encryptionKeyEnvVar)
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)", path)
	if key != "" {
		dsn = fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma_key=%s", path, key)
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS cache (
			key TEXT PRIMARY KEY,
			status INTEGER NOT NULL,
			headers TEXT NOT NULL,
			body BLOB NOT NULL,
			expires_at INTEGER NOT NULL,
			created_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_expires_at ON cache(expires_at);
	`); err != nil {
		db.Close()
		return nil, err
	}

	return &CacheStore{db: db}, nil
}

func (s *CacheStore) pruneExpiredLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if _, err := s.db.Exec("DELETE FROM cache WHERE expires_at <= ?", time.Now().Unix()); err != nil {
			log.Printf("failed pruning expired cache rows: %v", err)
		}
	}
}

func (s *CacheStore) get(key string) (*cachedResponse, bool, error) {
	row := s.db.QueryRow("SELECT status, headers, body, expires_at FROM cache WHERE key = ?", key)

	var status int
	var headersJSON string
	var body []byte
	var expiresAt int64
	if err := row.Scan(&status, &headersJSON, &body, &expiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}

	if time.Now().Unix() >= expiresAt {
		return nil, false, nil
	}

	headers := http.Header{}
	if err := json.Unmarshal([]byte(headersJSON), &headers); err != nil {
		return nil, false, err
	}

	return &cachedResponse{Status: status, Headers: headers, Body: body}, true, nil
}

func (s *CacheStore) put(key string, resp *cachedResponse, ttl time.Duration) error {
	headersJSON, err := json.Marshal(resp.Headers)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(
		"INSERT INTO cache(key, status, headers, body, expires_at, created_at) VALUES(?, ?, ?, ?, ?, ?) ON CONFLICT(key) DO UPDATE SET status = excluded.status, headers = excluded.headers, body = excluded.body, expires_at = excluded.expires_at, created_at = excluded.created_at",
		key,
		resp.Status,
		string(headersJSON),
		resp.Body,
		time.Now().Add(ttl).Unix(),
		time.Now().Unix(),
	)

	return err
}

// ---------------------------------------------------------------------------
// Deterministic Cache Key Generation
// ---------------------------------------------------------------------------

func buildCacheKey(r *http.Request, body []byte) (string, error) {
	scheme := r.URL.Scheme
	if scheme == "" {
		scheme = "https"
	}
	target := fmt.Sprintf("%s://%s%s", scheme, strings.ToLower(r.Host), r.URL.RequestURI())
	normalized := string(body)
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") && len(body) > 0 {
		normalizedJSON, err := normalizeJSON(body)
		if err != nil {
			return "", err
		}
		normalized = normalizedJSON
	}

	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s", strings.ToUpper(r.Method), target, normalized)))
	return fmt.Sprintf("%x", sum), nil
}

func normalizeJSON(data []byte) (string, error) {
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", err
	}

	normalized := normalizeValue(parsed)
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func normalizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			if isDynamicMetadataKey(key) {
				continue
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)

		ordered := make(map[string]any, len(keys))
		for _, key := range keys {
			ordered[key] = normalizeValue(typed[key])
		}
		return ordered
	case []any:
		normalized := make([]any, len(typed))
		for i, item := range typed {
			normalized[i] = normalizeValue(item)
		}
		return normalized
	default:
		return typed
	}
}

func isDynamicMetadataKey(key string) bool {
	lower := strings.ToLower(key)
	for _, candidate := range dynamicMetadataKeys {
		if strings.EqualFold(lower, strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}

func parseTTL(ttl string) (time.Duration, error) {
	if strings.HasSuffix(ttl, "d") {
		number := strings.TrimSuffix(ttl, "d")
		value, err := time.ParseDuration(number + "h")
		if err != nil {
			return 0, err
		}
		return value * 24, nil
	}
	return time.ParseDuration(ttl)
}
