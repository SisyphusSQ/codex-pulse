package logs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConfirmedDiscovererInspectsOnlyRequestedRollout(t *testing.T) {
	t.Parallel()

	home, discoverer := confirmedInspectFixture(t)
	wanted := filepath.Join(home, "sessions", "2026", "wanted.jsonl")
	sibling := filepath.Join(home, "sessions", "2026", "sibling.jsonl")
	if err := os.WriteFile(wanted, []byte("wanted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("sibling\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	snapshot, err := discoverer.Inspect(context.Background(), wanted, nil)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if snapshot.Path != wanted || snapshot.Kind != SourceKindSession || snapshot.Fingerprint.SizeBytes != int64(len("wanted\n")) {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestConfirmedDiscovererInspectRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()

	home, discoverer := confirmedInspectFixture(t)
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	if err := os.WriteFile(outside, []byte("private\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "sessions", "escape.jsonl")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverer.Inspect(context.Background(), link, nil); !errors.Is(err, ErrUnsafeSource) {
		t.Fatalf("Inspect(symlink) error = %v, want ErrUnsafeSource", err)
	}
}

func TestConfirmedDiscovererInspectProducesPriorPrefixProof(t *testing.T) {
	t.Parallel()

	home, discoverer := confirmedInspectFixture(t)
	path := filepath.Join(home, "sessions", "append.jsonl")
	if err := os.WriteFile(path, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous, err := discoverer.Inspect(context.Background(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("second\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	current, err := discoverer.Inspect(context.Background(), path, &previous)
	if err != nil {
		t.Fatal(err)
	}
	if current.Comparison == nil || current.Comparison.PrefixBytes != previous.Fingerprint.PrefixBytes ||
		current.Comparison.PrefixSHA256 != previous.Fingerprint.PrefixSHA256 {
		t.Fatalf("prefix comparison = %+v, previous=%+v", current.Comparison, previous.Fingerprint)
	}
}

func TestConfirmedDiscovererUnchangedUsesExactFileMetadata(t *testing.T) {
	t.Parallel()

	home, discoverer := confirmedInspectFixture(t)
	path := filepath.Join(home, "sessions", "unchanged.jsonl")
	if err := os.WriteFile(path, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous, err := discoverer.Inspect(context.Background(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := discoverer.Unchanged(context.Background(), path, previous)
	if err != nil || !unchanged {
		t.Fatalf("Unchanged(exact) = %t, %v", unchanged, err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("grown\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	unchanged, err = discoverer.Unchanged(context.Background(), path, previous)
	if err != nil || unchanged {
		t.Fatalf("Unchanged(grown) = %t, %v", unchanged, err)
	}
}

func confirmedInspectFixture(t *testing.T) (string, *Discoverer) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "sessions", "2026"), 0o700); err != nil {
		t.Fatal(err)
	}
	metadata, err := NewHomeProbe().Probe(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	discoverer, err := NewConfirmedDiscoverer(metadata.Path, metadata.DeviceID, metadata.Inode)
	if err != nil {
		t.Fatal(err)
	}
	return metadata.Path, discoverer
}
