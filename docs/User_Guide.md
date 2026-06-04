# Mintry Fabric: User Guide

Welcome to the Mintry Fabric User Guide. This document is written for DevOps engineers, Site Reliability Engineers (SREs), and Infrastructure teams responsible for deploying and managing the Mintry Fabric proxy in a production environment.

## Overview

Mintry Fabric is a transparent, zero-touch Man-in-the-Middle (MITM) caching proxy. By placing it inside the same network namespace as your backend services (the **Sidecar Pattern**), it can automatically intercept outbound HTTP/HTTPS requests to expensive third-party financial APIs (e.g., TransUnion, SmileID) and cache the responses securely.

Because the proxy operates purely at the network layer using standard proxy headers, **you do not need to rewrite or modify any application code to use Mintry Fabric.**

---

## 1. Deploying the Sidecar

Mintry Fabric is compiled as a standalone Go binary, but for modern containerized environments, it is distributed as a lightweight Docker image. 

### Generating the Root Certificate
To intercept TLS traffic without triggering SSL verification errors in your application, you must generate a Mintry Root Certificate Authority (CA) keypair.
Place `mintry-root.crt` and `mintry-root.key` in the root of your deployment directory.

### Orchestrating with Docker Compose
We recommend using Docker Compose or Kubernetes sidecars to ensure the proxy is co-located with your application. Below is the standard Compose blueprint:

```yaml
version: '3.8'

services:
  mintry-sidecar:
    image: mintry/fabric:latest
    container_name: mintry-sidecar
    user: "10001:10001" # Runs as a secure, non-root user
    environment:
      - MINTRY_SQLCIPHER_KEY=your_secure_aes256_key
    ports:
      - "8080:8080" # Interception Proxy Port
      - "8081:8081" # Telemetry & API Port
    volumes:
      - ./mintry-root.crt:/mintry-root.crt:ro
      - ./mintry-root.key:/mintry-root.key:ro
      - ./config.yaml:/app/config.yaml:ro
      - ./cache:/app/cache
    restart: unless-stopped
```

### Trusting the Mintry CA
Your application container must implicitly trust the `mintry-root.crt`. Mount the certificate into your application container's trusted store (e.g., `/usr/local/share/ca-certificates/` on Alpine) and run `update-ca-certificates` as part of your container's entrypoint script.

### Activating the Interception
To force your application to use the sidecar, set standard environment variables in your application container pointing to the sidecar's address:
```bash
HTTP_PROXY=http://mintry-sidecar:8080
HTTPS_PROXY=http://mintry-sidecar:8080
```
*(Note: Use the Docker service name `mintry-sidecar` if within the same Docker bridge network, or `localhost` if deployed as a true Kubernetes pod sidecar).*

---

## 2. Managing Routing Policies

The core intelligence of Mintry Fabric relies on the `config.yaml` file. If a domain is not explicitly listed in this file, the proxy will ignore it and pass the traffic through transparently.

### Defining TTLs (Time-To-Live)
You can define exact cache lifespans based on the endpoint:

```yaml
routes:
  # TransUnion South Africa — credit scores update monthly
  - match: "api.transunion.co.za/v1/score"
    ttl: "30d"
    cache_errors: false

  # BankservAfrica — account verifications can be cached for a week
  - match: "api.bankservafrica.com/v1/verify"
    ttl: "7d"
    cache_errors: false
```
*Never set `cache_errors: true` unless you explicitly want to cache `4xx` or `5xx` responses from the vendor.*

---

## 3. The Command Center Dashboard

Mintry Fabric includes a robust, visually stunning **Neon-Grid Command Center** built in Next.js.

### Starting the Dashboard
Run the dashboard from the `dashboard/` directory:
```bash
npm install
npm run dev
```

### Dashboard Capabilities
1. **Overview:** View your total ZAR capital saved dynamically in real-time. This metric multiplies your cache hits by an estimated vendor API cost.
2. **Routes:** An embedded YAML code editor allowing you to live-edit your proxy policies and instantly sync them to the proxy engine via REST API.
3. **Interception Feed:** A live, WebSockets-powered view of every request your microservices are making. Watch instantly as requests hit the `CACHE_HIT` or `VENDOR_CALL` states.
4. **Settings:** Manage the `MINTRY_SQLCIPHER_KEY` state and toggle the proxy into an absolute pass-through bypass mode during critical system upgrades.
