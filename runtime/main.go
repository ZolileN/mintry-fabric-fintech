package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"gopkg.in/yaml.v3"
)

const (
	defaultListenAddr   = ":8080"
	defaultDBPath       = "cache.db"
	encryptionKeyEnvVar = "MINTRY_SQLCIPHER_KEY"
)

var dynamicMetadataKeys = []string{"timestamp", "requestId", "clientNonce", "nonce", "correlationId", "x-request-id"}

var hopByHopHeaders = []string{
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"TE",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

// RouteConfig defines a caching rule for a matched vendor endpoint.
type RouteConfig struct {
	Match       string `yaml:"match"`
	TTL         string `yaml:"ttl"`
	CacheErrors bool   `yaml:"cache_errors"`
}

// Config defines the runtime rule engine.
type Config struct {
	Routes []RouteConfig `yaml:"routes"`
}

// CacheStore manages the local SQLite cache backed by WAL.
type CacheStore struct {
	db *sql.DB
}

func main() {
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

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleProxy(w, r, config, store)
	})

	server := &http.Server{
		Addr:    defaultListenAddr,
		Handler: handler,
	}

	log.Printf("Mintry Fabric runtime listening on %s", defaultListenAddr)
	log.Fatal(server.ListenAndServe())
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

func handleProxy(w http.ResponseWriter, r *http.Request, cfg *Config, store *CacheStore) {
	if r.Method == http.MethodConnect {
		handleConnect(w, r)
		return
	}

	matchRoute := findRoute(r, cfg)
	if matchRoute == nil {
		proxyDirect(w, r)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("failed reading request body: %v", err)
		proxyDirect(w, r)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	cacheKey, err := buildCacheKey(r, body)
	if err != nil {
		log.Printf("failed building cache key: %v", err)
		proxyDirect(w, r)
		return
	}

	cached, found, err := store.get(cacheKey)
	if err != nil {
		log.Printf("cache get error: %v", err)
	}
	if found {
		writeCachedResponse(w, cached)
		return
	}

	proxyResponse, err := proxyRequest(r)
	if err != nil {
		log.Printf("proxy request failed: %v", err)
		http.Error(w, "proxy failure", http.StatusBadGateway)
		return
	}
	defer proxyResponse.Body.Close()

	respBody, err := io.ReadAll(proxyResponse.Body)
	if err != nil {
		log.Printf("failed reading response body: %v", err)
		http.Error(w, "response read error", http.StatusBadGateway)
		return
	}

	copyHeaders(w.Header(), proxyResponse.Header)
	w.WriteHeader(proxyResponse.StatusCode)
	if _, err := w.Write(respBody); err != nil {
		log.Printf("failed writing response body: %v", err)
	}

	if proxyResponse.StatusCode == http.StatusOK || matchRoute.CacheErrors {
		cachedResp := &cachedResponse{Status: proxyResponse.StatusCode, Headers: proxyResponse.Header, Body: respBody}
		ttl, err := parseTTL(matchRoute.TTL)
		if err != nil {
			log.Printf("invalid TTL for route %s: %v", matchRoute.Match, err)
			return
		}
		if err := store.put(cacheKey, cachedResp, ttl); err != nil {
			log.Printf("failed to write cache entry: %v", err)
		}
	}
}

func handleConnect(w http.ResponseWriter, r *http.Request) {
	target := r.Host
	conn, err := net.DialTimeout("tcp", target, 15*time.Second)
	if err != nil {
		log.Printf("connect dial error: %v", err)
		http.Error(w, "failed to establish tunnel", http.StatusServiceUnavailable)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		log.Printf("hijack error: %v", err)
		return
	}

	_, err = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if err != nil {
		clientConn.Close()
		conn.Close()
		return
	}

	go copyStream(conn, clientConn)
	copyStream(clientConn, conn)
}

func copyStream(dst net.Conn, src net.Conn) {
	defer dst.Close()
	defer src.Close()
	io.Copy(dst, src)
}

func proxyDirect(w http.ResponseWriter, r *http.Request) {
	resp, err := proxyRequest(r)
	if err != nil {
		log.Printf("proxyRequest error: %v", err)
		http.Error(w, "proxy failure", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func proxyRequest(r *http.Request) (*http.Response, error) {
	req := r.Clone(r.Context())
	req.RequestURI = ""

	if req.URL.Scheme == "" {
		req.URL.Scheme = defaultRequestScheme(r)
	}
	if req.URL.Host == "" {
		req.URL.Host = r.Host
	}

	sanitizeHeaders(req.Header)
	return http.DefaultTransport.RoundTrip(req)
}

func findRoute(r *http.Request, cfg *Config) *RouteConfig {
	target := strings.ToLower(r.Host + r.URL.Path)
	for _, route := range cfg.Routes {
		if strings.Contains(target, strings.ToLower(route.Match)) {
			return &route
		}
	}
	return nil
}

func buildCacheKey(r *http.Request, body []byte) (string, error) {
	scheme := r.URL.Scheme
	if scheme == "" {
		scheme = defaultRequestScheme(r)
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

func defaultRequestScheme(r *http.Request) string {
	if r.URL.Scheme != "" {
		return r.URL.Scheme
	}
	if r.TLS != nil {
		return "https"
	}
	return "https"
}

func sanitizeHeaders(headers http.Header) {
	connectionHeader := headers.Get("Connection")
	if connectionHeader != "" {
		for _, field := range strings.Split(connectionHeader, ",") {
			headers.Del(strings.TrimSpace(field))
		}
	}

	for _, name := range hopByHopHeaders {
		headers.Del(name)
	}
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

type cachedResponse struct {
	Status  int
	Headers http.Header
	Body    []byte
}

func writeCachedResponse(w http.ResponseWriter, cached *cachedResponse) {
	copyHeaders(w.Header(), cached.Headers)
	w.WriteHeader(cached.Status)
	if _, err := w.Write(cached.Body); err != nil {
		log.Printf("failed writing cached response body: %v", err)
	}
}

func copyHeaders(dst, src http.Header) {
	skipHeaders := map[string]struct{}{}
	for _, name := range hopByHopHeaders {
		skipHeaders[strings.ToLower(name)] = struct{}{}
	}

	if connectionHeader := src.Get("Connection"); connectionHeader != "" {
		for _, field := range strings.Split(connectionHeader, ",") {
			skipHeaders[strings.ToLower(strings.TrimSpace(field))] = struct{}{}
		}
	}

	for key, values := range src {
		if _, skip := skipHeaders[strings.ToLower(key)]; skip {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}
