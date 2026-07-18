package logs

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDiscovererFindsAllowlistedJSONLWithStableFingerprints(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	sessionPath := writeJSONLFixture(t, home, "sessions/2026/07/14/session-a.jsonl", "session-secret-a\n")
	archivedPath := writeJSONLFixture(t, home, "archived_sessions/session-b.jsonl", "session-secret-b\n")
	indexPath := writeJSONLFixture(t, home, "session_index.jsonl", "index-secret\n")
	writeJSONLFixture(t, home, "sessions/ignored.txt", "not-jsonl")
	writeJSONLFixture(t, home, "outside.jsonl", "not-allowlisted")

	discoverer, err := NewDiscoverer(home)
	if err != nil {
		t.Fatalf("NewDiscoverer() error = %v", err)
	}
	first, err := discoverer.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover(first) error = %v", err)
	}
	second, err := discoverer.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover(second) error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repeated Discover() differs:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if len(first.Issues) != 0 || len(first.Snapshots) != 3 {
		t.Fatalf("Discover() = %#v, want 3 snapshots and no issues", first)
	}

	wantKinds := map[string]SourceKind{
		archivedPath: SourceKindArchivedSession,
		indexPath:    SourceKindSessionIndex,
		sessionPath:  SourceKindSession,
	}
	for index, snapshot := range first.Snapshots {
		if index > 0 && first.Snapshots[index-1].Path >= snapshot.Path {
			t.Fatalf("snapshots are not in stable path order: %#v", first.Snapshots)
		}
		if wantKinds[snapshot.Path] != snapshot.Kind {
			t.Fatalf("snapshot kind for %q = %q, want %q", snapshot.Path, snapshot.Kind, wantKinds[snapshot.Path])
		}
		if snapshot.SourceFileID == "" || snapshot.Fingerprint.Digest == "" ||
			len(snapshot.Fingerprint.PrefixSHA256) != 64 || snapshot.Comparison != nil {
			t.Fatalf("snapshot fingerprint is incomplete: %#v", snapshot)
		}
	}

	serialized := strings.Join([]string{
		first.Snapshots[0].Fingerprint.Digest,
		first.Snapshots[1].Fingerprint.Digest,
		first.Snapshots[2].Fingerprint.Digest,
	}, " ")
	for _, secret := range []string{"session-secret-a", "session-secret-b", "index-secret"} {
		if strings.Contains(serialized, secret) {
			t.Fatalf("fingerprint result leaked source content %q", secret)
		}
	}
}

func TestDiscovererTreatsMissingTopLevelRootsAsEmpty(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	discoverer, err := NewDiscoverer(home)
	if err != nil {
		t.Fatalf("NewDiscoverer() error = %v", err)
	}
	result, err := discoverer.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(result.Snapshots) != 0 || len(result.Issues) != 0 {
		t.Fatalf("Discover(empty roots) = %#v, want legitimate empty result", result)
	}
}

func TestDiscovererRejectsSymlinksAndDuplicatePhysicalIdentity(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	original := writeJSONLFixture(t, home, "sessions/original.jsonl", "safe\n")
	if err := os.Link(original, filepath.Join(home, "sessions", "hardlink.jsonl")); err != nil {
		t.Fatalf("os.Link() error = %v", err)
	}
	outsideDir := t.TempDir()
	outsideFile := writeJSONLFixture(t, outsideDir, "outside.jsonl", "outside-secret\n")
	if err := os.Symlink(outsideFile, filepath.Join(home, "sessions", "escape.jsonl")); err != nil {
		t.Fatalf("os.Symlink(file) error = %v", err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(home, "sessions", "escape-dir")); err != nil {
		t.Fatalf("os.Symlink(dir) error = %v", err)
	}

	discoverer, err := NewDiscoverer(home)
	if err != nil {
		t.Fatalf("NewDiscoverer() error = %v", err)
	}
	result, err := discoverer.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(result.Snapshots) != 0 {
		t.Fatalf("Discover() snapshots = %#v, want ambiguous/symlink files excluded", result.Snapshots)
	}
	assertIssueCount(t, result.Issues, DiscoveryIssueUnsafeSymlink, 2)
	assertIssueCount(t, result.Issues, DiscoveryIssueDuplicateIdentity, 2)
	for _, issue := range result.Issues {
		if issue.Code == DiscoveryIssueDuplicateIdentity && issue.SourceFileID == "" {
			t.Fatalf("duplicate identity issue lacks SourceFileID: %#v", issue)
		}
	}

	symlinkHome := filepath.Join(t.TempDir(), "codex-home")
	if err := os.Symlink(home, symlinkHome); err != nil {
		t.Fatalf("os.Symlink(home) error = %v", err)
	}
	if _, err := NewDiscoverer(symlinkHome); !errors.Is(err, ErrUnsafeHome) {
		t.Fatalf("NewDiscoverer(symlink home) error = %v, want ErrUnsafeHome", err)
	}
	if _, err := NewDiscoverer("relative/home"); !errors.Is(err, ErrInvalidHome) {
		t.Fatalf("NewDiscoverer(relative home) error = %v, want ErrInvalidHome", err)
	}
}

func TestDiscovererBindsConfirmedHomePhysicalIdentity(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	home := filepath.Join(parent, "codex-home")
	writeJSONLFixture(t, home, "sessions/confirmed.jsonl", "confirmed\n")
	discoverer, err := NewDiscoverer(home)
	if err != nil {
		t.Fatalf("NewDiscoverer() error = %v", err)
	}
	if err := os.Rename(home, filepath.Join(parent, "old-home")); err != nil {
		t.Fatalf("os.Rename(home) error = %v", err)
	}
	writeJSONLFixture(t, home, "sessions/unconfirmed.jsonl", "unconfirmed\n")
	result, err := discoverer.Discover(context.Background())
	if !errors.Is(err, ErrHomeChanged) {
		t.Fatalf("Discover(replaced home) error = %v, want ErrHomeChanged", err)
	}
	if len(result.Snapshots) != 0 || len(result.Issues) != 0 {
		t.Fatalf("Discover(replaced home) = %#v, want empty", result)
	}
}

func TestDiscovererDetectsParentSymlinkRetarget(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	firstParent := filepath.Join(parent, "first")
	secondParent := filepath.Join(parent, "second")
	writeJSONLFixture(t, filepath.Join(firstParent, "codex-home"), "sessions/first.jsonl", "first\n")
	writeJSONLFixture(t, filepath.Join(secondParent, "codex-home"), "sessions/second.jsonl", "second\n")
	link := filepath.Join(parent, "selected")
	if err := os.Symlink(firstParent, link); err != nil {
		t.Fatalf("os.Symlink(first parent) error = %v", err)
	}
	discoverer, err := NewDiscoverer(filepath.Join(link, "codex-home"))
	if err != nil {
		t.Fatalf("NewDiscoverer(parent symlink) error = %v", err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatalf("os.Remove(parent symlink) error = %v", err)
	}
	if err := os.Symlink(secondParent, link); err != nil {
		t.Fatalf("os.Symlink(second parent) error = %v", err)
	}
	if _, err := discoverer.Discover(context.Background()); !errors.Is(err, ErrHomeChanged) {
		t.Fatalf("Discover(retargeted parent) error = %v, want ErrHomeChanged", err)
	}
}

func TestDiscovererFDEnumerationRejectsDirectorySymlinkRace(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	previousPath := writeJSONLFixture(t, home, "sessions/nested/original.jsonl", "original\n")
	baselineDiscoverer, err := NewDiscoverer(home)
	if err != nil {
		t.Fatalf("NewDiscoverer(baseline) error = %v", err)
	}
	previous, err := baselineDiscoverer.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover(baseline) error = %v", err)
	}
	outside := t.TempDir()
	writeJSONLFixture(t, outside, "outside-secret.jsonl", "outside-secret\n")

	racedFS := &directoryRaceFileSystem{
		fileSystem: osFileSystem{},
		home:       home,
		outside:    outside,
	}
	discoverer, err := newDiscoverer(home, racedFS)
	if err != nil {
		t.Fatalf("newDiscoverer(race) error = %v", err)
	}
	current, err := discoverer.DiscoverAgainst(context.Background(), previous.Snapshots)
	if err != nil {
		t.Fatalf("DiscoverAgainst(race) error = %v", err)
	}
	if len(current.Snapshots) != 0 || len(current.Issues) != 1 {
		t.Fatalf("DiscoverAgainst(race) = %#v, want one subtree issue", current)
	}
	if current.Issues[0].Path != filepath.Join(home, "sessions", "nested") ||
		current.Issues[0].Scope != IssueScopeSubtree {
		t.Fatalf("race issue = %#v, want nested subtree", current.Issues[0])
	}
	if strings.Contains(current.Issues[0].Path, "outside-secret") {
		t.Fatalf("race issue leaked outside entry: %#v", current.Issues[0])
	}
	plan, err := PlanReconcile(home, previous.Snapshots, current)
	if err != nil {
		t.Fatalf("PlanReconcile(race) error = %v", err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != ChangeUnreadable ||
		plan.Actions[0].Previous == nil || plan.Actions[0].Previous.Path != previousPath {
		t.Fatalf("PlanReconcile(race) = %#v, want previous source unreadable", plan)
	}
}

func TestDiscovererRecursiveDirectoryDisappearanceProducesSubtreeIssue(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	previousPath := writeJSONLFixture(t, home, "sessions/nested/original.jsonl", "original\n")
	baselineDiscoverer, err := NewDiscoverer(home)
	if err != nil {
		t.Fatalf("NewDiscoverer(baseline) error = %v", err)
	}
	previous, err := baselineDiscoverer.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover(baseline) error = %v", err)
	}

	discoverer, err := newDiscoverer(home, &directoryDisappearFileSystem{
		fileSystem: osFileSystem{}, home: home,
	})
	if err != nil {
		t.Fatalf("newDiscoverer(disappear) error = %v", err)
	}
	current, err := discoverer.DiscoverAgainst(context.Background(), previous.Snapshots)
	if err != nil {
		t.Fatalf("DiscoverAgainst(disappear) error = %v", err)
	}
	wantPath := filepath.Join(home, "sessions", "nested")
	if len(current.Snapshots) != 0 || len(current.Issues) != 1 ||
		current.Issues[0].Path != wantPath || current.Issues[0].Scope != IssueScopeSubtree ||
		current.Issues[0].Code != DiscoveryIssueChangedDuringScan {
		t.Fatalf("DiscoverAgainst(disappear) = %#v, want changed subtree issue at %q", current, wantPath)
	}
	plan, err := PlanReconcile(home, previous.Snapshots, current)
	if err != nil {
		t.Fatalf("PlanReconcile(disappear) error = %v", err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != ChangeUnreadable ||
		plan.Actions[0].Previous == nil || plan.Actions[0].Previous.Path != previousPath {
		t.Fatalf("PlanReconcile(disappear) = %#v, want previous source unreadable", plan)
	}
}

func TestDiscovererAndPlannerDetectContentChangeWithStableSizeAndMTime(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := writeJSONLFixture(t, home, "sessions/session.jsonl", "old-value\n")
	discoverer, err := NewDiscoverer(home)
	if err != nil {
		t.Fatalf("NewDiscoverer() error = %v", err)
	}
	previous, err := discoverer.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover(previous) error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("new-value\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(replacement) error = %v", err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatalf("os.Chtimes() error = %v", err)
	}
	current, err := discoverer.DiscoverAgainst(context.Background(), previous.Snapshots)
	if err != nil {
		t.Fatalf("DiscoverAgainst(current) error = %v", err)
	}
	if len(previous.Snapshots) != 1 || len(current.Snapshots) != 1 ||
		previous.Snapshots[0].Fingerprint.SizeBytes != current.Snapshots[0].Fingerprint.SizeBytes ||
		previous.Snapshots[0].Fingerprint.MTimeNS != current.Snapshots[0].Fingerprint.MTimeNS {
		t.Fatalf("fixture did not preserve size/mtime: previous=%#v current=%#v", previous, current)
	}
	assertSingleChange(t, home, previous.Snapshots, current, ChangeReplaced)
}

func TestDiscoverAgainstDistinguishesShortAppendFromRewriteGrowth(t *testing.T) {
	t.Parallel()

	t.Run("append", func(t *testing.T) {
		home := t.TempDir()
		path := writeJSONLFixture(t, home, "sessions/short.jsonl", "abc")
		discoverer, err := NewDiscoverer(home)
		if err != nil {
			t.Fatalf("NewDiscoverer() error = %v", err)
		}
		previous, err := discoverer.Discover(context.Background())
		if err != nil {
			t.Fatalf("Discover(previous) error = %v", err)
		}
		file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatalf("os.OpenFile() error = %v", err)
		}
		if _, err := file.WriteString("d"); err != nil {
			_ = file.Close()
			t.Fatalf("WriteString() error = %v", err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		current, err := discoverer.DiscoverAgainst(context.Background(), previous.Snapshots)
		if err != nil {
			t.Fatalf("DiscoverAgainst() error = %v", err)
		}
		if current.Snapshots[0].Comparison == nil ||
			current.Snapshots[0].Comparison.PrefixBytes != previous.Snapshots[0].Fingerprint.PrefixBytes {
			t.Fatalf("comparison proof = %#v, want previous prefix length", current.Snapshots[0].Comparison)
		}
		assertSingleChange(t, home, previous.Snapshots, current, ChangeGrown)
	})

	t.Run("rewrite then grow", func(t *testing.T) {
		home := t.TempDir()
		path := writeJSONLFixture(t, home, "sessions/short.jsonl", "abc")
		discoverer, err := NewDiscoverer(home)
		if err != nil {
			t.Fatalf("NewDiscoverer() error = %v", err)
		}
		previous, err := discoverer.Discover(context.Background())
		if err != nil {
			t.Fatalf("Discover(previous) error = %v", err)
		}
		if err := os.WriteFile(path, []byte("wxyz"), 0o600); err != nil {
			t.Fatalf("os.WriteFile(rewrite) error = %v", err)
		}
		current, err := discoverer.DiscoverAgainst(context.Background(), previous.Snapshots)
		if err != nil {
			t.Fatalf("DiscoverAgainst() error = %v", err)
		}
		assertSingleChange(t, home, previous.Snapshots, current, ChangeReplaced)
	})
}

func TestDiscoverAgainstRejectsPreviousOutsideConfirmedAllowlist(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	discoverer, err := NewDiscoverer(home)
	if err != nil {
		t.Fatalf("NewDiscoverer() error = %v", err)
	}
	outside := fixtureSnapshot(
		filepath.Join(t.TempDir(), "outside.jsonl"), SourceKindSession, "dev", 1, 4, 1, "head",
	)
	if _, err := discoverer.DiscoverAgainst(context.Background(), []Snapshot{outside}); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("DiscoverAgainst(outside previous) error = %v, want ErrInvalidSnapshot", err)
	}
}

func TestNilDiscovererRejectsDiscoverAgainst(t *testing.T) {
	t.Parallel()

	var discoverer *Discoverer
	if _, err := discoverer.DiscoverAgainst(context.Background(), nil); !errors.Is(err, ErrInvalidHome) {
		t.Fatalf("DiscoverAgainst(nil receiver) error = %v, want ErrInvalidHome", err)
	}
}

func TestDiscovererRejectsNonRegularJSONLWithoutBlocking(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0o700); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	pipePath := filepath.Join(home, "sessions", "named-pipe.jsonl")
	if err := unix.Mkfifo(pipePath, 0o600); err != nil {
		t.Fatalf("unix.Mkfifo() error = %v", err)
	}
	discoverer, err := NewDiscoverer(home)
	if err != nil {
		t.Fatalf("NewDiscoverer() error = %v", err)
	}
	result, err := discoverer.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(result.Snapshots) != 0 {
		t.Fatalf("Discover() snapshots = %#v, want named pipe excluded", result.Snapshots)
	}
	assertIssueCount(t, result.Issues, DiscoveryIssueUnsupportedFile, 1)
}

func TestDiscovererReportsProbeFailuresWithoutPartialSnapshot(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	permissionPath := writeJSONLFixture(t, home, "sessions/permission.jsonl", "private\n")
	changedPath := writeJSONLFixture(t, home, "sessions/changed.jsonl", "changing\n")
	discoverer, err := newDiscoverer(home, faultFileSystem{
		fileSystem: osFileSystem{},
		probeErrors: map[string]error{
			permissionPath: fs.ErrPermission,
			changedPath:    ErrChangedDuringScan,
		},
	})
	if err != nil {
		t.Fatalf("newDiscoverer() error = %v", err)
	}
	result, err := discoverer.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(result.Snapshots) != 0 {
		t.Fatalf("Discover() snapshots = %#v, want failed probes excluded", result.Snapshots)
	}
	assertIssueCount(t, result.Issues, DiscoveryIssuePermission, 1)
	assertIssueCount(t, result.Issues, DiscoveryIssueChangedDuringScan, 1)

	recovered, err := NewDiscoverer(home)
	if err != nil {
		t.Fatalf("NewDiscoverer(recovered) error = %v", err)
	}
	recoveredResult, err := recovered.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover(recovered) error = %v", err)
	}
	if len(recoveredResult.Issues) != 0 || len(recoveredResult.Snapshots) != 2 {
		t.Fatalf("Discover(recovered) = %#v, want two healthy snapshots", recoveredResult)
	}
}

func TestOSScanRootProbeRejectsIntermediateSymlink(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0o700); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	outside := t.TempDir()
	writeJSONLFixture(t, outside, "secret.jsonl", "outside-secret\n")
	if err := os.Symlink(outside, filepath.Join(home, "sessions", "raced")); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}
	root, err := (osFileSystem{}).OpenRoot(home, true)
	if err != nil {
		t.Fatalf("OpenRoot() error = %v", err)
	}
	defer func() { _ = root.Close() }()
	if _, err := root.Probe("sessions/raced/secret.jsonl", PrefixLimitBytes); err == nil {
		t.Fatal("Probe(intermediate symlink) error = nil, want fail closed")
	}
}

func TestDiscovererHonorsCancellation(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeJSONLFixture(t, home, "sessions/a.jsonl", "value\n")
	discoverer, err := NewDiscoverer(home)
	if err != nil {
		t.Fatalf("NewDiscoverer() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := discoverer.Discover(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Discover(canceled) error = %v, want context.Canceled", err)
	}
	if len(result.Snapshots) != 0 || len(result.Issues) != 0 {
		t.Fatalf("Discover(canceled) result = %#v, want empty", result)
	}
}

type faultFileSystem struct {
	fileSystem
	probeErrors map[string]error
}

func (filesystem faultFileSystem) OpenRoot(path string, resolveAncestors bool) (scanRoot, error) {
	root, err := filesystem.fileSystem.OpenRoot(path, resolveAncestors)
	if err != nil {
		return nil, err
	}
	return &faultScanRoot{scanRoot: root, home: path, probeErrors: filesystem.probeErrors}, nil
}

type faultScanRoot struct {
	scanRoot
	home        string
	probeErrors map[string]error
}

func (root *faultScanRoot) Probe(relativePath string, prefixLimit int64) (fileProbe, error) {
	if err := root.probeErrors[filepath.Join(root.home, relativePath)]; err != nil {
		return fileProbe{}, err
	}
	return root.scanRoot.Probe(relativePath, prefixLimit)
}

type directoryRaceFileSystem struct {
	fileSystem
	home    string
	outside string
}

func (filesystem *directoryRaceFileSystem) OpenRoot(path string, resolveAncestors bool) (scanRoot, error) {
	root, err := filesystem.fileSystem.OpenRoot(path, resolveAncestors)
	if err != nil {
		return nil, err
	}
	return &directoryRaceRoot{
		scanRoot: root,
		home:     filesystem.home,
		outside:  filesystem.outside,
	}, nil
}

type directoryRaceRoot struct {
	scanRoot
	home    string
	outside string
	once    sync.Once
}

type directoryDisappearFileSystem struct {
	fileSystem
	home string
}

func (filesystem *directoryDisappearFileSystem) OpenRoot(path string, resolveAncestors bool) (scanRoot, error) {
	root, err := filesystem.fileSystem.OpenRoot(path, resolveAncestors)
	if err != nil {
		return nil, err
	}
	return &directoryDisappearRoot{scanRoot: root, home: filesystem.home}, nil
}

type directoryDisappearRoot struct {
	scanRoot
	home string
	once sync.Once
}

func (root *directoryDisappearRoot) ReadDir(relativePath string) ([]fs.DirEntry, error) {
	entries, err := root.scanRoot.ReadDir(relativePath)
	if err != nil {
		return nil, err
	}
	if relativePath == "sessions" {
		var mutationErr error
		root.once.Do(func() {
			mutationErr = os.Rename(
				filepath.Join(root.home, "sessions", "nested"),
				filepath.Join(root.home, "removed-after-enumeration"),
			)
		})
		if mutationErr != nil {
			return nil, mutationErr
		}
	}
	return entries, nil
}

func (root *directoryRaceRoot) ReadDir(relativePath string) ([]fs.DirEntry, error) {
	entries, err := root.scanRoot.ReadDir(relativePath)
	if err != nil {
		return nil, err
	}
	if relativePath == "sessions" {
		root.once.Do(func() {
			nested := filepath.Join(root.home, "sessions", "nested")
			_ = os.Rename(nested, filepath.Join(root.home, "sessions", "nested-original"))
			_ = os.Symlink(root.outside, nested)
		})
	}
	return entries, nil
}

func writeJSONLFixture(t *testing.T, root, relativePath, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
	return path
}

func assertIssueCount(t *testing.T, issues []DiscoveryIssue, code DiscoveryIssueCode, want int) {
	t.Helper()
	got := 0
	for _, issue := range issues {
		if issue.Code == code {
			got++
		}
	}
	if got != want {
		t.Fatalf("issue count for %q = %d, want %d; issues=%#v", code, got, want, issues)
	}
}

func assertSingleChange(
	t *testing.T,
	confirmedHome string,
	previous []Snapshot,
	current DiscoveryResult,
	want ChangeKind,
) {
	t.Helper()
	plan, err := PlanReconcile(confirmedHome, previous, current)
	if err != nil {
		t.Fatalf("PlanReconcile() error = %v", err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != want {
		t.Fatalf("PlanReconcile() = %#v, want %q", plan, want)
	}
}
