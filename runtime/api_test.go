package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mintryfabric/mintry-fabric-runtime/internal/cache"
)

func TestRoutesAPI(t *testing.T) {
	// Back up config.yaml to guarantee test isolation
	var origConfig []byte
	var configExists bool
	if _, err := os.Stat("config.yaml"); err == nil {
		origConfig, _ = os.ReadFile("config.yaml")
		configExists = true
	}
	defer func() {
		if configExists {
			_ = os.WriteFile("config.yaml", origConfig, 0644)
		} else {
			_ = os.Remove("config.yaml")
		}
	}()

	// Create mock config
	cfg := &Config{
		CACert: "../mintry-root.crt",
		CAKey:  "../mintry-root.key",
		Routes: []RouteConfig{
			{Match: "api.transunion.co.za/v1/score", TTL: "30d"},
		},
	}

	// Make sure a temp config.yaml exists for GET handler
	_ = os.WriteFile("config.yaml", []byte("routes:\n  - match: \"api.transunion.co.za/v1/score\"\n    ttl: \"30d\""), 0644)

	handler := handleRoutesAPI(cfg)

	// Test GET
	req := httptest.NewRequest("GET", "/api/routes", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected GET status 200, got %d", w.Code)
	}

	var getResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("failed to decode GET body: %v", err)
	}
	if !strings.Contains(getResp["config_yaml"], "api.transunion.co.za/v1/score") {
		t.Fatalf("expected YAML to contain transunion match rule, got: %s", getResp["config_yaml"])
	}

	// Test POST invalid YAML
	badBody := `{"config_yaml": "routes:\n  - match: api.smileid.com\n  ttl: 7d\n  cache_errors: invalid_bool_here"}`
	req = httptest.NewRequest("POST", "/api/routes", bytes.NewBufferString(badBody))
	w = httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected POST status 400 for bad YAML, got %d", w.Code)
	}

	// Test POST valid YAML
	goodYAML := `
ca_cert: "../mintry-root.crt"
ca_key: "../mintry-root.key"
routes:
  - match: "api.stitch.money/v1/accounts"
    ttl: "1d"
`
	goodBody, _ := json.Marshal(map[string]string{"config_yaml": goodYAML})
	req = httptest.NewRequest("POST", "/api/routes", bytes.NewReader(goodBody))
	w = httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected POST status 200, got %d", w.Code)
	}

	// Verify reload changed routes in memory
	configMu.RLock()
	routesCount := len(cfg.Routes)
	matchedRoute := cfg.Routes[0].Match
	configMu.RUnlock()

	if routesCount != 1 || matchedRoute != "api.stitch.money/v1/accounts" {
		t.Errorf("expected hot-reloaded route match to be 'api.stitch.money/v1/accounts', got: %s", matchedRoute)
	}
}

func TestSettingsAndPassThroughAPI(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := dbDir + "/test_settings.db"
	store, err := newCacheStore(dbPath)
	if err != nil {
		t.Fatalf("failed to initialize cache store: %v", err)
	}
	defer store.db.Close()

	// Register global telemetry
	telemetry = cache.NewTelemetry()
	if err := telemetry.SetDB(store.db); err != nil {
		t.Fatalf("failed to bind telemetry database: %v", err)
	}

	settingsHandler := handleSettingsAPI(store)

	// Test GET
	req := httptest.NewRequest("GET", "/api/settings", nil)
	w := httptest.NewRecorder()
	settingsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected GET settings status 200, got %d", w.Code)
	}

	var settings map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &settings); err != nil {
		t.Fatalf("failed to unmarshal settings response: %v", err)
	}
	if settings["circuit_breaker_state"] != "CLOSED" {
		t.Errorf("expected circuit_breaker_state to be CLOSED, got: %v", settings["circuit_breaker_state"])
	}
	if settings["pass_through_forced"] != false {
		t.Errorf("expected pass_through_forced to be false, got: %v", settings["pass_through_forced"])
	}

	// Test POST toggle pass-through to true
	toggleBody, _ := json.Marshal(map[string]bool{"forced": true})
	req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(toggleBody))
	w = httptest.NewRecorder()
	settingsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected POST settings status 200, got %d", w.Code)
	}

	// Verify breaker is forced to block database calls
	if store.breaker.Allow() {
		t.Error("expected breaker.Allow() to return false when pass-through is forced")
	}

	// Test POST toggle pass-through back to false
	toggleBody, _ = json.Marshal(map[string]bool{"forced": false})
	req = httptest.NewRequest("POST", "/api/settings", bytes.NewReader(toggleBody))
	w = httptest.NewRecorder()
	settingsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected POST settings status 200, got %d", w.Code)
	}

	if !store.breaker.Allow() {
		t.Error("expected breaker.Allow() to return true when pass-through is released")
	}
}

func TestMetricsHistoryAPI(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := dbDir + "/test_history.db"
	store, err := newCacheStore(dbPath)
	if err != nil {
		t.Fatalf("failed to initialize cache store: %v", err)
	}
	defer store.db.Close()

	telemetry = cache.NewTelemetry()
	if err := telemetry.SetDB(store.db); err != nil {
		t.Fatalf("failed to bind telemetry database: %v", err)
	}

	// Record a hit to verify group counts
	telemetry.RecordCacheHit("api.smileid.com/v1/async/verify", 1.5, "6d 23h")

	historyHandler := handleMetricsHistoryAPI(store)

	req := httptest.NewRequest("GET", "/api/metrics/history", nil)
	w := httptest.NewRecorder()
	historyHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected GET metrics history status 200, got %d", w.Code)
	}

	var points []ChartDataPoint
	if err := json.Unmarshal(w.Body.Bytes(), &points); err != nil {
		t.Fatalf("failed to unmarshal metrics history: %v", err)
	}

	if len(points) == 0 {
		t.Fatal("expected points to be returned, got empty list")
	}

	// The last point or the current date point should have 1 total and 1 cached request
	today := time.Now().Format("2006-01-02")
	foundToday := false
	for _, p := range points {
		if p.Date == today {
			foundToday = true
			if p.TotalRequests != 1 || p.CachedRequests != 1 {
				t.Errorf("expected 1 total and 1 cached request for today, got: total=%d, cached=%d", p.TotalRequests, p.CachedRequests)
			}
		}
	}
	if !foundToday {
		t.Errorf("expected to find data point for today (%s) in history response", today)
	}
}

func TestDatabaseFallback(t *testing.T) {
	// Initialize a fallback store where the DB is nil (offline)
	store := &CacheStore{
		db:      nil,
		breaker: newCircuitBreaker(3, 5*time.Second, 150*time.Millisecond),
	}
	store.breaker.manualPassThrough = true

	// 1. Verify Cache get/put returns clean errors instead of panicking
	_, found, err := store.get("some-key")
	if found || err == nil {
		t.Errorf("expected cache get on nil database to fail, got found=%v, err=%v", found, err)
	}

	err = store.put("some-key", &cachedResponse{Status: 200}, 5*time.Second)
	if err == nil {
		t.Errorf("expected cache put on nil database to fail, got err=nil")
	}

	// 2. Verify handleSettingsAPI returns valid settings JSON on nil database
	settingsHandler := handleSettingsAPI(store)
	reqSettings := httptest.NewRequest("GET", "/api/settings", nil)
	wSettings := httptest.NewRecorder()
	settingsHandler(wSettings, reqSettings)

	if wSettings.Code != http.StatusOK {
		t.Errorf("expected settings status 200, got %d", wSettings.Code)
	}

	var settings map[string]any
	if err := json.Unmarshal(wSettings.Body.Bytes(), &settings); err != nil {
		t.Fatalf("failed to unmarshal settings: %v", err)
	}
	if settings["db_path"] != "cache.db" {
		t.Errorf("expected db_path to be cache.db, got %v", settings["db_path"])
	}
	if settings["pass_through_forced"] != true {
		t.Errorf("expected pass_through_forced to be true, got %v", settings["pass_through_forced"])
	}

	// 3. Verify handleMetricsHistoryAPI returns empty array on nil database
	historyHandler := handleMetricsHistoryAPI(store)
	reqHistory := httptest.NewRequest("GET", "/api/metrics/history", nil)
	wHistory := httptest.NewRecorder()
	historyHandler(wHistory, reqHistory)

	if wHistory.Code != http.StatusOK {
		t.Errorf("expected metrics history status 200, got %d", wHistory.Code)
	}

	var points []ChartDataPoint
	if err := json.Unmarshal(wHistory.Body.Bytes(), &points); err != nil {
		t.Fatalf("failed to unmarshal metrics history: %v", err)
	}
	if len(points) != 0 {
		t.Errorf("expected 0 points on nil database fallback, got %d", len(points))
	}
}
