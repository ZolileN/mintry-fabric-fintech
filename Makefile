# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
# Mintry Fabric Build Automation
# ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

.PHONY: all setup test build run clean

# Compiler Settings
GO := go
CGO_ENABLED := 1
CGO_CFLAGS := -I/usr/include/sqlcipher
CGO_LDFLAGS := -L$(shell pwd)/runtime/lib -lsqlcipher
BUILD_TAGS := libsqlite3

# Targets
all: setup test build

# Setup local library symlinks so go-sqlite3 links with system SQLCipher
setup:
	@echo "🔧 Setting up SQLCipher library linkages..."
	@mkdir -p runtime/lib
	@ln -sf /usr/lib/x86_64-linux-gnu/libsqlcipher.so runtime/lib/libsqlite3.so
	@if [ ! -f mintry-root.key ] || [ ! -f mintry-root.crt ]; then \
		echo "🔑 Generating Mintry Root CA certificates..."; \
		openssl genrsa -out mintry-root.key 4096; \
		openssl req -new -x509 -days 3650 -key mintry-root.key -out mintry-root.crt -subj "/CN=Mintry Fabric CA"; \
	fi

# Run the test suite with SQLCipher enabled
test: setup
	@echo "🧪 Running tests with SQLCipher..."
	@cd runtime && CGO_ENABLED=$(CGO_ENABLED) CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" $(GO) test -tags "$(BUILD_TAGS)" -v -count=1 ./...

# Build the runtime binary
build: setup
	@echo "🔨 Building Mintry Fabric runtime binary..."
	@cd runtime && CGO_ENABLED=$(CGO_ENABLED) CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" $(GO) build -tags "$(BUILD_TAGS)" -o ../mintry-runtime .

# Run the runtime (requires MINTRY_SQLCIPHER_KEY to be set for encryption)
run: setup
	@echo "🚀 Starting Mintry Fabric runtime..."
	@cd runtime && CGO_ENABLED=$(CGO_ENABLED) CGO_CFLAGS="$(CGO_CFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" $(GO) run -tags "$(BUILD_TAGS)" .

# Clean build outputs
clean:
	@echo "🧹 Cleaning build artifacts..."
	@rm -f mintry-runtime
	@rm -rf runtime/lib
	@rm -f runtime/integration_cache.db runtime/err_cache.db
