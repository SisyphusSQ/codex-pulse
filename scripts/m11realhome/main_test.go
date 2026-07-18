package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/bootstrap"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
	"github.com/SisyphusSQ/codex-pulse/internal/query/usagecost"
)

type failingPrivacyRows struct {
	err    error
	closed bool
}

func (*failingPrivacyRows) Next() bool        { return false }
func (*failingPrivacyRows) Scan(...any) error { return nil }
func (rows *failingPrivacyRows) Err() error   { return rows.err }
func (rows *failingPrivacyRows) Close() error { rows.closed = true; return nil }

type scriptedBootstrapRunner struct {
	reports []bootstrap.SliceReport
	calls   int
}

func (runner *scriptedBootstrapRunner) RunSlice(
	context.Context,
	string,
	bootstrap.SliceBudget,
) (bootstrap.SliceReport, error) {
	report := runner.reports[runner.calls]
	runner.calls++
	return report, nil
}

func TestValidateConfigFailsClosed(t *testing.T) {
	t.Parallel()
	absolute := filepath.Join(t.TempDir(), "codex")
	tests := []config{
		{},
		{home: absolute, confirm: "yes"},
		{home: "relative", confirm: confirmationPhrase},
		{home: absolute, confirm: confirmationPhrase, observeAppend: -time.Second},
		{home: absolute, confirm: confirmationPhrase, observeAppend: 2 * time.Minute},
		{home: absolute, confirm: confirmationPhrase, tempParent: "relative"},
	}
	for index, input := range tests {
		if err := validateConfig(input); !errors.Is(err, errInvalidConfig) {
			t.Fatalf("case %d error = %v, want invalid config", index, err)
		}
	}
}

func TestValidateConfigRejectsIsolatedParentInsideConfirmedHome(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	inside := filepath.Join(home, "derived")
	if err := os.Mkdir(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	alias := filepath.Join(outside, "home-alias")
	if err := os.Symlink(home, alias); err != nil {
		t.Fatal(err)
	}
	for _, parent := range []string{home, inside, alias} {
		input := config{home: home, confirm: confirmationPhrase, tempParent: parent}
		if err := validateConfig(input); !errors.Is(err, errInvalidConfig) {
			t.Fatalf("temp parent %q error = %v, want invalid config", parent, err)
		}
	}
	if err := validateConfig(config{
		home: home, confirm: confirmationPhrase, tempParent: outside,
	}); err != nil {
		t.Fatalf("outside temp parent error = %v", err)
	}
}

func TestClassifyReconcileOnlyAllowsReadOnlySourceEvolution(t *testing.T) {
	t.Parallel()
	allowed := logs.ReconcilePlan{Actions: []logs.ReconcileAction{
		{Kind: logs.ChangeUnchanged}, {Kind: logs.ChangeGrown}, {Kind: logs.ChangeAdded}, {Kind: logs.ChangeMoved},
	}}
	result := summary{}
	if err := classifyReconcile(allowed, &result); err != nil {
		t.Fatalf("classifyReconcile(allowed) error = %v", err)
	}
	if result.UnchangedSources != 1 || result.GrownSources != 1 || result.AddedSources != 1 || result.MovedSources != 1 {
		t.Fatalf("allowed counts = %#v", result)
	}
	for _, kind := range []logs.ChangeKind{
		logs.ChangeTruncated, logs.ChangeReplaced, logs.ChangeDeleted, logs.ChangeUnreadable,
	} {
		if err := classifyReconcile(logs.ReconcilePlan{Actions: []logs.ReconcileAction{{Kind: kind}}}, &summary{}); !errors.Is(err, errSourceMutation) {
			t.Fatalf("kind %s error = %v, want source mutation", kind, err)
		}
	}
}

func TestSensitiveEnvelopeAndDTOSanitizer(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"Authorization: Bearer secret", `{"role":"user"}`, `{"type":"response_item"}`, `{"tool_output":{}}`,
		`{" access_token " : "secret"}`, `{"prompt" : "private"}`, "Cookie: private",
	} {
		if !containsSensitiveEnvelope(value) {
			t.Fatalf("sensitive value was accepted")
		}
	}
	if err := scanDTOs("/private/codex", map[string]string{"path": "/private/codex"}); !errors.Is(err, errPrivacyContract) {
		t.Fatalf("scanDTOs(home) error = %v", err)
	}
	if err := scanDTOs("/private/codex", map[string]string{"project": "/Users/example/work/project"}); !errors.Is(err, errPrivacyContract) {
		t.Fatalf("scanDTOs(outside absolute path) error = %v", err)
	}
	if err := scanDTOs("/private/codex", map[string]string{"status": "complete"}); err != nil {
		t.Fatalf("scanDTOs(safe) error = %v", err)
	}
}

func TestScanPrivacyRowsFailsClosedOnIterationError(t *testing.T) {
	t.Parallel()
	want := errors.New("iteration failed")
	rows := &failingPrivacyRows{err: want}
	result := privacyResult{}
	if err := scanPrivacyRows(rows, &result); !errors.Is(err, want) {
		t.Fatalf("scanPrivacyRows() error = %v, want iteration failure", err)
	}
	if !rows.closed {
		t.Fatal("scanPrivacyRows() did not close rows")
	}
	var _ privacyRows = rows
}

func TestSameKnownUsageTotalsRequiresExactKnownIntegers(t *testing.T) {
	t.Parallel()
	known := func(value int64, unit basequery.NumericUnit) basequery.NumericValue {
		result, err := basequery.KnownNumeric(value, unit)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	totals := usagecost.UsageTotals{
		TurnCount: known(3, basequery.NumericCount), InputTokens: known(10, basequery.NumericTokens),
		CachedInputTokens: known(2, basequery.NumericTokens), OutputTokens: known(4, basequery.NumericTokens),
		ReasoningTokens: known(1, basequery.NumericTokens), TotalTokens: known(14, basequery.NumericTokens),
		EstimatedUSDMicros: known(7, basequery.NumericMicroUSD),
		PricedTurnCount:    known(2, basequery.NumericCount), UnpricedTurnCount: known(1, basequery.NumericCount),
	}
	if !sameKnownUsageTotals(totals, totals) {
		t.Fatal("equal known totals were rejected")
	}
	different := totals
	different.TotalTokens = known(15, basequery.NumericTokens)
	if sameKnownUsageTotals(totals, different) {
		t.Fatal("different totals were accepted")
	}
	unknown := totals
	unknown.TotalTokens, _ = basequery.UnknownNumeric(basequery.NumericTokens, basequery.UnknownUnavailable)
	if sameKnownUsageTotals(totals, unknown) {
		t.Fatal("unknown totals were accepted")
	}
}

func TestRemoveIsolatedRootRejectsBroadPaths(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/", t.TempDir(), "/tmp/codex-pulse-m11-real-"} {
		if err := removeIsolatedRoot(path); !errors.Is(err, errUnsafeCleanup) {
			t.Fatalf("removeIsolatedRoot(%q) error = %v", path, err)
		}
	}
	root, err := filepath.Abs(filepath.Join(t.TempDir(), "codex-pulse-m11-real-test"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".m11-real-home-root"), []byte(isolationMarker), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeIsolatedRoot(root); err != nil {
		t.Fatalf("removeIsolatedRoot(private) error = %v", err)
	}
}

func TestStableFailureNeverIncludesUnderlyingDetails(t *testing.T) {
	t.Parallel()
	secret := errors.New("/Users/example/.codex Authorization: Bearer secret")
	if got := stableFailure(secret); got != "M11-LIVE-090: live validation failed" {
		t.Fatalf("stableFailure() = %q", got)
	}
}

func TestStableFailureClassifiesPublicQuerySubstageWithoutCause(t *testing.T) {
	t.Parallel()
	secret := errors.New("/private/session-id")
	err := errors.Join(errStageSessionQuery, basequery.NewUnavailableFailure(secret))
	got := stableFailure(err)
	if got != "M11-LIVE-015B: public session query failed category unavailable detail other" || strings.Contains(got, secret.Error()) {
		t.Fatalf("stableFailure(query) = %q", got)
	}
	classified := errors.Join(
		errStageSessionQuery,
		basequery.NewUnavailableFailure(errors.New("session analytics attribution is incomplete")),
	)
	if got := stableFailure(classified); got != "M11-LIVE-015B: public session query failed category unavailable detail session_attribution_incomplete" {
		t.Fatalf("stableFailure(classified query) = %q", got)
	}
}

func TestStableFailureClassifiesCostRollupWithoutCause(t *testing.T) {
	t.Parallel()
	secret := errors.New("private cost rollup cause")
	got := stableFailure(errors.Join(errStageCostRollup, secret))
	if got != "M11-LIVE-015D: UTC cost rollup failed detail other" || strings.Contains(got, secret.Error()) {
		t.Fatalf("stableFailure(cost rollup) = %q", got)
	}
}

func TestRunBootstrapWithProgressReportsBoundedSlices(t *testing.T) {
	t.Parallel()
	runner := &scriptedBootstrapRunner{reports: []bootstrap.SliceReport{
		{FilesProcessed: 2, BytesRead: 10, Active: time.Second, ExhaustedBy: bootstrap.SliceStopTimeBudget},
		{
			RunReport: bootstrap.RunReport{FullHistoryReady: true}, FilesProcessed: 1, BytesRead: 4,
			Active: 2 * time.Second, Complete: true, ExhaustedBy: bootstrap.SliceStopCompleted,
		},
	}}
	var progress strings.Builder
	report, err := runBootstrapWithProgress(context.Background(), runner, "job", &progress)
	if err != nil || !report.FullHistoryReady || runner.calls != 2 {
		t.Fatalf("runBootstrapWithProgress() = %#v, %v; calls=%d", report, err, runner.calls)
	}
	if got := progress.String(); !strings.Contains(got, "slice=1 files=2 slice_bytes=10 total_bytes=10") ||
		!strings.Contains(got, "slice=2 files=1 slice_bytes=4 total_bytes=14") {
		t.Fatalf("progress = %q", got)
	}
	if _, err := runBootstrapWithProgress(context.Background(), nil, "job", io.Discard); !errors.Is(err, errInvalidConfig) {
		t.Fatalf("runBootstrapWithProgress(nil) error = %v, want invalid config", err)
	}
}
