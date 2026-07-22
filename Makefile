.PHONY: check test-go test-swift verify verify-architecture verify-proto generate-proto \
	verify-go verify-helper verify-swift-proto verify-swift-client \
	verify-swift-transport verify-swift-app verify-swift-app-smoke-isolated \
	verify-swift-app-live verify-swift-app-smoke verify-swift-primary-pages \
	verify-live

export RUN_ID

verify-architecture:
	bash scripts/project-checks/check.sh

verify-proto:
	bash scripts/proto/generate.sh --check

generate-proto:
	bash scripts/proto/generate.sh --write

test-go:
	go test ./...

verify-go:
	go test -race ./...
	go vet ./...

verify-helper:
	mkdir -p bin
	go build -trimpath -ldflags '-X main.applicationVersion=dev' -o bin/codex-pulse .

verify-swift-proto:
	bash scripts/proto/generate-swift.sh --check

verify-swift-client:
	mkdir -p bin
	go build -trimpath -o bin/codex-pulse-cancel-probe ./scripts/swift-cancel-probe
	CODEX_PULSE_CANCEL_PROBE="$(CURDIR)/bin/codex-pulse-cancel-probe" \
		swift run --package-path app/macos codex-pulse-core-client-tests

verify-swift-transport: verify-helper verify-swift-proto verify-swift-client
	swift run --package-path app/macos codex-pulse-transport-spike --helper "$(CURDIR)/bin/codex-pulse"

verify-swift-app:
	swift run --package-path app/macos codex-pulse-app-tests
	swift build --package-path app/macos --product codex-pulse-app

test-swift: verify-swift-client verify-swift-app

check: verify-architecture verify-proto verify-swift-proto test-go test-swift

verify-swift-app-smoke-isolated: verify-helper verify-swift-app
	bash scripts/macos/run-app-smoke.sh

verify-swift-app-live: verify-helper verify-swift-app
	bash scripts/macos/run-app-live-smoke.sh

verify-swift-app-smoke: verify-swift-app-live

verify-swift-primary-pages: verify-swift-app-live

verify-live: verify-swift-app-live

verify: verify-architecture verify-proto verify-go verify-swift-transport verify-swift-app-smoke-isolated
