package cache

import (
	"database/sql"
	"sync"
	"sync/atomic"
	"time"
)

// ProxyStats represents the current operational metrics of the proxy
type ProxyStats struct {
	TotalCapitalSavedZAR float64                `json:"total_capital_saved_zar"`
	SavingsGrowthText    string                 `json:"savings_growth_text"`
	CacheHitRate         float64                `json:"cache_hit_rate"`
	ProxyStatus          string                 `json:"proxy_status"`
	MemoryUsageMB        float64                `json:"memory_usage_mb"`
	ActiveConnections    int                    `json:"active_connections"`
	RecentInterceptions  []InterceptionLogEntry `json:"recent_interceptions"`
}

// InterceptionLogEntry represents a single proxy request interception
type InterceptionLogEntry struct {
	Timestamp      time.Time `json:"timestamp"`
	TargetEndpoint string    `json:"target_endpoint"`
	Action         string    `json:"action"` // "CACHE_HIT" or "VENDOR_CALL"
	LatencyMS      float64   `json:"latency_ms"`
	TTLRemaining   string    `json:"ttl_remaining"`
}

// Telemetry tracks proxy operational metrics
type Telemetry struct {
	mu                  sync.RWMutex
	db                  *sql.DB
	cacheHits           int64
	cacheMisses         int64
	totalLatencyMS      float64
	recentInterceptions []InterceptionLogEntry
	OnInterception      func(entry InterceptionLogEntry)
}

// NewTelemetry creates a new telemetry tracker
func NewTelemetry() *Telemetry {
	return &Telemetry{
		recentInterceptions: make([]InterceptionLogEntry, 0, 50),
	}
}

// SetDB configures the database connection for persistent telemetry tracking
// and initializes the telemetry_log schema if it doesn't exist.
func (t *Telemetry) SetDB(db *sql.DB) error {
	t.mu.Lock()
	t.db = db
	t.mu.Unlock()

	if db == nil {
		return nil // telemetry runs in-memory cleanly
	}

	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS telemetry_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp INTEGER NOT NULL,
			target_endpoint TEXT NOT NULL,
			action TEXT NOT NULL,
			latency_ms REAL NOT NULL,
			ttl_remaining TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_telemetry_timestamp ON telemetry_log(timestamp);
	`)
	return err
}

// RecordCacheHit records a cache hit event
func (t *Telemetry) RecordCacheHit(endpoint string, latencyMS float64, ttlRemaining string) {
	entry := InterceptionLogEntry{
		Timestamp:      time.Now(),
		TargetEndpoint: endpoint,
		Action:         "CACHE_HIT",
		LatencyMS:      latencyMS,
		TTLRemaining:   ttlRemaining,
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.db != nil {
		_, err := t.db.Exec(`
			INSERT INTO telemetry_log (timestamp, target_endpoint, action, latency_ms, ttl_remaining)
			VALUES (?, ?, 'CACHE_HIT', ?, ?)
		`, entry.Timestamp.UnixNano(), endpoint, latencyMS, ttlRemaining)
		if err == nil {
			if t.OnInterception != nil {
				t.OnInterception(entry)
			}
			return
		}
	}

	atomic.AddInt64(&t.cacheHits, 1)
	t.addInterception(entry)
	if t.OnInterception != nil {
		t.OnInterception(entry)
	}
}

// RecordVendorCall records a vendor call (cache miss) event
func (t *Telemetry) RecordVendorCall(endpoint string, latencyMS float64) {
	entry := InterceptionLogEntry{
		Timestamp:      time.Now(),
		TargetEndpoint: endpoint,
		Action:         "VENDOR_CALL",
		LatencyMS:      latencyMS,
		TTLRemaining:   "-",
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.db != nil {
		_, err := t.db.Exec(`
			INSERT INTO telemetry_log (timestamp, target_endpoint, action, latency_ms, ttl_remaining)
			VALUES (?, ?, 'VENDOR_CALL', ?, '-')
		`, entry.Timestamp.UnixNano(), endpoint, latencyMS)
		if err == nil {
			if t.OnInterception != nil {
				t.OnInterception(entry)
			}
			return
		}
	}

	atomic.AddInt64(&t.cacheMisses, 1)
	t.totalLatencyMS += latencyMS
	t.addInterception(entry)
	if t.OnInterception != nil {
		t.OnInterception(entry)
	}
}

func (t *Telemetry) addInterception(entry InterceptionLogEntry) {
	t.recentInterceptions = append([]InterceptionLogEntry{entry}, t.recentInterceptions...)
	if len(t.recentInterceptions) > 50 {
		t.recentInterceptions = t.recentInterceptions[:50]
	}
}

// GetStats returns the current operational statistics
func (t *Telemetry) GetStats(memoryUsageMB float64, activeConnections int) ProxyStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.db != nil {
		var cacheHits int64
		var cacheMisses int64

		// Query cache hits
		errHits := t.db.QueryRow("SELECT COUNT(*) FROM telemetry_log WHERE action = 'CACHE_HIT'").Scan(&cacheHits)
		errMisses := t.db.QueryRow("SELECT COUNT(*) FROM telemetry_log WHERE action = 'VENDOR_CALL'").Scan(&cacheMisses)
		
		if errHits == nil && errMisses == nil {
			// Query recent 50 interceptions
			rows, errQuery := t.db.Query(`
				SELECT timestamp, target_endpoint, action, latency_ms, ttl_remaining
				FROM telemetry_log
				ORDER BY timestamp DESC
				LIMIT 50
			`)
			if errQuery == nil {
				defer rows.Close()
				recentInterceptions := make([]InterceptionLogEntry, 0, 50)
				for rows.Next() {
					var tsNano int64
					var entry InterceptionLogEntry
					if err := rows.Scan(&tsNano, &entry.TargetEndpoint, &entry.Action, &entry.LatencyMS, &entry.TTLRemaining); err == nil {
						entry.Timestamp = time.Unix(0, tsNano)
						recentInterceptions = append(recentInterceptions, entry)
					}
				}

				totalRequests := cacheHits + cacheMisses
				hitRate := 0.0
				if totalRequests > 0 {
					hitRate = float64(cacheHits) / float64(totalRequests) * 100
				}

				costPerCallZAR := 46.50
				totalSavedZAR := float64(cacheHits) * costPerCallZAR

				return ProxyStats{
					TotalCapitalSavedZAR: totalSavedZAR,
					SavingsGrowthText:    "+12% this month",
					CacheHitRate:         hitRate,
					ProxyStatus:          "Online",
					MemoryUsageMB:        memoryUsageMB,
					ActiveConnections:    activeConnections,
					RecentInterceptions:  recentInterceptions,
				}
			}
		}
	}

	// Fallback to in-memory stats if DB is nil or querying fails
	cacheHits := atomic.LoadInt64(&t.cacheHits)
	cacheMisses := atomic.LoadInt64(&t.cacheMisses)
	totalRequests := cacheHits + cacheMisses

	hitRate := 0.0
	if totalRequests > 0 {
		hitRate = float64(cacheHits) / float64(totalRequests) * 100
	}

	// Calculate estimated savings (ZAR per vendor call)
	costPerCallZAR := 46.50
	totalSavedZAR := float64(cacheHits) * costPerCallZAR

	recentInterceptionsCopy := make([]InterceptionLogEntry, len(t.recentInterceptions))
	copy(recentInterceptionsCopy, t.recentInterceptions)

	return ProxyStats{
		TotalCapitalSavedZAR: totalSavedZAR,
		SavingsGrowthText:    "+12% this month",
		CacheHitRate:         hitRate,
		ProxyStatus:          "Online",
		MemoryUsageMB:        memoryUsageMB,
		ActiveConnections:    activeConnections,
		RecentInterceptions:  recentInterceptionsCopy,
	}
}
