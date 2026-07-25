# Go Package Boundaries Phase 1 Design

## Status

Awaiting written-spec review before implementation.

## Context

The Go codebase has several directories whose file counts make review and
navigation harder. Static analysis of `main` at
`3547b2041e0d838e7e0f41fe4c08cd341e874ca6` found:

- `internal/store`: 86 production files and 54 test files in one package.
- `internal/app`: 19 production files and 14 test files.
- `internal/codex/logs`: 11 production files and 11 test files.
- `internal/scheduler`: 10 production files and 8 test files.
- `internal/codex/quota`: 9 production files and 6 test files.

`internal/store` needs a dedicated persistence-boundary redesign because its
single `Repository` coordinates cross-domain transactions, schema migrations,
and 126 exported repository methods. This phase deliberately avoids mixing
that high-risk work with two lower-risk package extractions.

## Goal

Create two cohesive Go subpackages without changing runtime behavior, persisted
data, RPC contracts, filesystem safety, migration semantics, or parser output:

1. Move Codex rollout parsing into `internal/codex/logs/rollout`.
2. Move the migration-recovery state machine and platform database swap into
   `internal/app/migrationrecovery`.

The branch for this work is `refactor/go-package-boundaries`.

## Non-goals

- Do not split `internal/store` in this phase.
- Do not change SQLite schema or migration checksums.
- Do not change parser validation, diagnostics, limits, checkpoint format, or
  event ordering.
- Do not change migration-recovery phases, confirmation semantics, audit
  content, backup validation, or atomic-swap behavior.
- Do not add compatibility facades solely to preserve old internal import
  paths.
- Do not redesign `internal/scheduler` or `internal/codex/quota`.

## Considered approaches

### A. Extract two behavior-preserving subpackages

Move the two already cohesive subsystems, update internal consumers, and use
their existing test suites as characterization tests.

Advantages:

- Small, reviewable dependency changes.
- No database or RPC contract change.
- Establishes a repeatable package-extraction pattern before touching Store.

Disadvantages:

- Does not yet address the largest directory, `internal/store`.

### B. Split Store domains in the same branch

Extract migrations, analytics, quota, scheduling, runtime, and ingest together.

Advantages:

- Largest immediate reduction in root-package size.

Disadvantages:

- `WriteUnit`, `models.go`, and `migration.go` currently create cross-domain
  dependency cycles.
- Would require a new shared DB-model layer and broad internal API changes.
- Failure isolation and reviewability would be poor.

### C. Only regroup or rename files inside existing packages

Advantages:

- Lowest compile-time risk.

Disadvantages:

- Does not improve import boundaries, ownership, or package-level API size.
- Directory navigation remains largely unchanged.

### Decision

Use approach A. Treat Store decomposition as a separate design and branch after
this phase has passed repository verification.

## Package design

### `internal/codex/logs/rollout`

The new `rollout` package owns the complete content-parsing pipeline:

- line framing;
- duplicate-key-safe JSON decoding;
- rollout record decoding;
- turn lifecycle normalization;
- parser seed validation and checkpoint continuation;
- parser facts, diagnostics, counters, and parse results.

Move these production files:

- `internal/codex/logs/decoder.go`
- `internal/codex/logs/framer.go`
- `internal/codex/logs/lifecycle.go`
- `internal/codex/logs/parser.go`
- `internal/codex/logs/parser_types.go`

Move the corresponding tests:

- `decoder_test.go`
- `framer_test.go`
- `lifecycle_test.go`
- `parser_test.go`
- `parser_fuzz_test.go`
- `quota_test.go`

The parent `internal/codex/logs` package continues to own Home probing, file
discovery, snapshot reading, source fingerprinting, and reconciliation.

`logs.SourceKind` and its three existing constants remain in the parent package
because both discovery and rollout parsing consume the same source
classification. `rollout.ParserConfig` and `rollout.SessionMetaFact` refer to
`logs.SourceKind`; the parent package must not import `rollout`, preserving an
acyclic dependency:

```text
internal consumers -> logs
internal consumers -> logs/rollout
logs/rollout        -> logs
logs                -X-> logs/rollout
```

Consumers that parse content, principally `internal/indexer`, import
`logs/rollout` directly. Discovery and lifecycle consumers continue importing
`internal/codex/logs`.

No aliases remain in `logs` for the moved parser API. All repository references
and documentation are updated to the new package path.

### `internal/app/migrationrecovery`

The new `migrationrecovery` package owns:

- migration-recovery controller state;
- backup enumeration and validation;
- retry, prepare, confirm, cancel, and exit transitions;
- content-free audit persistence;
- ready-database verification;
- platform-specific atomic SQLite database exchange.

Move these production files:

- `internal/app/migration_recovery.go`
- `internal/app/migration_swap_darwin.go`
- `internal/app/migration_swap_linux.go`
- `internal/app/migration_swap_other.go`

Move `internal/app/migration_recovery_test.go` with the controller.

Keep `internal/app/migration_recovery_service.go` and
`migration_recovery_service_test.go` in package `app`. That file is the
application-layer adapter from the recovery controller to
`core.MigrationRecoveryService` and intentionally continues to use
`publicRuntimeCommandFailure`.

The child package exposes only the API needed by the application adapter and
startup composition:

- `type Controller`
- `func NewController(storesqlite.Config, *store.MigrationFailure) (*Controller, error)`
- `func RunStartupGate(context.Context, storesqlite.Config) (*store.MigrationFailure, error)`
- `func FailureFrom(error) *store.MigrationFailure`
- the existing snapshot, receipt, confirmation, phase, and error contracts;
- controller methods already required by the service adapter.

`internal/app` imports the child package. The child package never imports
`internal/app`.

## Error and compatibility contract

This refactor preserves all existing sentinel errors and `errors.Is` behavior.
Moved errors retain their existing text. The application adapter continues to
map controller failures through `publicRuntimeCommandFailure`.

The parser's error values move to `rollout`; internal consumers update their
imports and comparisons. No public cross-process contract contains these Go
package paths.

No persisted identifiers, JSON field names, audit records, parser versions, or
RPC messages change.

## Test strategy

This is a structural refactor, so existing behavior tests are the primary
contract.

For each extraction:

1. Move the subsystem tests and change their package declaration/imports first.
2. Run the focused test package and observe the expected compile failure
   because the new package implementation does not yet exist.
3. Move the minimum production files and update package-qualified references.
4. Run focused tests until green.
5. Run affected dependent-package tests.

Focused verification:

```bash
CGO_ENABLED=0 go test ./internal/codex/logs ./internal/codex/logs/rollout ./internal/indexer -count=1
CGO_ENABLED=0 go test ./internal/app ./internal/app/migrationrecovery ./internal/core ./internal/helper -count=1
```

Refactor verification:

```bash
CGO_ENABLED=0 go test ./... -count=1
go vet ./...
make verify-architecture
git diff --check
```

The final repository gate remains `make verify`; it must not use a real Codex
Home and must not run `make verify-live`.

## Delivery boundaries

- Implement and verify only the two extractions described above.
- Commit each extraction separately so either can be reviewed or reverted
  independently.
- Do not begin Store extraction in this branch.
- After this branch is stable, write a separate Store design centered on a
  transaction-safe model/migration boundary.
