# Mintry Fabric: Developer Guide

Welcome to the Mintry Fabric Developer Guide. This document is intended for Software Engineers extending the core Go runtime, integrating new SQLite pragmas, or adding features to the Next.js control plane.

## Repository Structure

- `runtime/`: Contains the core Go application, including the TLS MITM interceptor, caching engine, and telemetry database.
- `dashboard/`: A Next.js 15 application running Tailwind CSS, serving as the Command Center UI.
- `docs/`: Technical guides and whitepapers.
- `Dockerfile`: Multi-stage build definitions.
- `docker-compose.yml`: Standard sidecar orchestration blueprint.

---

## 1. Core Go Runtime Architecture

The Mintry Fabric proxy is built upon `elazarl/goproxy`, offering highly extensible hooks into the HTTP request/response lifecycle.

### The Interception Flow (`main.go`)
When a request enters the proxy:
1. **Target Evaluation:** `config.yaml` is queried to see if the target URL matches an explicitly defined cache route. If no match is found, the proxy connects transparently to the remote host.
2. **Cache Retrieval:** If matched, the proxy queries the SQLite database via `CacheStore.get(key)`. If valid cache data exists (and TTL hasn't expired), the proxy intercepts the request natively and returns the cached body/headers, completely bypassing the network out.
3. **Upstream Request:** If cache is missed, the proxy streams the request to the vendor API, intercepts the response via `OnResponse`, and commits the payload to the database using `CacheStore.put()`.

### Building with CGO and SQLCipher
Because we utilize SQLCipher to encrypt the SQLite Write-Ahead Log, the runtime requires CGO to be enabled during compilation to link against native C libraries.

#### Ubuntu/Debian Prerequisites:
```bash
sudo apt-get install -y gcc libc6-dev libsqlcipher-dev
```

#### Local Compilation:
```bash
cd runtime
CGO_ENABLED=1 CGO_CFLAGS="-I/usr/include/sqlcipher" go build -tags "libsqlite3" -o mintry-runtime .
```

### Extending Telemetry (`internal/cache/ledger.go`)
Telemetry is designed as an ultra-fast, memory-safe ring buffer (`recentInterceptions`). If you wish to extend the metrics captured (e.g., adding Prometheus tracking or Datadog statsd traces), hook into `Telemetry.RecordCacheHit` and `Telemetry.RecordVendorCall`. Be exceptionally mindful of holding the `sync.RWMutex` locks during blocking I/O calls.

---

## 2. Dashboard Engineering (Next.js)

The frontend is built using Next.js App Router (`app/`), leveraging deep Glassmorphism CSS and the official Mintry "Neon Grid" aesthetic.

### WebSocket Integration
The `Feed` page utilizes native WebSockets to stream telemetry from the Go runtime. 
To modify the feed payload, you must edit the payload emitter in `runtime/main.go` inside the `/ws/feed` handler, and then update the `InterceptionLogEntry` interface inside `dashboard/app/feed/page.tsx`.

### Theming and Aesthetics
The core theme is controlled entirely via `dashboard/app/globals.css`. Do not apply rigid background colors (like `bg-slate-900`) to components in the dashboard. The application relies on a master `body::before` CSS pseudo-element to render the global grid and noise overlay. Components must remain transparent (`bg-black/20`, `backdrop-blur-md`) to ensure the grid propagates correctly.

---

## 3. Running the Stress Test Suite

Before committing any structural changes to the Go `RWMutex` locking mechanisms or SQLite bindings, you **must** pass the high-concurrency stress tests.

```bash
cd runtime
CGO_ENABLED=1 go test -v -run=TestConcurrency -race
```
The race detector (`-race`) will aggressively profile thousands of goroutines. If a lock drops early or a map is accessed concurrently, the test will fatally fail. Never bypass these tests before submitting a Pull Request.
