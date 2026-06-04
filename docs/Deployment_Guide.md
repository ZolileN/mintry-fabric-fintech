# Mintry Fabric: Deployment Guide

This document outlines how to deploy the Mintry Fabric proxy across various enterprise production environments. While `docker-compose.yml` is fantastic for local and simple orchestrations, staging and production workloads typically utilize Kubernetes (K8s), AWS ECS, or bare-metal Linux.

---

## 1. Kubernetes (K8s) Sidecar Deployment

The ultimate way to deploy Mintry Fabric is as a true **Sidecar Container** within the exact same Kubernetes `Pod` as your backend microservice. Because all containers in a Pod share the same `localhost` network namespace, traffic interception incurs zero network hops.

### Kubernetes Manifest Architecture

**1. Create a Secret for the Encryption Key & Root Key**
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: mintry-fabric-secrets
type: Opaque
stringData:
  MINTRY_SQLCIPHER_KEY: "your_super_secret_aes_key"
  mintry-root.key: |
    -----BEGIN PRIVATE KEY-----
    (your private key here)
    -----END PRIVATE KEY-----
```

**2. Create a ConfigMap for Configuration & Root Cert**
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: mintry-fabric-config
data:
  config.yaml: |
    ca_cert: "/certs/mintry-root.crt"
    ca_key: "/certs/mintry-root.key"
    routes:
      - match: "api.transunion.co.za/v1/score"
        ttl: "30d"
  mintry-root.crt: |
    -----BEGIN CERTIFICATE-----
    (your root cert here)
    -----END CERTIFICATE-----
```

**3. Inject into your Application Deployment**
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-fintech-service
spec:
  replicas: 3
  template:
    spec:
      containers:
        # ────────────────────────────────────────────────────────
        # 1. YOUR MICROSERVICE (THE CLIENT)
        # ────────────────────────────────────────────────────────
        - name: application-container
          image: my-fintech-service:v1
          env:
            # Force traffic to local sidecar
            - name: HTTP_PROXY
              value: "http://localhost:8080"
            - name: HTTPS_PROXY
              value: "http://localhost:8080"
          volumeMounts:
            # Mount the root cert into the Alpine/Ubuntu trust store
            - name: mintry-certs
              mountPath: /usr/local/share/ca-certificates/mintry-root.crt
              subPath: mintry-root.crt

        # ────────────────────────────────────────────────────────
        # 2. MINTRY FABRIC (THE SIDECAR)
        # ────────────────────────────────────────────────────────
        - name: mintry-sidecar
          image: mintry/fabric:latest
          env:
            - name: MINTRY_SQLCIPHER_KEY
              valueFrom:
                secretKeyRef:
                  name: mintry-fabric-secrets
                  key: MINTRY_SQLCIPHER_KEY
          ports:
            - containerPort: 8080 # Proxy
            - containerPort: 8081 # Telemetry
          volumeMounts:
            - name: mintry-certs
              mountPath: /certs
            - name: mintry-config
              mountPath: /app/config.yaml
              subPath: config.yaml
            - name: proxy-cache-volume
              mountPath: /app/cache

      volumes:
        - name: mintry-certs
          projected:
            sources:
            - configMap:
                name: mintry-fabric-config
                items:
                - key: mintry-root.crt
                  path: mintry-root.crt
            - secret:
                name: mintry-fabric-secrets
                items:
                - key: mintry-root.key
                  path: mintry-root.key
        - name: mintry-config
          configMap:
            name: mintry-fabric-config
        - name: proxy-cache-volume
          emptyDir: {} # Temporary disk cache tied to pod lifecycle
```

---

## 2. AWS Elastic Container Service (ECS)

Deploying to AWS ECS (Fargate or EC2) follows a similar multi-container Task Definition pattern.

When creating your Task Definition JSON:
1. Define two containers within the same task: `appContainer` and `mintrySidecarContainer`.
2. Map the `MINTRY_SQLCIPHER_KEY` using AWS Secrets Manager ARNs.
3. Because ECS containers running in the `awsvpc` network mode share the same Elastic Network Interface (ENI), the application container can address the proxy using `localhost:8080`.
4. Inject the `HTTPS_PROXY=http://localhost:8080` variable strictly into the `appContainer` definition.

---

## 3. Bare-Metal Linux (Systemd)

If you are running the runtime directly on an Ubuntu/Debian server without Docker, you must utilize `systemd` to ensure the proxy survives reboots and crashes.

**1. Create a non-root system user:**
```bash
sudo useradd -r -s /bin/false mintry
```

**2. Create the Systemd Unit File (`/etc/systemd/system/mintry-fabric.service`):**
```ini
[Unit]
Description=Mintry Fabric TLS Proxy
After=network.target

[Service]
Type=simple
User=mintry
Group=mintry
WorkingDirectory=/opt/mintry
Environment="MINTRY_SQLCIPHER_KEY=your_secure_aes256_key"
ExecStart=/opt/mintry/mintry-runtime
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

**3. Enable and Start the Proxy:**
```bash
sudo systemctl daemon-reload
sudo systemctl enable mintry-fabric
sudo systemctl start mintry-fabric
sudo systemctl status mintry-fabric
```

*Note: You must still inject the `HTTPS_PROXY` environment variables into whatever application or scripts are running on that server.*
