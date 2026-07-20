.PHONY: harness-check harness-verify harness-review-gate project-check project-check-test \
	verify verify-project verify-proto generate-proto verify-go verify-helper

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

verify:
	$(MAKE) harness-verify
	$(MAKE) verify-project
	$(MAKE) verify-proto
	$(MAKE) verify-go
	$(MAKE) verify-helper
