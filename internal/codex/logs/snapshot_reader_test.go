package logs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotReaderReadsExactDiscoveredBytesInChunks(t *testing.T) {
	t.Parallel()

	home, snapshot, content := snapshotReaderFixture(t)
	reader, err := NewSnapshotReader(home, 5)
	if err != nil {
		t.Fatalf("NewSnapshotReader() error = %v", err)
	}
	var got []byte
	var eofCalls int
	readOffset, err := reader.Read(context.Background(), snapshot, 0, func(chunk []byte, eof bool) error {
		got = append(got, chunk...)
		if eof {
			eofCalls++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if readOffset != int64(len(content)) || !bytes.Equal(got, content) || eofCalls != 1 {
		t.Fatalf("Read() offset=%d content=%q eof=%d, want %d %q 1", readOffset, got, eofCalls, len(content), content)
	}

	got = nil
	readOffset, err = reader.Read(context.Background(), snapshot, 5, func(chunk []byte, _ bool) error {
		got = append(got, chunk...)
		return nil
	})
	if err != nil || readOffset != int64(len(content)) || !bytes.Equal(got, content[5:]) {
		t.Fatalf("Read(resume) offset=%d content=%q err=%v", readOffset, got, err)
	}
}

func TestSnapshotReaderRejectsSymlinkAndReportsMidReadDrift(t *testing.T) {
	t.Parallel()

	t.Run("symlink replacement", func(t *testing.T) {
		home, snapshot, _ := snapshotReaderFixture(t)
		outside := filepath.Join(t.TempDir(), "outside.jsonl")
		if err := os.WriteFile(outside, []byte("outside\n"), 0o600); err != nil {
			t.Fatalf("WriteFile(outside) error = %v", err)
		}
		if err := os.Remove(snapshot.Path); err != nil {
			t.Fatalf("Remove(source) error = %v", err)
		}
		if err := os.Symlink(outside, snapshot.Path); err != nil {
			t.Fatalf("Symlink() error = %v", err)
		}
		reader, err := NewSnapshotReader(home, 4)
		if err != nil {
			t.Fatalf("NewSnapshotReader() error = %v", err)
		}
		if _, err := reader.Read(context.Background(), snapshot, 0, func([]byte, bool) error { return nil }); !errors.Is(err, ErrUnsafeSource) {
			t.Fatalf("Read(symlink) error = %v, want ErrUnsafeSource", err)
		}
	})

	t.Run("append during read", func(t *testing.T) {
		home, snapshot, _ := snapshotReaderFixture(t)
		reader, err := NewSnapshotReader(home, 4)
		if err != nil {
			t.Fatalf("NewSnapshotReader() error = %v", err)
		}
		calls := 0
		_, err = reader.Read(context.Background(), snapshot, 0, func([]byte, bool) error {
			calls++
			if calls == 1 {
				file, openErr := os.OpenFile(snapshot.Path, os.O_APPEND|os.O_WRONLY, 0)
				if openErr != nil {
					return openErr
				}
				_, writeErr := file.WriteString("drift\n")
				closeErr := file.Close()
				return errors.Join(writeErr, closeErr)
			}
			return nil
		})
		if !errors.Is(err, ErrChangedDuringScan) {
			t.Fatalf("Read(drift) error = %v, want ErrChangedDuringScan", err)
		}
	})
}

func TestSnapshotReaderHonorsCancellationBeforeReading(t *testing.T) {
	t.Parallel()

	home, snapshot, _ := snapshotReaderFixture(t)
	reader, err := NewSnapshotReader(home, 4)
	if err != nil {
		t.Fatalf("NewSnapshotReader() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.Read(ctx, snapshot, 0, func([]byte, bool) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("Read(cancelled) error = %v, want context.Canceled", err)
	}
}

func snapshotReaderFixture(t *testing.T) (string, Snapshot, []byte) {
	t.Helper()
	home := t.TempDir()
	sessions := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := []byte("first\nsecond\nthird\n")
	path := filepath.Join(sessions, "fixture.jsonl")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	discoverer, err := NewDiscoverer(home)
	if err != nil {
		t.Fatalf("NewDiscoverer() error = %v", err)
	}
	result, err := discoverer.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(result.Snapshots) != 1 {
		t.Fatalf("len(Snapshots) = %d, want 1", len(result.Snapshots))
	}
	return home, result.Snapshots[0], content
}
