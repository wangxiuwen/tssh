GO_CMD ?= $(shell command -v go1.23.6 2>/dev/null || echo "go")

# Every cmd/<BIN>/ dir is built as its own binary. Main `tssh` is the
# big-tent binary; tssh-k8s (and future tssh-net / tssh-db / tssh-arms)
# are slim slices linked from the same internal/ packages.
BINARIES = tssh tssh-k8s

# Platforms matching the 4 machines + Windows for parity.
PLATFORMS = \
	darwin/amd64 \
	darwin/arm64 \
	linux/amd64 \
	linux/arm64 \
	windows/amd64 \
	windows/arm64

.PHONY: all clean build install test vet $(BINARIES)

# `make build` builds all binaries for the host OS+arch into the repo root.
build: $(BINARIES)

$(BINARIES):
	$(GO_CMD) build -ldflags="-s -w" -o $@ ./cmd/$@/

test:
	$(GO_CMD) test ./internal/... ./cmd/... -count=1

vet:
	$(GO_CMD) vet ./...

install: build
	sudo cp $(BINARIES) /usr/local/bin/

# `make all` cross-compiles every binary × every platform into dist/.
all:
	@mkdir -p dist
	@for bin in $(BINARIES); do \
		for platform in $(PLATFORMS); do \
			os=$$(echo $$platform | cut -d/ -f1); \
			arch=$$(echo $$platform | cut -d/ -f2); \
			output=dist/$$bin-$$os-$$arch; \
			if [ "$$os" = "windows" ]; then \
				output="$$output.exe"; \
			fi; \
			echo "Building $$output..."; \
			CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO_CMD) build -ldflags="-s -w" -o $$output ./cmd/$$bin/ || exit 1; \
		done; \
	done
	@echo "✅ All binaries in dist/"
	@ls -la dist/

clean:
	rm -rf $(BINARIES) dist/
