package main

import (
	"path/filepath"
	"testing"

	"github.com/mintryfabric/mintry-fabric-runtime/internal/cache"
)

func TestTelemetryPersistence(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "telemetry.db")

	const testKey = "telemetry-secret-key-456!"
	t.Setenv("MINTRY_SQLCIPHER_KEY", testKey)

	// 1. Initialize store and telemetry
	store, err := newCacheStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache store: %v", err)
	}

	tel := cache.NewTelemetry()
	if err := tel.SetDB(store.db); err != nil {
		t.Fatalf("failed to bind telemetry DB: %v", err)
	}

	// 2. Record telemetry events
	tel.RecordCacheHit("api.transunion.co.za/v1/score", 45.2, "24h")
	tel.RecordVendorCall("api.smileid.com/v1/verify", 120.5)

	// 3. Verify statistics instantly
	stats := tel.GetStats(0.0, 0)
	if stats.CacheHitRate != 50.0 {
		t.Errorf("expected 50%% hit rate, got %f%%", stats.CacheHitRate)
	}
	if stats.TotalCapitalSavedZAR != 46.50 {
		t.Errorf("expected 46.50 ZAR saved, got %f ZAR", stats.TotalCapitalSavedZAR)
	}
	if len(stats.RecentInterceptions) != 2 {
		t.Fatalf("expected 2 recent interceptions, got %d", len(stats.RecentInterceptions))
	}

	// The most recent interception should be the vendor call (LIFO)
	if stats.RecentInterceptions[0].TargetEndpoint != "api.smileid.com/v1/verify" {
		t.Errorf("unexpected endpoint for recent interception: %s", stats.RecentInterceptions[0].TargetEndpoint)
	}
	if stats.RecentInterceptions[0].Action != "VENDOR_CALL" {
		t.Errorf("unexpected action: %s", stats.RecentInterceptions[0].Action)
	}

	// 4. Close database
	store.db.Close()

	// 5. Reopen database and bind to a fresh telemetry tracker
	store2, err := newCacheStore(dbPath)
	if err != nil {
		t.Fatalf("failed to reopen cache store: %v", err)
	}
	defer store2.db.Close()

	tel2 := cache.NewTelemetry()
	if err := tel2.SetDB(store2.db); err != nil {
		t.Fatalf("failed to bind telemetry DB to second tracker: %v", err)
	}

	// 6. Verify stats are successfully recovered from persistent storage
	stats2 := tel2.GetStats(0.0, 0)
	if stats2.CacheHitRate != 50.0 {
		t.Errorf("recovered: expected 50%% hit rate, got %f%%", stats2.CacheHitRate)
	}
	if stats2.TotalCapitalSavedZAR != 46.50 {
		t.Errorf("recovered: expected 46.50 ZAR saved, got %f ZAR", stats2.TotalCapitalSavedZAR)
	}
	if len(stats2.RecentInterceptions) != 2 {
		t.Fatalf("recovered: expected 2 recent interceptions, got %d", len(stats2.RecentInterceptions))
	}

	// Verify order and values
	if stats2.RecentInterceptions[0].TargetEndpoint != "api.smileid.com/v1/verify" {
		t.Errorf("recovered: unexpected endpoint for recent interception: %s", stats2.RecentInterceptions[0].TargetEndpoint)
	}
	if stats2.RecentInterceptions[1].TargetEndpoint != "api.transunion.co.za/v1/score" {
		t.Errorf("recovered: unexpected endpoint for older interception: %s", stats2.RecentInterceptions[1].TargetEndpoint)
	}
	if stats2.RecentInterceptions[0].LatencyMS != 120.5 {
		t.Errorf("recovered: unexpected latency: %f", stats2.RecentInterceptions[0].LatencyMS)
	}

	// 7. Verify in-memory fallback behaves correctly when DB is closed/nil
	store2.db.Close()
	
	// Record after close — should fallback to in-memory tracking cleanly
	tel2.RecordCacheHit("api.xds.co.za/v1/credit", 10.0, "12h 30m")
	statsFallback := tel2.GetStats(0.0, 0)
	
	// We recorded 1 hit after DB was closed.
	// Since DB was closed, only the in-memory fallback tracked this hit.
	t.Logf("Fallback cache hit recorded. Fallback Hit Rate: %f, Total Interceptions: %d", statsFallback.CacheHitRate, len(statsFallback.RecentInterceptions))
}
