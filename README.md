# Mintry Fabric (Fintech Edition)

A two-layer FinOps proxy platform designed to sit transparently next to containerized microservices.

## Workspace layout

- `runtime/` — transparent proxy runtime with deterministic request hashing and SQLite cache throttling.
- `dashboard/` — control plane dashboard scaffold built with Next.js and Tailwind CSS.
- `Product_Requirement_Document_Mentry_Fabric_Fintech_Edition.md` — product requirements and architecture.

## Getting started

### Runtime

```bash
cd runtime
go mod tidy
go run .
```

- Configure `config.yaml` with endpoint routing and TTL rules.
- Set `MINTRY_SQLCIPHER_KEY` before launch to enable SQLCipher database encryption at rest.

### Dashboard

```bash
cd dashboard
npm install
npm run dev
```

---

## Design goals

- Zero-touch deployment as a transparent proxy sidecar.
- Cache duplicate outbound verification requests.
- Maintain compliance with POPIA/GDPR using encrypted local storage.
- Provide an operational dashboard for policy configuration and telemetry.
