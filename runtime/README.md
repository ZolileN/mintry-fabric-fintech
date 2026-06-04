# Runtime

The Mintry Fabric runtime is a transparent forward proxy designed to intercept outbound fintech verification requests, normalize payloads, and reduce duplicate vendor calls using a local SQLite cache.

## Features

- Transparent proxy with HTTP/HTTPS CONNECT handling
- Deterministic JSON hashing for cache key generation
- SQLite WAL store with configurable TTL rules
- Route-level caching policy with error caching controls
- Pass-through fallback for unconfigured endpoints

## Run locally

```bash
cd runtime
go mod tidy
go run .
```

## Configuration

Edit `config.yaml` to define endpoint matchers, TTL, and caching behavior.

- `match`: substring matcher for vendor host+path
- `ttl`: time-to-live, either duration (`5m`, `24h`) or days (`30d`)
- `cache_errors`: whether to cache non-200 responses

## Encryption

Set the `MINTRY_SQLCIPHER_KEY` environment variable to activate SQLCipher encryption if the runtime is built with SQLCipher support.
