# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Mintry Fabric Multi-Stage Dockerfile
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

# Build Stage
FROM golang:1.22-bookworm AS builder

# Install build-time dependencies for CGO & SQLCipher
RUN apt-get update && apt-get install -y \
    libsqlcipher-dev \
    gcc \
    libc6-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Pre-copy dependency files to cache downloads
COPY runtime/go.mod runtime/go.sum ./runtime/

WORKDIR /src/runtime
RUN go mod download

# Copy the remaining runtime source code
COPY runtime/ /src/runtime/

# Setup local library linkages to hook the system SQLCipher library
RUN mkdir -p lib && ln -sf /usr/lib/x86_64-linux-gnu/libsqlcipher.so lib/libsqlite3.so

# Build the production-ready binary with SQLCipher CGO tags
ENV CGO_ENABLED=1
ENV CGO_CFLAGS="-I/usr/include/sqlcipher"
ENV CGO_LDFLAGS="-L/src/runtime/lib -lsqlcipher"

RUN go build -tags "libsqlite3" -o /src/mintry-runtime .

# Production Runner Stage
FROM debian:bookworm-slim

# Install SQLCipher runtime shared libraries and CA certificates
RUN apt-get update && apt-get install -y \
    libsqlcipher-dev \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Create a non-root group and user for security compliance
RUN groupadd -g 10001 mintry && \
    useradd -u 10001 -g mintry -m -d /home/mintry -s /sbin/nologin mintry

WORKDIR /app

# Copy compiled binary from builder
COPY --from=builder /src/mintry-runtime /app/mintry-runtime

# Ensure the app folder is writable by the non-root user (for cache.db file creation)
RUN chown -R mintry:mintry /app

# Switch to the non-root user
USER mintry

# Expose Proxy Interception Port (8080) and Telemetry Metrics Port (8081)
EXPOSE 8080
EXPOSE 8081

# Execute runtime
ENTRYPOINT ["/app/mintry-runtime"]
