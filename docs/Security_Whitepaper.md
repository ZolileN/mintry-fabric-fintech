# Mintry Fabric: Security Whitepaper

This whitepaper details the strict compliance, cryptographic integrity, and high-availability failsafe engineering built into the Mintry Fabric proxy. It is intended for Security Auditors, DevSecOps engineers, and Compliance Officers evaluating the proxy's deployment inside a secure financial network.

## 1. Cryptographic Storage Integrity

Because Mintry Fabric intercepts HTTP payloads directed at Identity Verification (KYC), Credit Bureaus, and Open Banking platforms, the proxy inherently caches Personally Identifiable Information (PII) and highly sensitive financial payloads. 

Storing this data safely on disk is the highest priority of the architecture.

### SQLCipher AES-256-GCM Integration
We completely eliminated standard SQLite bindings and explicitly compile the proxy using **SQLCipher**. 
- The proxy initializes the `cache.db` Write-Ahead Log (WAL) using a symmetric 256-bit AES encryption key passed securely via the `MINTRY_SQLCIPHER_KEY` environment variable.
- SQLCipher encrypts every single page of the database before it is flushed to disk. It is mathematically impossible to read, grep, or extract JSON payloads from the `cache.db` file without possessing the symmetric master key.
- **Zero-Knowledge Architecture:** If the container host is breached and the Docker volumes are exported by an attacker, the SQLite cache payload remains entirely cryptographically opaque.

---

## 2. Dynamic Circuit Breakers & Fail-Open Resiliency

Security is not just encryption; it is also availability. A proxy sits directly in the critical execution path of a microservice. If the proxy fails, the microservice fails.

### The Failsafe Pass-Through Mechanism
Mintry Fabric implements a strictly enforced **Fail-Open** policy. The proxy utilizes a sliding-window circuit breaker around the SQLite database engine.

If the proxy detects high I/O contention (e.g., `database is locked` under immense load) or if the `MINTRY_SQLCIPHER_KEY` is missing/corrupted, the circuit breaker immediately trips to an `OPEN` state.

When the breaker is `OPEN`:
1. The proxy instantly ceases all read/write attempts to the encrypted disk.
2. It transitions immediately to a transparent **Pass-Through State**.
3. All network traffic is sent directly to the vendor upstream, completely bypassing the local cache.

**Impact:** Your microservice experiences a cache miss, but it completes its task. It does not crash, timeout, or panic. High-availability is fundamentally preserved.

---

## 3. Trust Boundary & PKI Management

To perform a Man-in-the-Middle (MITM) cache on TLS (HTTPS) traffic without triggering security alerts, the client must implicitly trust the proxy's cryptographic authority.

### Automated Root CA Generation
The proxy utilizes a self-signed, isolated Root Certificate Authority (`mintry-root.crt`).
- The proxy dynamically signs forged leaf certificates on the fly for requested domains (e.g., `api.transunion.co.za`).
- The `mintry-root.key` is kept entirely local to the proxy's volume mount. It is never transmitted across the network, avoiding distributed PKI vulnerabilities.

### Sidecar Network Isolation
Because Mintry Fabric is designed using the **Sidecar Pattern**, it is deployed within the exact same Kubernetes Pod or Docker Bridge Network as the client application.
- The trust boundary is isolated to `localhost` or the immediate container bridge.
- The `mintry-root.crt` is only injected into the targeted client microservice. The broader host operating system is never compromised with a forged root CA.
- Unencrypted payloads (`HTTP`) existing between the application and the proxy are never exposed to the wider internal VPC, as they never leave the localized sidecar network namespace.
