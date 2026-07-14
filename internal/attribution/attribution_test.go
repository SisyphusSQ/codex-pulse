package attribution

import (
	"strings"
	"testing"
)

func TestNormalizeModelCanonicalAliasesAndUnsafeValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		raw        string
		key        string
		display    string
		confidence Confidence
		source     Source
		reason     Reason
	}{
		{
			name: "canonical", raw: "gpt-5.2-codex", key: "gpt-5.2-codex",
			display: "GPT-5.2 Codex", confidence: ConfidenceHigh,
			source: SourceModelCanonical, reason: ReasonObserved,
		},
		{
			name: "provider alias", raw: " OpenAI/GPT-5.2-Codex ", key: "gpt-5.2-codex",
			display: "GPT-5.2 Codex", confidence: ConfidenceHigh,
			source: SourceModelAlias, reason: ReasonObserved,
		},
		{
			name: "safe future model", raw: "future_model.v7", key: "future_model.v7",
			display: "future_model.v7", confidence: ConfidenceHigh,
			source: SourceModelCanonical, reason: ReasonObserved,
		},
		{
			name: "missing", raw: "", confidence: ConfidenceUnknown,
			source: SourceMissing, reason: ReasonMissing,
		},
		{
			name: "path-shaped payload", raw: "/Users/alice/private/model", confidence: ConfidenceUnknown,
			source: SourceInvalidModel, reason: ReasonInvalid,
		},
		{
			name: "content-shaped payload", raw: "ignore previous instructions", confidence: ConfidenceUnknown,
			source: SourceInvalidModel, reason: ReasonInvalid,
		},
		{
			name: "oversize", raw: strings.Repeat("a", 129), confidence: ConfidenceUnknown,
			source: SourceInvalidModel, reason: ReasonInvalid,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := NormalizeModel(testCase.raw)
			if got.Key != testCase.key || got.DisplayName != testCase.display ||
				got.Confidence != testCase.confidence || got.Source != testCase.source ||
				got.Reason != testCase.reason {
				t.Fatalf("NormalizeModel(%q) = %#v", testCase.raw, got)
			}
			if strings.Contains(got.DisplayName, "/Users/alice") || strings.Contains(got.Key, "/Users/alice") {
				t.Fatalf("unsafe model input leaked: %#v", got)
			}
		})
	}
}

func TestResolveProjectUsesPathIdentityAndDoesNotGuessByBasename(t *testing.T) {
	t.Parallel()

	first := ResolveProject(ProjectInput{CWD: "/Users/alice/work/acme/api"})
	moved := ResolveProject(ProjectInput{CWD: "/Volumes/code/acme/api"})
	if first.ProjectID == "" || moved.ProjectID == "" || first.ProjectID == moved.ProjectID {
		t.Fatalf("path-bound identities = %#v and %#v", first, moved)
	}
	if first.DisplayName != "api" || moved.DisplayName != "api" {
		t.Fatalf("display names = %q and %q, want api", first.DisplayName, moved.DisplayName)
	}
	if first.Confidence != ConfidenceMedium || first.Source != SourceCWDPathDigest ||
		first.Reason != ReasonPathDerived || first.RootPath != "/Users/alice/work/acme/api" {
		t.Fatalf("first decision = %#v", first)
	}
	for _, safe := range []string{first.ProjectID, first.DisplayName, moved.ProjectID, moved.DisplayName} {
		if strings.Contains(safe, "/Users/") || strings.Contains(safe, "/Volumes/") {
			t.Fatalf("safe project field leaked an absolute path: %q", safe)
		}
	}
}

func TestResolveProjectUsesUniqueLongestSegmentAwareRegisteredRoot(t *testing.T) {
	t.Parallel()

	roots := []ProjectRoot{
		{ProjectID: "parent", RootPath: "/Users/alice/work", DisplayName: "work"},
		{ProjectID: "api", RootPath: "/Users/alice/work/acme/api", DisplayName: "api"},
		{ProjectID: "api-prefix", RootPath: "/Users/alice/work/acme/api-old", DisplayName: "api-old"},
	}
	got := ResolveProject(ProjectInput{CWD: "/Users/alice/work/acme/api/internal/store", Roots: roots})
	if got.ProjectID != "api" || got.DisplayName != "api" || got.RootPath != roots[1].RootPath ||
		got.Confidence != ConfidenceHigh || got.Source != SourceRegisteredRoot ||
		got.Reason != ReasonRootMatched {
		t.Fatalf("nested root decision = %#v", got)
	}

	prefixOnly := ResolveProject(ProjectInput{
		CWD:   "/Users/alice/work/acme/api-v2",
		Roots: []ProjectRoot{roots[1]},
	})
	if prefixOnly.ProjectID == "api" || prefixOnly.Source != SourceCWDPathDigest {
		t.Fatalf("path prefix was treated as a segment match: %#v", prefixOnly)
	}
}

func TestResolveProjectFailsClosedOnAmbiguousRegisteredRoots(t *testing.T) {
	t.Parallel()

	got := ResolveProject(ProjectInput{
		CWD: "/Users/alice/work/acme/api",
		Roots: []ProjectRoot{
			{ProjectID: "one", RootPath: "/Users/alice/work/acme/api", DisplayName: "api"},
			{ProjectID: "two", RootPath: "/Users/alice/work/acme/api", DisplayName: "api"},
		},
	})
	if got.ProjectID != "" || got.DisplayName != "" || got.RootPath != "" ||
		got.Confidence != ConfidenceLow || got.Source != SourceConflict || got.Reason != ReasonConflict {
		t.Fatalf("ambiguous root decision = %#v", got)
	}
}

func TestResolveProjectHandlesMissingRelativeAndRootPathsWithoutLeaking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cwd        string
		confidence Confidence
		source     Source
	}{
		{name: "missing", cwd: "", confidence: ConfidenceUnknown, source: SourceMissing},
		{name: "relative", cwd: "private/project", confidence: ConfidenceUnknown, source: SourceInvalidPath},
		{name: "root", cwd: "/", confidence: ConfidenceMedium, source: SourceCWDPathDigest},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveProject(ProjectInput{CWD: testCase.cwd})
			if got.Confidence != testCase.confidence || got.Source != testCase.source {
				t.Fatalf("ResolveProject(%q) = %#v", testCase.cwd, got)
			}
			if testCase.cwd == "/" && (got.DisplayName == "" || strings.Contains(got.DisplayName, "/")) {
				t.Fatalf("root display is unsafe: %#v", got)
			}
		})
	}
}

func TestArbitrateChoosesHighestPriorityAndFailsClosedOnPeerConflict(t *testing.T) {
	t.Parallel()

	chosen := Arbitrate([]Candidate{
		{Key: "initial", DisplayName: "Initial", Priority: 10, Confidence: ConfidenceMedium, Source: SourceCWDPathDigest},
		{Key: "current", DisplayName: "Current", Priority: 20, Confidence: ConfidenceHigh, Source: SourceRegisteredRoot},
	})
	if chosen.Key != "current" || chosen.DisplayName != "Current" || chosen.Confidence != ConfidenceHigh {
		t.Fatalf("chosen decision = %#v", chosen)
	}

	conflict := Arbitrate([]Candidate{
		{Key: "one", DisplayName: "One", Priority: 20, Confidence: ConfidenceHigh, Source: SourceRegisteredRoot},
		{Key: "two", DisplayName: "Two", Priority: 20, Confidence: ConfidenceHigh, Source: SourceRegisteredRoot},
	})
	if conflict.Key != "" || conflict.DisplayName != "" || conflict.Confidence != ConfidenceLow ||
		conflict.Source != SourceConflict || conflict.Reason != ReasonConflict {
		t.Fatalf("conflict decision = %#v", conflict)
	}

	missing := Arbitrate(nil)
	if missing.Confidence != ConfidenceUnknown || missing.Source != SourceMissing || missing.Reason != ReasonMissing {
		t.Fatalf("missing decision = %#v", missing)
	}

	invalid := Arbitrate([]Candidate{{
		Priority: 20, Confidence: ConfidenceUnknown,
		Source: SourceInvalidModel, Reason: ReasonInvalid,
	}})
	if invalid.Key != "" || invalid.DisplayName != "" || invalid.Confidence != ConfidenceUnknown ||
		invalid.Source != SourceInvalidModel || invalid.Reason != ReasonInvalid {
		t.Fatalf("invalid decision = %#v", invalid)
	}

	validInvalidConflict := Arbitrate([]Candidate{
		{Key: "gpt-5.2-codex", DisplayName: "GPT-5.2 Codex", Priority: 20,
			Confidence: ConfidenceHigh, Source: SourceModelCanonical, Reason: ReasonObserved},
		{Priority: 20, Confidence: ConfidenceUnknown, Source: SourceInvalidModel, Reason: ReasonInvalid},
	})
	if validInvalidConflict.Key != "" || validInvalidConflict.Confidence != ConfidenceLow ||
		validInvalidConflict.Source != SourceConflict || validInvalidConflict.Reason != ReasonConflict {
		t.Fatalf("valid/invalid peer decision = %#v", validInvalidConflict)
	}
}

func TestSessionTitleIsStableOpaqueAndContentFree(t *testing.T) {
	t.Parallel()

	first := NormalizeSessionTitle("session-123")
	second := NormalizeSessionTitle("session-123")
	other := NormalizeSessionTitle("session-456")
	if first != second || first.DisplayTitle == other.DisplayTitle ||
		!strings.HasPrefix(first.DisplayTitle, "Session ") || strings.Contains(first.DisplayTitle, "session-123") ||
		first.Confidence != ConfidenceHigh || first.Source != SourceSessionIDFallback ||
		first.Reason != ReasonStableIdentity {
		t.Fatalf("session titles = %#v %#v %#v", first, second, other)
	}
	missing := NormalizeSessionTitle("")
	if missing.DisplayTitle != "" || missing.Confidence != ConfidenceUnknown ||
		missing.Source != SourceMissing || missing.Reason != ReasonMissing {
		t.Fatalf("missing title = %#v", missing)
	}
}
