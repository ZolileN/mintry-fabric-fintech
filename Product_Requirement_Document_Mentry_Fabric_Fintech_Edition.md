# Product Requirement Document (PRD)

**Project Title:** Mintry Fabric (Fintech Edition)

**Document Version:** 1.0.0

**Author:** Zolile Nonzapa

**Status:** Draft

---

## 1. Executive Summary & Objective

### 1.1 Objective
Mintry Fabric (Fintech Edition) is an invisible, transport-layer FinOps and security proxy designed to run as a sidecar alongside existing microservices. Its core mission is to eliminate duplicate, high-cost third-party verification API calls (for example, credit bureaus, identity verification providers, AML/KYC registries) and protect fragile internal legacy systems without forcing engineering teams to alter a single line of application code.

### 1.2 The Problem
Fintechs, lenders, and property platforms waste significant capital executing redundant outbound data fetches. Because internal engineering teams are siloed (for example, Onboarding, Credit Underwriting, Fraud Management), different services independently query identical identities (for example, matching a South African identity number) within short time frames.

Building a centralized internal caching system requires extensive database architectural overhaul, robust data protection implementation (POPIA/GDPR compliance), and disruptive legacy codebase rewrites.

### 1.3 The Solution
Mintry Fabric drops into the containerized or server network environment as a transparent forward proxy. It intercepts outbound traffic to specified fintech endpoints, normalizes payload criteria dynamically, executes high-speed lookup against an encrypted local SQLite Write-Ahead Logging (WAL) ledger, and intercepts/short-circuits duplicate requests by serving locally cached hits within user-defined Time-To-Live (TTL) frameworks.

---

## 2. Target Audience & Personas

### 2.1 The Fintech CFO / Head of Finance

- Pain Point: Ballooning monthly verification bills from data suppliers such as TransUnion, Experian, TPN, Home Affairs, Smile ID.
- Desire: Immediate, measurable cost mitigation without interrupting day-to-day operations or slowing product features.

### 2.2 The Engineering CTO / Tech Lead

- Pain Point: Massive tech debt, fragile legacy code, and complex integration projects.
- Desire: A developer-friendly, zero-touch operational architecture that drops in smoothly, introduces sub-millisecond network overhead, and handles data security effortlessly.

---

## 3. Scope & Key Functional Requirements

The end-to-end request flow is:

[ Outbound Client Request ] ──► [ Mintry Interceptor ] ──► [ Token/Body Normalization ]
                                      │
                         ┌────────────┴────────────┐
                         ▼                         ▼
                  [ WAL Cache Hit ]         [ WAL Cache Miss ]
                         │                         │
            (Decrypt via SQLCipher)        (Forward to Vendor)
                         │                         │
                         ▼                         ▼
              [ Fast HTTP 200 Return ]    [ Encrypt Payload & Commit ]

### 3.1 Transparent Network Interception (The Kernel)

- Requirement: Act as a reverse/forward proxy intercepting targeted HTTPS traffic using `HTTP_PROXY`/`HTTPS_PROXY` environmental routing or container-level service routing.
- TLS Handling: Terminate outbound TLS matching targeted vendor domain patterns using local trusted root certificates, inspect payloads, and securely re-encrypt traffic boundaries.

### 3.2 Deterministic Payload Hashing Engine

- Requirement: Prevent cache misses caused by arbitrary JSON payload sorting or shifting operational parameters.
- Mechanism:
  - Parse the outgoing JSON request body.
  - Strip out dynamic meta-keys (for example, `timestamp`, `requestId`, `clientNonce`).
  - Sort all remaining request keys alphabetically.
  - Generate a deterministic SHA-256 string matching the sorted text combined with the path endpoint to serve as the immutable primary cache key.

### 3.3 Ultra-Low Latency State Engine (SQLite WAL)

- Requirement: Read lookups must execute within sub-millisecond speeds to prevent adding latency onto critical transaction flows.
- Mechanism: Utilize an embedded SQLite engine running in Write-Ahead Logging (WAL) mode with indexed cache keys to ensure lightning-fast reads without network round-trips.

### 3.4 Regulatory Security Mesh (POPIA / GDPR Readiness)

- Requirement: Zero unencrypted storage of Personally Identifiable Information (PII) such as national ID numbers, banking details, or credit grades.
- Mechanism: Integrate full database encryption at rest using SQLCipher (AES-256-GCM). Data structures must securely isolate local data scopes from any external cloud syncing components.

### 3.5 Rule Engine & Lifecycle Manager

- Requirement: Granular control over data staleness policies.
- Configuration Syntax (YAML/JSON):

```yaml
routes:
  - match: "api.transunion.co.za/v1/score"
    ttl: "30d"
    cache_errors: false
  - match: "api.smileid.com/v1/async/verify"
    ttl: "7d"
    cache_errors: false
```

- Error Exclusions: Explicitly prohibit the caching of non-200 HTTP response payloads (for example, `401 Unauthorized`, `500 Server Error`, `429 Rate Limited`) to guarantee subsequent retries reach live vendors.

---

## 4. Technical Architecture & Stack Specification

- **Control Plane Dashboard:** Built using React 19, Next.js App Router, TypeScript (Strict Mode), and styled via Tailwind CSS. This layer governs policy creation, usage monitoring, and metrics visualizations.
- **Proxy Runtime Layer:** A concurrent, highly performant proxy runtime optimized for raw connection handling.
- **Storage Architecture:** Embedded SQLite compiled with SQLCipher for seamless hardware-accelerated local encryption.
- **Communication Plane:** Asynchronous, non-blocking telemetry threads logging transaction metadata (omitting raw PII) back to the control plane dashboard to avoid impacting runtime network pathways.

---

## 5. Non-Functional Requirements & Security Guardrails

### 5.1 Performance Overhead

- The proxy layer must limit network processing overhead to under 1.5 ms on a standard multi-core VPS runtime.

### 5.2 Failsafe Open Strategy

- If the proxy runs out of allocated storage space, hits database locking anomalies, or throws an unhandled exception, it must instantly transition into **Pass-Through Mode**.
- It will bypass all internal caching processes and cleanly proxy the raw, unchanged live traffic out to the vendor to guarantee zero service disruption.

### 5.3 Storage Pruning

- An asynchronous background worker loop must clean out expired rows from the SQLite database dynamically when rows pass their specified TTL limit, maintaining an optimized local memory boundary.

---

## 6. Release & Phased Milestones

- **Phase 1 (MVP Local Core):** Develop the internal standalone network interception architecture, the dynamic JSON alphanumeric key-sorting mechanism, and base-level SQLite writing capability.
- **Phase 2 (Security Enforcement):** Embed deep SQLCipher integration and compile strict regulatory validation configurations.
- **Phase 3 (Dashboard Connection):** Launch the Next.js control panel interface to visualize real-time cost-saving indices and modify endpoint lifecycle properties remotely.

---

## 7. Success Evaluation Criteria

**Deduplication Efficiency** = (Intercepted Cache Hits / Total Verification Outbound Requests) × 100

- Primary Metric: Reduce a pilot client's monthly verification expenditure by at least 20% within the initial 30 days of production operation.
- Technical Metric: Achieve a 100% success rate during engineered Failsafe Open simulation scenarios.

---

## Appendix

- Branding guidance: Use colors and logo direction from `https://mintry-page.vercel.app/`.
- Keep the product messaging aligned with invisible, non-intrusive deployment, and compliance-ready fintech operations.
