# Mintry Fabric (Fintech Edition)

A two-layer FinOps proxy platform designed to sit transparently next to containerized microservices.

## Workspace layout

- `runtime/` — transparent MITM proxy runtime with TLS interception, deterministic request hashing, and SQLite WAL cache.
- `dashboard/` — control plane dashboard scaffold built with Next.js and Tailwind CSS.
- `mintry-root.crt` — Root CA certificate for TLS MITM interception (distributable to client services).
- `mintry-root.key` — Root CA private key (**NEVER commit to git**).
- `Product_Requirement_Document_Mentry_Fabric_Fintech_Edition.md` — product requirements and architecture.

## Getting started

### 1. Generate the Root CA (one-time setup)

```bash
openssl genrsa -out mintry-root.key 4096
openssl req -new -x509 -days 3650 -key mintry-root.key -out mintry-root.crt -subj "/CN=Mintry Fabric CA"
```

### 2. Start the Runtime

```bash
cd runtime
go mod tidy
go run .
```

- Configure `config.yaml` with endpoint routing rules, TTL policies, and CA certificate paths.
- Set `MINTRY_SQLCIPHER_KEY` before launch to enable SQLCipher database encryption at rest.

### 3. Configure Client Microservices

For MITM interception to work, client services must:

1. **Trust the Mintry Root CA** — add `mintry-root.crt` to their certificate store.
2. **Route traffic through the proxy** — set `HTTP_PROXY=http://localhost:8080` and `HTTPS_PROXY=http://localhost:8080`.

```bash
# Example: trust the CA system-wide on Ubuntu/Debian
sudo cp mintry-root.crt /usr/local/share/ca-certificates/mintry-root.crt
sudo update-ca-certificates

# Example: set proxy for a single service
export HTTP_PROXY=http://localhost:8080
export HTTPS_PROXY=http://localhost:8080
```

### 4. Start the Dashboard

```bash
cd dashboard
npm install
npm run dev
```

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    Client Microservice                          │
│  (Onboarding, Credit, Fraud — zero code changes required)      │
│  Trusts mintry-root.crt · Routes via HTTPS_PROXY               │
└──────────────────────────┬──────────────────────────────────────┘
                           │ CONNECT api.transunion.co.za:443
                           ▼
┌──────────────────────────────────────────────────────────────────┐
│                    Mintry Fabric Runtime (:8080)                 │
│                                                                  │
│  1. TLS MITM — signs dynamic cert using Mintry Root CA           │
│  2. Payload Extraction — reads decrypted JSON body               │
│  3. Deterministic Hashing — strips timestamps, sorts keys,      │
│     generates SHA-256 cache key                                  │
│  4. SQLite WAL Lookup:                                           │
│     ├── HIT  → return cached response (sub-ms)                  │
│     └── MISS → forward to vendor, cache response on return       │
│                                                                  │
│  Non-vendor traffic passes through completely untouched.         │
└──────────────────────────────────────────────────────────────────┘
```

## Design goals

- Zero-touch deployment as a transparent MITM proxy sidecar.
- Cache duplicate outbound HTTPS verification requests.
- Maintain compliance with POPIA/GDPR using encrypted local storage.
- Provide an operational dashboard for policy configuration and telemetry.
