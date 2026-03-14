BINARY = tssh
VERSION = 1.0.0

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
	$$(go env GOPATH)/bin/go1.23.6 build -ldflags="-s -w" -o $(BINARY) .

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
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $$(go env GOPATH)/bin/go1.23.6 build -ldflags="-s -w" -o $$output . || exit 1; \
	done
	@echo "✅ All binaries in dist/"
	@ls -la dist/

clean:
	rm -rf $(BINARY) dist/

# Deploy to all machines
deploy: all
	@echo "Deploying to NUC..."
	scp dist/$(BINARY)-darwin-amd64 nuc:/usr/local/bin/$(BINARY)
	@echo "Deploying to rasp3..."
	scp dist/$(BINARY)-linux-arm64 rasp3:/usr/local/bin/$(BINARY)
	@echo "Deploying to rk3568..."
	scp -P 22 dist/$(BINARY)-linux-arm64 williamwong@192.168.40.13:~/bin/$(BINARY)
	@echo "Installing locally..."
	cp dist/$(BINARY)-darwin-arm64 /usr/local/bin/$(BINARY) 2>/dev/null || sudo cp dist/$(BINARY)-darwin-arm64 /usr/local/bin/$(BINARY)
	@echo "✅ Deployed to all machines"
