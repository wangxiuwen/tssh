BINARY = tssh
VERSION = 1.0.0
GO_CMD ?= $(shell command -v go1.23.6 2>/dev/null || echo "go")

# Platforms matching the 4 machines
PLATFORMS = \
	darwin/amd64 \
	darwin/arm64 \
	linux/amd64 \
	linux/arm64 \
	windows/amd64 \
	windows/arm64

.PHONY: all clean build install

build:
	$(GO_CMD) build -ldflags="-s -w" -o $(BINARY) ./cmd/tssh/

install: build
	sudo cp $(BINARY) /usr/local/bin/

all:
	@mkdir -p dist
	@for platform in $(PLATFORMS); do \
		os=$$(echo $$platform | cut -d/ -f1); \
		arch=$$(echo $$platform | cut -d/ -f2); \
		output=dist/$(BINARY)-$$os-$$arch; \
		if [ "$$os" = "windows" ]; then \
			output="$$output.exe"; \
		fi; \
		echo "Building $$output..."; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO_CMD) build -ldflags="-s -w" -o $$output ./cmd/tssh/ || exit 1; \
	done
	@echo "✅ All binaries in dist/"
	@ls -la dist/

clean:
	rm -rf $(BINARY) dist/


