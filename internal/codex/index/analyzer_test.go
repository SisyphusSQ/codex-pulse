package index

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"

	"github.com/google/uuid"
)

func TestAnalyzePlansOnlyMissingAndStrictlyNewerExpectedNames(t *testing.T) {
	t.Parallel()

	parsed, err := Parse([]byte(
		`{"id":"019d4b69-25d1-7be2-b5f4-8c4234ed5dee","thread_name":"same","updated_at":"2026-04-01T00:00:00Z"}` + "\n" +
			`{"id":"019d4b69-25d1-7be2-b5f4-8c4234ed5def","thread_name":"old","updated_at":"2026-04-01T00:00:00Z"}` + "\n" +
			`{"id":"019d4b69-25d1-7be2-b5f4-8c4234ed5df0","thread_name":"index-newer","updated_at":"2026-04-03T00:00:00Z"}` + "\n" +
			`{"id":"019d4b69-25d1-7be2-b5f4-8c4234ed5dee","thread_name":"same","updated_at":"2026-04-02T00:00:00Z"}` + "\n",
	))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	expected := []Expectation{
		{SessionID: "019d4b69-25d1-7be2-b5f4-8c4234ed5dee", ThreadName: "same", UpdatedAtMS: 1775088000000},
		{SessionID: "019d4b69-25d1-7be2-b5f4-8c4234ed5def", ThreadName: "store-newer", UpdatedAtMS: 1775174400000},
		{SessionID: "019d4b69-25d1-7be2-b5f4-8c4234ed5df0", ThreadName: "store-older", UpdatedAtMS: 1775001600000},
		{SessionID: "019d4b69-25d1-7be2-b5f4-8c4234ed5df1", ThreadName: "missing", UpdatedAtMS: 1775260800000},
	}
	version := FileVersion{Exists: true, DeviceID: "1", Inode: 2, SizeBytes: 400, MTimeNS: 3, SHA256: testDigest("source")}

	plan, err := Analyze(parsed, expected, version, 1775260800123)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(plan.Actions) != 2 {
		t.Fatalf("actions = %#v, want 2", plan.Actions)
	}
	if plan.Actions[0].SessionID != "019d4b69-25d1-7be2-b5f4-8c4234ed5def" || plan.Actions[0].Reason != ReasonStale ||
		plan.Actions[1].SessionID != "019d4b69-25d1-7be2-b5f4-8c4234ed5df1" || plan.Actions[1].Reason != ReasonMissing {
		t.Fatalf("actions = %#v", plan.Actions)
	}
	if len(plan.Conflicts) != 1 || plan.Conflicts[0].SessionID != "019d4b69-25d1-7be2-b5f4-8c4234ed5df0" ||
		plan.Conflicts[0].Reason != ConflictIndexNewerOrUnknown {
		t.Fatalf("conflicts = %#v", plan.Conflicts)
	}
	if len(plan.Histories) != 1 || plan.Histories[0].Count != 2 {
		t.Fatalf("histories = %#v", plan.Histories)
	}
	if err := VerifyPlan(plan); err != nil {
		t.Fatalf("VerifyPlan() error = %v", err)
	}

	repeated, err := Analyze(parsed, expected, version, 1775260800123)
	if err != nil || repeated.ID != plan.ID {
		t.Fatalf("repeated plan = %#v, error = %v", repeated, err)
	}
	plan.Actions[0].ThreadName = "tampered"
	if err := VerifyPlan(plan); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("VerifyPlan(tampered) error = %v, want ErrInvalidPlan", err)
	}
}

func TestAnalyzeRejectsConflictingExpectedIdentity(t *testing.T) {
	t.Parallel()

	_, err := Analyze(ParsedIndex{}, []Expectation{
		{SessionID: "019d4b69-25d1-7be2-b5f4-8c4234ed5dee", ThreadName: "one", UpdatedAtMS: 1},
		{SessionID: "019d4b69-25d1-7be2-b5f4-8c4234ed5dee", ThreadName: "two", UpdatedAtMS: 2},
	}, FileVersion{}, 3)
	if !errors.Is(err, ErrInvalidExpectation) {
		t.Fatalf("Analyze() error = %v, want ErrInvalidExpectation", err)
	}
}

func TestAnalyzeRejectsDiagnosticRawContent(t *testing.T) {
	t.Parallel()

	_, err := Analyze(
		ParsedIndex{Diagnostics: []Diagnostic{{
			Line: 1, Code: DiagnosticMalformedJSON, Raw: "private session name",
		}}},
		nil,
		FileVersion{},
		3,
	)
	if !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("Analyze() error = %v, want ErrInvalidPlan", err)
	}
}

func TestVerifyPlanRejectsSelfConsistentInvalidStructure(t *testing.T) {
	t.Parallel()

	plan := RepairPlan{
		AnalyzedAtMS: 3,
		Source:       FileVersion{},
		Actions: []RepairAction{{
			SessionID:  "019d4b69-25d1-7be2-b5f4-8c4234ed5dee",
			ThreadName: "valid",
			UpdatedAt:  "2026-04-01T23:39:24Z",
			Reason:     RepairReason("unsupported"),
		}},
	}
	plan.ID = planDigest(plan)
	if err := VerifyPlan(plan); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("VerifyPlan() error = %v, want ErrInvalidPlan", err)
	}
}

func TestAnalyzeRejectsCorrectionsThatCannotFitConfirmedIndex(t *testing.T) {
	t.Parallel()

	version := FileVersion{
		Exists: true, DeviceID: "1", Inode: 2, SizeBytes: maxIndexBytes - 1,
		MTimeNS: 3, SHA256: testDigest("almost-full"),
	}
	_, err := Analyze(
		ParsedIndex{},
		[]Expectation{{
			SessionID:  "019d4b69-25d1-7be2-b5f4-8c4234ed5dee",
			ThreadName: "missing", UpdatedAtMS: 1,
		}},
		version,
		2,
	)
	if !errors.Is(err, ErrIndexTooLarge) {
		t.Fatalf("Analyze() error = %v, want ErrIndexTooLarge", err)
	}

	validPlan, err := Analyze(
		ParsedIndex{},
		[]Expectation{{
			SessionID:  "019d4b69-25d1-7be2-b5f4-8c4234ed5dee",
			ThreadName: "missing", UpdatedAtMS: 1,
		}},
		FileVersion{},
		2,
	)
	if err != nil {
		t.Fatalf("Analyze(valid) error = %v", err)
	}
	validPlan.Source = version
	validPlan.ID = planDigest(validPlan)
	if err := VerifyPlan(validPlan); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("VerifyPlan(oversized) error = %v, want ErrInvalidPlan", err)
	}
}

func TestAnalyzeSupportsMoreThan4096CorrectionsWhenTheyFit(t *testing.T) {
	t.Parallel()

	expectations := make([]Expectation, 4097)
	for index := range expectations {
		expectations[index] = Expectation{
			SessionID:  uuid.NewSHA1(uuid.NameSpaceOID, []byte(strconv.Itoa(index))).String(),
			ThreadName: "missing", UpdatedAtMS: int64(index + 1),
		}
	}
	plan, err := Analyze(ParsedIndex{}, expectations, FileVersion{}, 5000)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	entries := make([]Entry, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		entries = append(entries, Entry{
			ID: action.SessionID, ThreadName: action.ThreadName, UpdatedAt: action.UpdatedAt,
		})
	}
	if _, err := canonicalEntries(entries); err != nil {
		t.Fatalf("canonicalEntries(%d) error = %v", len(entries), err)
	}
}

func testDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
