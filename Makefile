.PHONY: harness-check harness-verify harness-review-gate \
	project-check project-check-test project-generated-check-test verify verify-project verify-go \
	verify-frontend verify-package verify-generated

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

project-generated-check-test:
	bash scripts/project-checks/check_generated_test.sh

verify:
	$(MAKE) harness-verify
	$(MAKE) verify-project
	$(MAKE) verify-go
	$(MAKE) verify-frontend
	$(MAKE) verify-generated
	$(MAKE) verify-package

verify-project:
	$(MAKE) project-check
	$(MAKE) project-check-test
	$(MAKE) project-generated-check-test

verify-go:
	go test ./...
	go vet ./...

verify-frontend:
	npm --prefix frontend run typecheck
	npm --prefix frontend test
	npm --prefix frontend run build

verify-package:
	wails3 package GOOS=darwin
	wails3 task package:verify

verify-generated:
	bash scripts/project-checks/check_binding_generation_failure.sh
	bash scripts/project-checks/check_generated.sh
