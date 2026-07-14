package logs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPlanReconcileDecisionMatrixIsDeterministic(t *testing.T) {
	t.Parallel()

	root := filepath.Join(string(filepath.Separator), "synthetic", "codex-home")
	previous := []Snapshot{
		fixtureSnapshot(root+"/sessions/unchanged.jsonl", SourceKindSession, "dev", 1, 10, 10, "head"),
		fixtureSnapshot(root+"/sessions/grown.jsonl", SourceKindSession, "dev", 2, 10, 10, "head"),
		fixtureSnapshot(root+"/sessions/truncated.jsonl", SourceKindSession, "dev", 3, 20, 10, "head"),
		fixtureSnapshot(root+"/sessions/moved.jsonl", SourceKindSession, "dev", 4, 10, 10, "head"),
		fixtureSnapshot(root+"/sessions/replaced.jsonl", SourceKindSession, "dev", 5, 10, 10, "head"),
		fixtureSnapshot(root+"/sessions/deleted.jsonl", SourceKindSession, "dev", 6, 10, 10, "head"),
		fixtureSnapshot(root+"/sessions/unreadable.jsonl", SourceKindSession, "dev", 7, 10, 10, "head"),
		fixtureSnapshot(root+"/sessions/moved-grown.jsonl", SourceKindSession, "dev", 8, 10, 10, "head"),
	}
	current := DiscoveryResult{Snapshots: []Snapshot{
		previous[0],
		fixtureSnapshot(previous[1].Path, SourceKindSession, "dev", 2, 20, 11, "head"),
		fixtureSnapshot(previous[2].Path, SourceKindSession, "dev", 3, 8, 11, "head"),
		fixtureSnapshot(root+"/archived_sessions/moved.jsonl", SourceKindArchivedSession, "dev", 4, 10, 11, "head"),
		fixtureSnapshot(previous[4].Path, SourceKindSession, "dev", 50, 10, 11, "other"),
		fixtureSnapshot(root+"/sessions/added.jsonl", SourceKindSession, "dev", 9, 10, 10, "head"),
		fixtureSnapshot(root+"/archived_sessions/moved-grown.jsonl", SourceKindArchivedSession, "dev", 8, 30, 12, "head"),
	}, Issues: []DiscoveryIssue{{
		Path: previous[6].Path, Code: DiscoveryIssuePermission, Scope: IssueScopeExact, Retryable: true,
	}}}

	first, err := PlanReconcile(root, previous, current)
	if err != nil {
		t.Fatalf("PlanReconcile(first) error = %v", err)
	}
	second, err := PlanReconcile(root, previous, current)
	if err != nil {
		t.Fatalf("PlanReconcile(second) error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("PlanReconcile() is not deterministic:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if len(first.Actions) != 9 {
		t.Fatalf("PlanReconcile() actions = %#v, want 9", first.Actions)
	}

	want := map[string]ChangeKind{
		root + "/sessions/added.jsonl":                ChangeAdded,
		root + "/sessions/deleted.jsonl":              ChangeDeleted,
		root + "/sessions/grown.jsonl":                ChangeGrown,
		root + "/archived_sessions/moved-grown.jsonl": ChangeGrown,
		root + "/archived_sessions/moved.jsonl":       ChangeMoved,
		root + "/sessions/replaced.jsonl":             ChangeReplaced,
		root + "/sessions/truncated.jsonl":            ChangeTruncated,
		root + "/sessions/unchanged.jsonl":            ChangeUnchanged,
		root + "/sessions/unreadable.jsonl":           ChangeUnreadable,
	}
	for index, action := range first.Actions {
		path := actionPath(action)
		if index > 0 && actionPath(first.Actions[index-1]) > path {
			t.Fatalf("actions are not in stable path order: %#v", first.Actions)
		}
		if want[path] != action.Kind {
			t.Fatalf("action for %q = %q, want %q", path, action.Kind, want[path])
		}
		if path == root+"/archived_sessions/moved-grown.jsonl" && !action.PathChanged {
			t.Fatalf("moved+grown action = %#v, want PathChanged", action)
		}
	}
}

func TestPlanReconcileDetectsSameMTimeSameSizeContentReplacement(t *testing.T) {
	t.Parallel()

	home := filepath.Join(string(filepath.Separator), "synthetic", "codex-home")
	path := home + "/sessions/same.jsonl"
	previous := fixtureSnapshot(path, SourceKindSession, "dev", 1, 10, 99, "old-head")
	current := fixtureSnapshot(path, SourceKindSession, "dev", 1, 10, 99, "new-head")
	plan, err := PlanReconcile(home, []Snapshot{previous}, DiscoveryResult{Snapshots: []Snapshot{current}})
	if err != nil {
		t.Fatalf("PlanReconcile() error = %v", err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != ChangeReplaced {
		t.Fatalf("PlanReconcile() = %#v, want replaced", plan)
	}
}

func TestPlanReconcileTreatsShortFileAppendAsGrowth(t *testing.T) {
	t.Parallel()

	home := filepath.Join(string(filepath.Separator), "synthetic", "codex-home")
	path := home + "/sessions/short.jsonl"
	previous := fixtureSnapshot(path, SourceKindSession, "dev", 1, 3, 99, "abc")
	current := fixtureSnapshot(path, SourceKindSession, "dev", 1, 4, 100, "abcd")
	current.Comparison = &PrefixComparison{
		PrefixBytes:  previous.Fingerprint.PrefixBytes,
		PrefixSHA256: previous.Fingerprint.PrefixSHA256,
	}
	plan, err := PlanReconcile(home, []Snapshot{previous}, DiscoveryResult{Snapshots: []Snapshot{current}})
	if err != nil {
		t.Fatalf("PlanReconcile() error = %v", err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != ChangeGrown {
		t.Fatalf("PlanReconcile() = %#v, want grown", plan)
	}
}

func TestPlanReconcileTreatsUnprovenGrowthAsReplacement(t *testing.T) {
	t.Parallel()

	home := filepath.Join(string(filepath.Separator), "synthetic", "codex-home")
	path := home + "/sessions/short.jsonl"
	previous := fixtureSnapshot(path, SourceKindSession, "dev", 1, 3, 99, "abc")
	current := fixtureSnapshot(path, SourceKindSession, "dev", 1, 4, 100, "abcd")

	plan, err := PlanReconcile(home, []Snapshot{previous}, DiscoveryResult{Snapshots: []Snapshot{current}})
	if err != nil {
		t.Fatalf("PlanReconcile() error = %v", err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != ChangeReplaced {
		t.Fatalf("PlanReconcile() = %#v, want conservative replacement", plan)
	}
}

func TestPlanReconcileSubtreeIssuePreventsFalseDeletion(t *testing.T) {
	t.Parallel()

	root := filepath.Join(string(filepath.Separator), "synthetic", "codex-home")
	previous := []Snapshot{
		fixtureSnapshot(root+"/sessions/2026/a.jsonl", SourceKindSession, "dev", 1, 10, 10, "a"),
		fixtureSnapshot(root+"/sessions/2026/b.jsonl", SourceKindSession, "dev", 2, 10, 10, "b"),
		fixtureSnapshot(root+"/archived_sessions/c.jsonl", SourceKindArchivedSession, "dev", 3, 10, 10, "c"),
	}
	result := DiscoveryResult{Issues: []DiscoveryIssue{{
		Path: root + "/sessions/2026", Code: DiscoveryIssuePermission,
		Scope: IssueScopeSubtree, Retryable: true,
	}}}
	plan, err := PlanReconcile(root, previous, result)
	if err != nil {
		t.Fatalf("PlanReconcile() error = %v", err)
	}
	got := map[string]ChangeKind{}
	for _, action := range plan.Actions {
		got[actionPath(action)] = action.Kind
	}
	if got[previous[0].Path] != ChangeUnreadable || got[previous[1].Path] != ChangeUnreadable {
		t.Fatalf("subtree actions = %#v, want unreadable for session files", plan.Actions)
	}
	if got[previous[2].Path] != ChangeDeleted {
		t.Fatalf("archived action = %q, want deleted outside failed subtree", got[previous[2].Path])
	}
}

func TestPlanReconcileDuplicateIdentityIssuePreventsFalseDeletionAfterMove(t *testing.T) {
	t.Parallel()

	root := filepath.Join(string(filepath.Separator), "synthetic", "codex-home")
	previous := fixtureSnapshot(
		root+"/archived_sessions/old.jsonl", SourceKindArchivedSession, "dev", 7, 10, 10, "head",
	)
	result := DiscoveryResult{Issues: []DiscoveryIssue{
		{
			Path: root + "/sessions/duplicate-a.jsonl", SourceFileID: previous.SourceFileID,
			Code: DiscoveryIssueDuplicateIdentity, Scope: IssueScopeExact, Retryable: true,
		},
		{
			Path: root + "/sessions/duplicate-b.jsonl", SourceFileID: previous.SourceFileID,
			Code: DiscoveryIssueDuplicateIdentity, Scope: IssueScopeExact, Retryable: true,
		},
	}}

	plan, err := PlanReconcile(root, []Snapshot{previous}, result)
	if err != nil {
		t.Fatalf("PlanReconcile() error = %v", err)
	}
	for _, action := range plan.Actions {
		if action.Kind == ChangeDeleted {
			t.Fatalf("PlanReconcile() = %#v, duplicate identity must not delete previous source", plan)
		}
	}
	if len(plan.Actions) != 2 || plan.Actions[0].Kind != ChangeUnreadable ||
		plan.Actions[0].Previous == nil || plan.Actions[0].Previous.SourceFileID != previous.SourceFileID {
		t.Fatalf("PlanReconcile() = %#v, want previous source protected as unreadable", plan)
	}
}

func TestPlanReconcileRejectsDuplicateIdentityOrPath(t *testing.T) {
	t.Parallel()

	home := filepath.Join(string(filepath.Separator), "synthetic", "codex-home")
	root := home + "/sessions"
	first := fixtureSnapshot(root+"/a.jsonl", SourceKindSession, "dev", 1, 10, 10, "a")
	duplicateIdentity := first
	duplicateIdentity.Path = root + "/b.jsonl"
	if _, err := PlanReconcile(home, []Snapshot{first, duplicateIdentity}, DiscoveryResult{}); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("PlanReconcile(duplicate identity) error = %v, want ErrInvalidSnapshot", err)
	}

	duplicatePath := fixtureSnapshot(first.Path, SourceKindSession, "dev", 2, 10, 10, "b")
	if _, err := PlanReconcile(home, []Snapshot{first, duplicatePath}, DiscoveryResult{}); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("PlanReconcile(duplicate path) error = %v, want ErrInvalidSnapshot", err)
	}
}

func TestPlanReconcileRejectsContradictoryComparisonProof(t *testing.T) {
	t.Parallel()

	home := filepath.Join(string(filepath.Separator), "synthetic", "codex-home")
	path := home + "/sessions/proof.jsonl"
	snapshot := fixtureSnapshot(path, SourceKindSession, "dev", 1, 4, 100, "abcd")
	otherHash := sha256.Sum256([]byte("wxyz"))
	snapshot.Comparison = &PrefixComparison{
		PrefixBytes: snapshot.Fingerprint.PrefixBytes, PrefixSHA256: hex.EncodeToString(otherHash[:]),
	}

	if _, err := PlanReconcile(home, nil, DiscoveryResult{Snapshots: []Snapshot{snapshot}}); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("PlanReconcile(contradictory proof) error = %v, want ErrInvalidSnapshot", err)
	}
}

func TestPlanReconcileMatchesAllIdentitiesBeforeReusedPaths(t *testing.T) {
	t.Parallel()

	home := filepath.Join(string(filepath.Separator), "synthetic", "codex-home")
	old := fixtureSnapshot(home+"/sessions/a.jsonl", SourceKindSession, "dev", 1, 10, 10, "head")
	moved := fixtureSnapshot(
		home+"/archived_sessions/a.jsonl", SourceKindArchivedSession, "dev", 1, 10, 11, "head",
	)
	reusedPath := fixtureSnapshot(home+"/sessions/a.jsonl", SourceKindSession, "dev", 2, 5, 12, "new")

	plan, err := PlanReconcile(home, []Snapshot{old}, DiscoveryResult{Snapshots: []Snapshot{reusedPath, moved}})
	if err != nil {
		t.Fatalf("PlanReconcile() error = %v", err)
	}
	if len(plan.Actions) != 2 {
		t.Fatalf("PlanReconcile() actions = %#v, want moved + added", plan.Actions)
	}
	kinds := map[string]ChangeKind{}
	previousUses := 0
	for _, action := range plan.Actions {
		kinds[actionPath(action)] = action.Kind
		if action.Previous != nil && action.Previous.SourceFileID == old.SourceFileID {
			previousUses++
		}
	}
	if kinds[moved.Path] != ChangeMoved || kinds[reusedPath.Path] != ChangeAdded || previousUses != 1 {
		t.Fatalf("PlanReconcile() = %#v, want moved old + added reused path with one previous use", plan)
	}
}

func TestPlanReconcileRejectsSourcesOutsideConfirmedAllowlist(t *testing.T) {
	t.Parallel()

	home := filepath.Join(string(filepath.Separator), "synthetic", "codex-home")
	tests := []struct {
		name      string
		previous  []Snapshot
		discovery DiscoveryResult
	}{
		{
			name: "outside home",
			previous: []Snapshot{fixtureSnapshot(
				filepath.Join(string(filepath.Separator), "synthetic", "outside.jsonl"),
				SourceKindSession, "dev", 1, 4, 1, "head",
			)},
		},
		{
			name: "arbitrary root jsonl",
			discovery: DiscoveryResult{Snapshots: []Snapshot{fixtureSnapshot(
				home+"/other.jsonl", SourceKindSessionIndex, "dev", 2, 4, 1, "head",
			)}},
		},
		{
			name: "kind path mismatch",
			discovery: DiscoveryResult{Snapshots: []Snapshot{fixtureSnapshot(
				home+"/sessions/a.jsonl", SourceKindArchivedSession, "dev", 3, 4, 1, "head",
			)}},
		},
		{
			name: "outside issue",
			discovery: DiscoveryResult{Issues: []DiscoveryIssue{{
				Path: filepath.Join(string(filepath.Separator), "synthetic", "outside"),
				Code: DiscoveryIssuePermission, Scope: IssueScopeSubtree, Retryable: true,
			}}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := PlanReconcile(home, test.previous, test.discovery); !errors.Is(err, ErrInvalidSnapshot) {
				t.Fatalf("PlanReconcile() error = %v, want ErrInvalidSnapshot", err)
			}
		})
	}

	if _, err := PlanReconcile("relative/home", nil, DiscoveryResult{}); !errors.Is(err, ErrInvalidHome) {
		t.Fatalf("PlanReconcile(relative home) error = %v, want ErrInvalidHome", err)
	}
}

func fixtureSnapshot(
	path string,
	kind SourceKind,
	deviceID string,
	inode int64,
	sizeBytes int64,
	mtimeNS int64,
	prefix string,
) Snapshot {
	prefixHash := sha256.Sum256([]byte(prefix))
	fingerprint := buildFingerprint(
		deviceID, inode, sizeBytes, mtimeNS, int64(len(prefix)), hex.EncodeToString(prefixHash[:]),
	)
	return Snapshot{
		SourceFileID: sourceFileID(ProviderCodex, deviceID, inode),
		Provider:     ProviderCodex,
		Kind:         kind,
		Path:         path,
		Fingerprint:  fingerprint,
	}
}

func actionPath(action ReconcileAction) string {
	if action.Current != nil {
		return action.Current.Path
	}
	if action.Previous != nil {
		return action.Previous.Path
	}
	if action.Issue != nil {
		return action.Issue.Path
	}
	return ""
}
