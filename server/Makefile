BINARY_NAME ?= codex-pulse-server
VARS_PKG ?= github.com/SisyphusSQ/codex-pulse/server/vars
BUILD_DIR ?= bin
GO ?= go
CGO_ENABLED ?= 0
PLATFORMS := windows-amd64 windows-arm64 darwin-amd64 darwin-arm64 linux-amd64 linux-arm64
VERSION ?= dev-$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)$(shell test -z "$$(git status --porcelain 2>/dev/null)" || echo -dirty)

BUILD_FLAGS  = -X '$(VARS_PKG).AppName=$(BINARY_NAME)'
BUILD_FLAGS += -X '$(VARS_PKG).AppVersion=$(VERSION)'
BUILD_FLAGS += -X '$(VARS_PKG).GoVersion=$(shell $(GO) env GOVERSION)'
BUILD_FLAGS += -X '$(VARS_PKG).BuildTime=$(shell date +"%Y-%m-%d %H:%M:%S")'
BUILD_FLAGS += -X '$(VARS_PKG).GitCommit=$(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)'

LDFLAGS = -ldflags="$(BUILD_FLAGS)"

.PHONY: help all build release run test vet lint fmt tidy clean

help:
	@echo "  build-all / release-all - 构建 Windows、macOS、Linux 的 amd64 和 arm64"
	@echo "  build-<os>-<arch> / release-<os>-<arch> - 单个平台，例如 build-windows-arm64"
	@echo "  跨平台输出：$(BUILD_DIR)/<os>-<arch>/$(BINARY_NAME)[.exe]；不执行测试或发布"
	@echo "Available targets:"
	@echo "  all      - Run fmt, test, and build"
	@echo "  build    - Build local binary"
	@echo "  release  - Build release binary"
	@echo "  run      - Run app"
	@echo "  test     - Run tests"
	@echo "  vet      - Run go vet"
	@echo "  lint     - Run golangci-lint"
	@echo "  fmt      - Run go fmt"
	@echo "  tidy     - Run go mod tidy"
	@echo "  clean    - Remove build artifacts"

all: fmt test build

build: web-build
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./app/main.go

release: web-build
	@mkdir -p $(BUILD_DIR)
	$(GO) build -trimpath $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./app/main.go

run: web-build
	$(GO) run ./app/main.go http

test: web-build
	$(GO) test ./...

vet: web-build
	$(GO) vet ./...

lint: web-build
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run

fmt: web-build
	$(GO) fmt ./...

tidy: web-build
	$(GO) mod tidy

clean:
	rm -rf $(BUILD_DIR)

.PHONY: verify integration vuln
verify: web-build
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))" || (echo 'Go files required formatting'; exit 1)
	$(GO) test -race ./...
	$(GO) vet ./...
	$(GO) build ./...

integration: web-build
	$(GO) test -tags integration ./app/cmd -run TestIsolatedComponents -count=1 -timeout=2m

vuln: web-build
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...

# 保留本机构建入口；六平台目标各自输出到独立目录，允许 make -j 并行。
.PHONY: build-all release-all $(addprefix build-,$(PLATFORMS)) $(addprefix release-,$(PLATFORMS))
build-all: $(addprefix build-,$(PLATFORMS))
release-all: $(addprefix release-,$(PLATFORMS))

# Windows 使用 .exe；默认禁用 CGO，因此交叉构建不需要额外 C 工具链。
define build_platform
	@target="$*"; target_os="$${target%-*}"; target_arch="$${target#*-}"; \
	ext=""; if [ "$$target_os" = windows ]; then ext=".exe"; fi; \
	output="$(BUILD_DIR)/$$target/$(BINARY_NAME)$$ext"; \
	mkdir -p "$(BUILD_DIR)/$$target" && \
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$$target_os GOARCH=$$target_arch $(GO) build $(1) $(LDFLAGS) -o "$$output" ./app/main.go
endef

$(addprefix build-,$(PLATFORMS)): build-%: web-build
	$(call build_platform,)

$(addprefix release-,$(PLATFORMS)): release-%: web-build
	$(call build_platform,-trimpath)

# Web 先构建并内嵌到 Go 二进制；中心打包不运行测试、数据库初始化或发布。
NPM ?= npm
CONFIG ?= $(CURDIR)/config/config.yml
PACKAGE_DIR ?= $(BUILD_DIR)/center
.PHONY: web-install web-build build-center run-center package-center
web-install:
	cd web && $(NPM) ci
web-build:
	cd web && $(NPM) run build
build-center: build
run-center:
	$(BUILD_DIR)/$(BINARY_NAME) --config "$(CONFIG)" http
package-center: build-center
	mkdir -p $(BUILD_DIR)
	mkdir "$(PACKAGE_DIR)"
	cp $(BUILD_DIR)/$(BINARY_NAME) $(PACKAGE_DIR)/$(BINARY_NAME)
	mkdir -p $(PACKAGE_DIR)/deploy $(PACKAGE_DIR)/scripts $(PACKAGE_DIR)/config
	cp -R deploy/. $(PACKAGE_DIR)/deploy/
	cp config/development.example.yml config/production.example.yml $(PACKAGE_DIR)/config/
	cp deploy/README.md $(PACKAGE_DIR)/README.md
	cp scripts/mysql-backup.sh $(PACKAGE_DIR)/scripts/mysql-backup.sh
	cp scripts/deadline.py scripts/macos-service.py $(PACKAGE_DIR)/scripts/
	cp docs/test/operations.md $(PACKAGE_DIR)/OPERATIONS.md
