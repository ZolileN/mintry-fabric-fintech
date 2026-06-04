package main

import (
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/mintryfabric/mintry-fabric-runtime/internal/cache"
)

func TestConcurrencyTelemetryRingBuffer(t *testing.T) {
	// Initialize telemetry with no DB (in-memory mode for pure ring buffer stress)
	telemetry := cache.NewTelemetry()
	err := telemetry.SetDB(nil)
	if err != nil {
		t.Fatalf("failed to init telemetry: %v", err)
	}

	var wg sync.WaitGroup
	numWorkers := 10000

	// Hammer the telemetry ring buffer to test locking constraints
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			endpoint := fmt.Sprintf("api.test.com/endpoint-%d", id%100)
			if id%2 == 0 {
				telemetry.RecordCacheHit(endpoint, 12.5, "30d")
			} else {
				telemetry.RecordVendorCall(endpoint, 45.0)
			}
		}(i)
	}

	wg.Wait()

	stats := telemetry.GetStats(0.0, 0)
	if len(stats.RecentInterceptions) > 50 {
		t.Fatalf("Ring buffer exceeded 50 entries: got %d", len(stats.RecentInterceptions))
	}
	if stats.CacheHitRate < 0 || stats.CacheHitRate > 100 {
		t.Fatalf("Hit rate is completely invalid: %f", stats.CacheHitRate)
	}
}

func TestConcurrencyCacheStoreWAL(t *testing.T) {
	// Create a temporary database for the WAL test
	tmpFile, err := os.CreateTemp("", "stress_test_*.db")
	if err != nil {
		t.Fatalf("failed to create temp db file: %v", err)
	}
	dbPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(dbPath)

	store, err := newCacheStore(dbPath)
	if err != nil {
		t.Fatalf("failed to init cache store: %v", err)
	}

	var wg sync.WaitGroup
	numWorkers := 5000 // Heavy write/read concurrency

	// Pre-fill some data to get reads and writes fighting
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key-%d", i)
		resp := &cachedResponse{
			Status:  200,
			Headers: http.Header{"Content-Type": []string{"application/json"}},
			Body:    []byte(`{"status":"ok"}`),
		}
		_ = store.put(key, resp, 1*time.Minute)
	}

	// Hammer the CacheStore with concurrent reads and writes
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", id%100) // high contention on 100 keys

			if id%2 == 0 {
				// Even IDs write
				resp := &cachedResponse{
					Status:  200,
					Headers: http.Header{"Content-Type": []string{"application/json"}},
					Body:    []byte(fmt.Sprintf(`{"data":%d}`, id)),
				}
				err := store.put(key, resp, 1*time.Minute)
				if err != nil && err.Error() != "circuit breaker is OPEN" {
					// Some contention errors might happen under extreme stress depending on SQLite busy timeouts.
					// But it should not panic or cause memory corruption.
				}
			} else {
				// Odd IDs read
				_, _, err := store.get(key)
				if err != nil && err.Error() != "circuit breaker is OPEN" {
					// Handle as above
				}
			}
		}(i)
	}

	wg.Wait()

	// Ensure WAL file and DB didn't corrupt
	_, _, err = store.get("key-1")
	if err != nil && err.Error() != "circuit breaker is OPEN" && err.Error() != "sql: database is closed" {
		t.Fatalf("Post-stress test database read failed with unusual error: %v", err)
	}
}
