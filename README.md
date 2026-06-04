# Mintry Fabric: FinOps TLS Sidecar Proxy

Mintry Fabric is a transparent, zero-touch Man-in-the-Middle (MITM) proxy designed exclusively for FinTech microservices. It intercepts, encrypts, and caches expensive third-party financial API calls (e.g., Credit Bureaus, KYC, AML checks) at the network layer, dramatically reducing your operational expenditures without requiring any code changes to your backend applications.

## Key Capabilities

- **Zero-Touch Integration:** Operates entirely at the network layer. Simply set `HTTP_PROXY` and `HTTPS_PROXY` in your application container, and the Sidecar handles the rest.
- **SQLCipher Encrypted Storage:** Sensitive intercepted PII and financial payloads are cached in a military-grade AES-256 encrypted SQLite write-ahead log database.
- **Dynamic Routing Policies:** Define TTL (Time-To-Live) cache invalidation rules per vendor endpoint using hot-reloadable YAML configurations.
- **Failsafe Resilience:** Built with an aggressive Circuit Breaker architecture. If the encrypted disk locks or fills up, the proxy degrades gracefully to a transparent pass-through mode—your app never goes offline.
- **Telemetry Dashboard:** A stunning Next.js command center tracking ZAR capital saved, real-time cache hits via WebSockets, and system memory allocations.

---

## 1. Production Architecture (Sidecar Pattern)

Mintry Fabric is designed to be deployed as a **Sidecar Container** running in the exact same network space (or Kubernetes Pod) as your client microservice. This eliminates network latency and centralizes certificate trust securely.

### Prerequisites
- Docker & Docker Compose
- Node.js 18+ (for running the Dashboard locally)

---

## 2. Deployment Instructions

### Step 1: Secure the Configuration
Ensure you have generated the custom Root CA certificates for the MITM interception:
- `mintry-root.crt`
- `mintry-root.key`

Place these in the root of the repository alongside `docker-compose.yml`.

### Step 2: Set the Encryption Key
The Fabric requires a symmetric key to unlock the SQLCipher database. Export this variable in your environment or CI/CD pipeline:
```bash
export MINTRY_SQLCIPHER_KEY="your_super_secret_aes_key"
```

### Step 3: Launch the Sidecar
Use the provided `docker-compose.yml` to orchestrate the multi-stage proxy build alongside your application container.

```bash
docker-compose up --build -d
```

### Step 4: Automate Certificate Injection
Your client microservice must trust the Mintry Root CA to prevent TLS handshake errors. If you are using Alpine Linux inside your application container, run the following on startup:

```bash
apk add --no-cache ca-certificates
# (Assuming the cert is mounted to /usr/local/share/ca-certificates/mintry-root.crt)
update-ca-certificates
```

Inject the proxy routing environment variables into your application runtime:
```env
HTTP_PROXY=http://mintry-sidecar:8080
HTTPS_PROXY=http://mintry-sidecar:8080
```

All outbound traffic will now intelligently route through the Mintry Fabric!

---

## 3. Dashboard Command Center

The telemetry dashboard is a Next.js application that provides real-time observability into the proxy's operations, powered by the Mintry Neon-Grid aesthetic.

1. Navigate to the `dashboard/` directory.
2. Install dependencies:
   ```bash
   npm install
   ```
3. Start the dashboard in development or production mode:
   ```bash
   npm run dev
   ```
4. Access the command center at `http://localhost:3000`.

The dashboard communicates with the Fabric runtime over WebSockets (`ws://localhost:8081/ws/feed`) and REST APIs (`http://localhost:8081/api/routes`) to stream live interception telemetry and manage YAML routing policies dynamically.

---

## 4. Testing & Benchmarking

The core runtime includes an aggressive Go Race Detector stress-testing suite to guarantee memory constraints and Write-Ahead Log (WAL) safety under load.

To verify thread-safety and SQLite locking resilience on your local machine:
```bash
cd runtime
CGO_ENABLED=1 go test -v -run=TestConcurrency -race
```
*(This simulates 15,000+ parallel goroutines hammering the circuit breakers and ring buffers.)*

---

**Mintry Fabric:** Close the attribution void and reclaim your API budget.
