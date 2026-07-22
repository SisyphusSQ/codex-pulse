.PHONY: harness-check harness-verify harness-review-gate project-check project-check-test \
	verify verify-project verify-proto generate-proto verify-go verify-helper \
	verify-swift-proto verify-swift-client verify-swift-transport \
	verify-swift-app verify-swift-app-smoke-isolated verify-swift-app-live \
	verify-swift-app-smoke verify-swift-primary-pages

export RUN_ID

harness-check:
	bash scripts/harness/check.sh

harness-verify: harness-check

harness-review-gate:
	@if [ -z "$(PLAN)" ]; then echo "usage: make harness-review-gate PLAN=path/to/plan.md" >&2; exit 2; fi
	bash scripts/harness/review_gate.sh --plan "$(PLAN)"

project-check:
	bash scripts/project-checks/check.sh

project-check-test:
	bash scripts/project-checks/check_test.sh

verify-project:
	$(MAKE) project-check
	$(MAKE) project-check-test

verify-proto:
	bash scripts/proto/generate.sh --check

generate-proto:
	bash scripts/proto/generate.sh --write

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

verify-swift-app-smoke-isolated: verify-helper verify-swift-app
	bash scripts/macos/run-app-smoke.sh

verify-swift-app-live: verify-helper verify-swift-app
	bash scripts/macos/run-app-live-smoke.sh

verify-swift-app-smoke: verify-swift-app-live

verify-swift-primary-pages: verify-swift-app-live

verify:
	$(MAKE) harness-verify
	$(MAKE) verify-project
	$(MAKE) verify-proto
	$(MAKE) verify-go
	$(MAKE) verify-swift-transport
	$(MAKE) verify-swift-app-smoke-isolated
