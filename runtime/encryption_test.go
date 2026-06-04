package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSQLCipherEncryption(t *testing.T) {
	// Create a temp database path
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "encrypted.db")

	const testKey = "my-secret-vault-key-123!"

	// 1. Create cache store with key
	t.Setenv("MINTRY_SQLCIPHER_KEY", testKey)
	store, err := newCacheStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create cache store with key: %v", err)
	}

	// Print cipher version to verify SQLCipher is loaded
	var cipherVer string
	err = store.db.QueryRow("PRAGMA cipher_version").Scan(&cipherVer)
	if err != nil {
		t.Fatalf("failed to query cipher_version: %v", err)
	}
	t.Logf("SQLCipher Version: %q", cipherVer)

	// Put value in cache
	key := "test-cache-key"
	val := &cachedResponse{
		Status:  200,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    []byte(`{"status":"success"}`),
	}
	err = store.put(key, val, 10*time.Minute)
	if err != nil {
		t.Fatalf("failed to put value in cache: %v", err)
	}

	// Get value from cache and verify
	got, found, err := store.get(key)
	if err != nil {
		t.Fatalf("failed to get value from cache: %v", err)
	}
	if !found {
		t.Fatal("expected value to be found in cache")
	}
	if string(got.Body) != `{"status":"success"}` {
		t.Errorf("unexpected body: got %s", string(got.Body))
	}

	// Close database
	err = store.db.Close()
	if err != nil {
		t.Fatalf("failed to close database: %v", err)
	}

	// 2. Try opening the database WITHOUT a key (should fail early in table creation)
	t.Setenv("MINTRY_SQLCIPHER_KEY", "")
	storeNoKey, err := newCacheStore(dbPath)
	if err == nil {
		storeNoKey.db.Close()
		t.Fatal("expected error opening encrypted database without key, but got nil")
	}
	t.Logf("Successfully verified opening without key failed with error: %v", err)

	// 3. Try opening the database with the WRONG key (should fail early in validation)
	t.Setenv("MINTRY_SQLCIPHER_KEY", "wrong-secret-key")
	storeWrongKey, err := newCacheStore(dbPath)
	if err == nil {
		storeWrongKey.db.Close()
		t.Fatal("expected error opening encrypted database with wrong key, but got nil")
	}
	t.Logf("Successfully verified opening with wrong key failed with error: %v", err)

	// 4. Re-open with the CORRECT key and verify it can read again
	t.Setenv("MINTRY_SQLCIPHER_KEY", testKey)
	storeCorrectKey, err := newCacheStore(dbPath)
	if err != nil {
		t.Fatalf("failed to reopen database with correct key: %v", err)
	}
	defer storeCorrectKey.db.Close()

	got2, found2, err := storeCorrectKey.get(key)
	if err != nil {
		t.Fatalf("failed to get value with correct key: %v", err)
	}
	if !found2 {
		t.Fatal("expected value to be found after reopening with correct key")
	}
	if string(got2.Body) != `{"status":"success"}` {
		t.Errorf("unexpected body: got %s", string(got2.Body))
	}
	t.Log("✅ SQLCipher encryption successfully verified!")
}
