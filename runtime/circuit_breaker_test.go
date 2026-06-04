package main

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestCircuitBreakerTrippingAndRecovery(t *testing.T) {
	// Create a temp database path
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "circuit.db")

	store, err := newCacheStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache store: %v", err)
	}
	defer store.db.Close()

	// Configure a test breaker with short timeouts and cooldowns for rapid testing
	// Threshold: 3, Cooldown: 100ms, Timeout: 20ms
	testBreaker := newCircuitBreaker(3, 100*time.Millisecond, 20*time.Millisecond)
	store.breaker = testBreaker

	// 1. Initial state must be StateClosed (Allow should return true)
	if !store.breaker.Allow() {
		t.Fatal("expected breaker to allow operations initially")
	}

	// 2. Perform a successful lookup (expecting sql.ErrNoRows which is recorded as success)
	_, found, err := store.get("non-existent-key")
	if err != nil {
		t.Fatalf("get failed unexpectedly: %v", err)
	}
	if found {
		t.Fatal("expected no rows to be found")
	}
	if store.breaker.consecutiveFails != 0 {
		t.Errorf("expected consecutive fails to be 0, got %d", store.breaker.consecutiveFails)
	}

	// 3. Inject consecutive query failures
	// We close the underlying DB to force query failures!
	store.db.Close()

	// Fail 1
	_, _, err = store.get("test-key")
	if err == nil || err.Error() == "circuit breaker is OPEN" {
		t.Fatalf("expected db query error, got: %v", err)
	}

	// Fail 2
	_, _, err = store.get("test-key")
	if err == nil || err.Error() == "circuit breaker is OPEN" {
		t.Fatalf("expected db query error, got: %v", err)
	}

	// Fail 3 — this should trip the breaker to OPEN
	_, _, err = store.get("test-key")
	if err == nil || err.Error() == "circuit breaker is OPEN" {
		t.Fatalf("expected db query error, got: %v", err)
	}

	// 4. Verify the breaker is now OPEN and rejects lookups immediately
	if store.breaker.Allow() {
		t.Fatal("expected breaker to reject operations (state OPEN)")
	}

	_, _, err = store.get("test-key")
	if err == nil || err.Error() != "circuit breaker is OPEN" {
		t.Fatalf("expected circuit breaker is OPEN error, got: %v", err)
	}

	t.Logf("Successfully verified breaker tripped and blocked call: %v", err)

	// 5. Re-open a fresh DB connection to allow recovery
	db2, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to reopen database: %v", err)
	}
	store.db = db2

	// 6. Wait for cooldown to elapse
	time.Sleep(120 * time.Millisecond)

	// Breaker should transition to HALF-OPEN on Allow()
	if !store.breaker.Allow() {
		t.Fatal("expected breaker to allow test operation in HALF-OPEN state")
	}

	// 7. Perform a successful lookup to reset the breaker to CLOSED
	_, found, err = store.get("test-key")
	if err != nil {
		t.Fatalf("expected lookup to succeed and reset breaker, got error: %v", err)
	}
	if found {
		t.Fatal("expected key to not be found")
	}

	// Breaker state must reset to StateClosed
	if store.breaker.state != StateClosed {
		t.Errorf("expected state to reset to CLOSED, got %v", store.breaker.state)
	}
	if store.breaker.consecutiveFails != 0 {
		t.Errorf("expected consecutive fails to reset to 0, got %d", store.breaker.consecutiveFails)
	}

	t.Log("✅ Circuit breaker tripping, failsafe blocking, and auto-recovery verified!")
}

func TestCircuitBreakerTimeout(t *testing.T) {
	// Verify that slow queries/operations trigger the timeout and record failure.
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "timeout.db")

	store, err := newCacheStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache store: %v", err)
	}
	defer store.db.Close()

	// Threshold: 1, Cooldown: 1s, Timeout: 1ms (extremely small to force context deadline exceeded)
	testBreaker := newCircuitBreaker(1, 1*time.Second, 1*time.Microsecond)
	store.breaker = testBreaker

	// Attempt put operation — should fail due to context timeout
	resp := &cachedResponse{Status: 200, Headers: nil, Body: []byte("xyz")}
	err = store.put("key", resp, 1*time.Minute)
	if err == nil {
		t.Fatal("expected operation to time out, but succeeded")
	}

	// Check if breaker is now tripped to OPEN
	if store.breaker.state != StateOpen {
		t.Errorf("expected breaker to trip to OPEN after timeout, got state: %v", store.breaker.state)
	}
	t.Logf("Successfully verified query timeout triggered breaker: %v", err)
}
