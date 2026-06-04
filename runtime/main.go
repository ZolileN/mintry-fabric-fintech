package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
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
		log.Printf("[WARN] failed to initialize cache store: %v. Starting in transparent bypass (pass-through) mode.", err)
		store = &CacheStore{
			db:      nil,
			breaker: newCircuitBreaker(3, 5*time.Second, 150*time.Millisecond),
		}
		store.breaker.manualPassThrough = true
		_ = telemetry.SetDB(nil)
	} else {
		defer store.db.Close()
		if err := telemetry.SetDB(store.db); err != nil {
			log.Printf("[WARN] failed to bind telemetry database: %v. Telemetry will run in-memory.", err)
		}
		go store.pruneExpiredLoop()
	}

	go startTelemetryServer(config, store)

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
	configMu.RLock()
	defer configMu.RUnlock()
	target := strings.ToLower(r.Host + r.URL.Path)
	for _, route := range cfg.Routes {
		if strings.Contains(target, strings.ToLower(route.Match)) {
			copied := route
			return &copied
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
			ttlRemaining := formatTTLRemaining(cached.ExpiresAt)
			telemetry.RecordCacheHit(req.Host+req.URL.Path, latencyMS, ttlRemaining)
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

// WebSocket Broadcaster State
type WSClient struct {
	send chan []byte
}

var (
	wsClients   = make(map[*WSClient]bool)
	wsClientsMu sync.Mutex
	configMu    sync.RWMutex
)

func broadcastInterception(entry cache.InterceptionLogEntry) {
	msg, err := json.Marshal(entry)
	if err != nil {
		return
	}

	wsClientsMu.Lock()
	defer wsClientsMu.Unlock()
	for client := range wsClients {
		select {
		case client.send <- msg:
		default:
			// Client's channel is full; drop frame
		}
	}
}

func startTelemetryServer(cfg *Config, store *CacheStore) {
	// Hook up real-time interception alerts to WebSocket broadcaster
	telemetry.OnInterception = broadcastInterception

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", handleTelemetry)
	mux.HandleFunc("/api/routes", handleRoutesAPI(cfg))
	mux.HandleFunc("/api/metrics/history", handleMetricsHistoryAPI(store))
	mux.HandleFunc("/api/settings", handleSettingsAPI(store))
	mux.HandleFunc("/ws/feed", handleWSFeed)

	server := &http.Server{
		Addr:    defaultTelemetryAddr,
		Handler: mux,
	}

	log.Printf("Telemetry & Management API server started on %s", defaultTelemetryAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("telemetry server error: %v", err)
	}
}

func handleTelemetry(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Get memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	memoryUsageMB := float64(m.Alloc) / 1024 / 1024

	// Get current stats from telemetry tracker
	stats := telemetry.GetStats(memoryUsageMB, 0)

	json.NewEncoder(w).Encode(stats)
}

func handleRoutesAPI(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == http.MethodGet {
			data, err := os.ReadFile("config.yaml")
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to read config: %v", err), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"config_yaml": string(data),
			})
			return
		}

		if r.Method == http.MethodPost {
			var body struct {
				ConfigYAML string `json:"config_yaml"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}

			// Validate YAML syntax
			var newCfg Config
			if err := yaml.Unmarshal([]byte(body.ConfigYAML), &newCfg); err != nil {
				http.Error(w, fmt.Sprintf("invalid YAML syntax: %v", err), http.StatusBadRequest)
				return
			}

			// Write config.yaml to disk
			if err := os.WriteFile("config.yaml", []byte(body.ConfigYAML), 0644); err != nil {
				http.Error(w, fmt.Sprintf("failed to save config: %v", err), http.StatusInternalServerError)
				return
			}

			// Hot-reload in memory
			configMu.Lock()
			cfg.Routes = newCfg.Routes
			cfg.CACert = newCfg.CACert
			cfg.CAKey = newCfg.CAKey
			configMu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"status":  "success",
				"message": "Configuration hot-reloaded successfully",
			})
			return
		}

		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type ChartDataPoint struct {
	Date           string `json:"date"`
	TotalRequests  int    `json:"totalRequests"`
	CachedRequests int    `json:"cachedRequests"`
}

func handleMetricsHistoryAPI(store *CacheStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		// Query historical metrics from DB
		query := `
			SELECT 
				date(timestamp / 1000000000, 'unixepoch') as day,
				COUNT(*) as total,
				SUM(CASE WHEN action = 'CACHE_HIT' THEN 1 ELSE 0 END) as cached
			FROM telemetry_log
			GROUP BY day
			ORDER BY day ASC
			LIMIT 7;
		`
		if store == nil || store.db == nil {
			log.Printf("[WARN] metrics history queried but database is offline")
			json.NewEncoder(w).Encode([]ChartDataPoint{})
			return
		}
		rows, err := store.db.QueryContext(r.Context(), query)
		if err != nil {
			log.Printf("[WARN] failed to query metrics history: %v", err)
			json.NewEncoder(w).Encode([]ChartDataPoint{})
			return
		}
		defer rows.Close()

		var points []ChartDataPoint
		for rows.Next() {
			var p ChartDataPoint
			if err := rows.Scan(&p.Date, &p.TotalRequests, &p.CachedRequests); err != nil {
				log.Printf("[WARN] failed to scan history row: %v", err)
				http.Error(w, "database scan error", http.StatusInternalServerError)
				return
			}
			points = append(points, p)
		}

		// Fallback to empty days if database yields zero entries
		if len(points) == 0 {
			now := time.Now()
			for i := 6; i >= 0; i-- {
				day := now.AddDate(0, 0, -i).Format("2006-01-02")
				points = append(points, ChartDataPoint{
					Date:           day,
					TotalRequests:  0,
					CachedRequests: 0,
				})
			}
		}

		json.NewEncoder(w).Encode(points)
	}
}

func handleSettingsAPI(store *CacheStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == http.MethodGet {
			var totalCached int
			var totalLogs int
			if store != nil && store.db != nil {
				_ = store.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM cache").Scan(&totalCached)
				_ = store.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM telemetry_log").Scan(&totalLogs)
			}

			key := os.Getenv(encryptionKeyEnvVar)
			encryptionEnabled := key != ""

			breakerState := "CLOSED"
			passThroughForced := false
			if store != nil && store.breaker != nil {
				store.breaker.mu.RLock()
				breakerState = store.breaker.state.String()
				passThroughForced = store.breaker.manualPassThrough
				store.breaker.mu.RUnlock()
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"db_path":                 "cache.db",
				"encryption_enabled":      encryptionEnabled,
				"total_cached_records":    totalCached,
				"total_telemetry_records": totalLogs,
				"circuit_breaker_state":   breakerState,
				"pass_through_forced":     passThroughForced,
			})
			return
		}

		if r.Method == http.MethodPost {
			var body struct {
				Forced bool `json:"forced"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}

			if store != nil && store.breaker != nil {
				store.breaker.SetManualPassThrough(body.Forced)
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"status":              "success",
				"pass_through_forced": body.Forced,
			})
			return
		}

		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleWSFeed(w http.ResponseWriter, r *http.Request) {
	// CORS validation preflight
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "webserver doesn't support hijacking", http.StatusInternalServerError)
		return
	}
	conn, bufrw, err := hj.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	// WebSocket handshake according to RFC 6455
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return
	}
	h := sha1.New()
	h.Write([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	accept := base64.StdEncoding.EncodeToString(h.Sum(nil))

	bufrw.WriteString("HTTP/1.1 101 Switching Protocols\r\n")
	bufrw.WriteString("Upgrade: websocket\r\n")
	bufrw.WriteString("Connection: Upgrade\r\n")
	bufrw.WriteString("Sec-WebSocket-Accept: " + accept + "\r\n\r\n")
	bufrw.Flush()

	client := &WSClient{send: make(chan []byte, 100)}

	wsClientsMu.Lock()
	wsClients[client] = true
	wsClientsMu.Unlock()

	defer func() {
		wsClientsMu.Lock()
		delete(wsClients, client)
		wsClientsMu.Unlock()
	}()

	// Discard loop to detect when the client closes connection
	go func() {
		buf := make([]byte, 1024)
		for {
			_, err := conn.Read(buf)
			if err != nil {
				conn.Close()
				return
			}
		}
	}()

	// Write loop to stream data frames
	for msg := range client.send {
		length := len(msg)
		var frame []byte
		if length < 126 {
			frame = []byte{0x81, byte(length)}
		} else if length < 65536 {
			frame = []byte{0x81, 126, byte(length >> 8), byte(length & 0xff)}
		} else {
			continue
		}
		frame = append(frame, msg...)

		wsClientsMu.Lock()
		_, err := conn.Write(frame)
		wsClientsMu.Unlock()
		if err != nil {
			break
		}
	}
}

// ---------------------------------------------------------------------------
// SQLite WAL Cache Store
// ---------------------------------------------------------------------------

// CircuitState represents the operational state of the database circuit breaker.
type CircuitState int

const (
	StateClosed CircuitState = iota
	StateOpen
	StateHalfOpen
)

// CircuitBreaker acts as a failsafe gatekeeper. If database queries/writes fail or timeout
// consecutively, it trips to StateOpen to bypass database calls entirely (transparent pass-through).
type CircuitBreaker struct {
	mu                sync.RWMutex
	state             CircuitState
	consecutiveFails  int
	lastStateChange   time.Time
	
	failureThreshold  int
	cooldown          time.Duration
	opTimeout         time.Duration
	manualPassThrough bool
}

func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF-OPEN"
	default:
		return "UNKNOWN"
	}
}

func newCircuitBreaker(threshold int, cooldown time.Duration, opTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: threshold,
		cooldown:         cooldown,
		opTimeout:        opTimeout,
		lastStateChange:  time.Now(),
	}
}

func (cb *CircuitBreaker) SetManualPassThrough(val bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.manualPassThrough = val
}

func (cb *CircuitBreaker) GetManualPassThrough() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.manualPassThrough
}

func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.manualPassThrough {
		return false
	}

	if cb.state == StateOpen {
		if time.Since(cb.lastStateChange) > cb.cooldown {
			cb.state = StateHalfOpen
			cb.lastStateChange = time.Now()
			log.Printf("[BREAKER] Cooldown elapsed. Transitioning from OPEN to HALF-OPEN.")
			return true
		}
		return false
	}
	return true
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveFails = 0
	if cb.state == StateHalfOpen {
		cb.state = StateClosed
		cb.lastStateChange = time.Now()
		log.Printf("[BREAKER] DB operation succeeded in HALF-OPEN. Resetting breaker to CLOSED.")
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveFails++
	if cb.state == StateClosed && cb.consecutiveFails >= cb.failureThreshold {
		cb.state = StateOpen
		cb.lastStateChange = time.Now()
		log.Printf("[BREAKER] Consecutive database failures reached threshold (%d). Tripping breaker to OPEN.", cb.consecutiveFails)
	} else if cb.state == StateHalfOpen {
		cb.state = StateOpen
		cb.lastStateChange = time.Now()
		log.Printf("[BREAKER] Database operation failed in HALF-OPEN. Breaker returned to OPEN.")
	}
}

// CacheStore manages the local SQLite cache backed by WAL, protected by a circuit breaker.
type CacheStore struct {
	db      *sql.DB
	breaker *CircuitBreaker
}

type cachedResponse struct {
	Status    int
	Headers   http.Header
	Body      []byte
	ExpiresAt int64
}

func newCacheStore(path string) (*CacheStore, error) {
	key := os.Getenv(encryptionKeyEnvVar)
	
	var dsn string
	if key != "" {
		// Pass the key via URL query parameter so SQLCipher can unlock the database
		// during sqlite3_open_v2, before go-sqlite3 executes default pragmas.
		// Use url.QueryEscape to ensure special characters in the key are safely encoded.
		dsn = fmt.Sprintf("file:%s?_journal_mode=WAL&key=%s", path, url.QueryEscape(key))
	} else {
		dsn = fmt.Sprintf("file:%s?_journal_mode=WAL", path)
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	// Restrict to a single connection in the pool so write operations and transaction
	// locks behave deterministically.
	db.SetMaxOpenConns(1)

	// If encryption key is provided, verify it immediately by performing a test query.
	// If the key is invalid or missing on an encrypted file, the query will fail.
	if key != "" {
		var testVal int
		if err := db.QueryRow("SELECT count(*) FROM sqlite_master;").Scan(&testVal); err != nil {
			db.Close()
			return nil, fmt.Errorf("invalid database encryption key: %w", err)
		}
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

	// Default circuit breaker config: 3 failures threshold, 5 seconds cooldown, 150ms timeout.
	breaker := newCircuitBreaker(3, 5*time.Second, 150*time.Millisecond)

	return &CacheStore{db: db, breaker: breaker}, nil
}

func (s *CacheStore) pruneExpiredLoop() {
	if s == nil || s.db == nil {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if s.breaker != nil && !s.breaker.Allow() {
			continue // skip pruning if breaker is tripped
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := s.db.ExecContext(ctx, "DELETE FROM cache WHERE expires_at <= ?", time.Now().Unix())
		cancel()
		if err != nil {
			log.Printf("failed pruning expired cache rows: %v", err)
		}
	}
}

func (s *CacheStore) get(key string) (*cachedResponse, bool, error) {
	if s.breaker != nil && !s.breaker.Allow() {
		return nil, false, errors.New("circuit breaker is OPEN")
	}

	if s == nil || s.db == nil {
		return nil, false, errors.New("database is offline")
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.breaker.opTimeout)
	defer cancel()

	var status int
	var headersJSON string
	var body []byte
	var expiresAt int64

	row := s.db.QueryRowContext(ctx, "SELECT status, headers, body, expires_at FROM cache WHERE key = ?", key)
	if err := row.Scan(&status, &headersJSON, &body, &expiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.breaker.RecordSuccess()
			return nil, false, nil
		}
		s.breaker.RecordFailure()
		return nil, false, err
	}

	s.breaker.RecordSuccess()

	if time.Now().Unix() >= expiresAt {
		return nil, false, nil
	}

	headers := http.Header{}
	if err := json.Unmarshal([]byte(headersJSON), &headers); err != nil {
		return nil, false, err
	}

	return &cachedResponse{
		Status:    status,
		Headers:   headers,
		Body:      body,
		ExpiresAt: expiresAt,
	}, true, nil
}

func (s *CacheStore) put(key string, resp *cachedResponse, ttl time.Duration) error {
	if s.breaker != nil && !s.breaker.Allow() {
		return errors.New("circuit breaker is OPEN")
	}

	if s == nil || s.db == nil {
		return errors.New("database is offline")
	}

	headersJSON, err := json.Marshal(resp.Headers)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.breaker.opTimeout)
	defer cancel()

	_, err = s.db.ExecContext(ctx,
		"INSERT INTO cache(key, status, headers, body, expires_at, created_at) VALUES(?, ?, ?, ?, ?, ?) ON CONFLICT(key) DO UPDATE SET status = excluded.status, headers = excluded.headers, body = excluded.body, expires_at = excluded.expires_at, created_at = excluded.created_at",
		key,
		resp.Status,
		string(headersJSON),
		resp.Body,
		time.Now().Add(ttl).Unix(),
		time.Now().Unix(),
	)

	if err != nil {
		s.breaker.RecordFailure()
		return err
	}

	s.breaker.RecordSuccess()
	return nil
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

func formatTTLRemaining(expiresAt int64) string {
	remaining := time.Until(time.Unix(expiresAt, 0))
	if remaining <= 0 {
		return "expired"
	}

	days := int(remaining.Hours()) / 24
	hours := int(remaining.Hours()) % 24
	minutes := int(remaining.Minutes()) % 60
	seconds := int(remaining.Seconds()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}
