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

	t.Run("removed before open", func(t *testing.T) {
		home, snapshot, _ := snapshotReaderFixture(t)
		if err := os.Remove(snapshot.Path); err != nil {
			t.Fatalf("Remove(source) error = %v", err)
		}
		reader, err := NewSnapshotReader(home, 4)
		if err != nil {
			t.Fatalf("NewSnapshotReader() error = %v", err)
		}
		if _, err := reader.Read(context.Background(), snapshot, 0, func([]byte, bool) error { return nil }); !errors.Is(err, ErrChangedDuringScan) {
			t.Fatalf("Read(removed) error = %v, want ErrChangedDuringScan", err)
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

func TestSnapshotReaderReadLimitedNeverReadsPastByteBudget(t *testing.T) {
	t.Parallel()

	home, snapshot, content := snapshotReaderSizedFixture(t, int(PrefixLimitBytes+20))
	reader, err := NewSnapshotReader(home, 5)
	if err != nil {
		t.Fatalf("NewSnapshotReader() error = %v", err)
	}
	var got []byte
	var eofCalls int
	result, err := reader.ReadLimited(
		context.Background(), snapshot, snapshot.Fingerprint.PrefixBytes,
		snapshot.Fingerprint.PrefixBytes+7,
		func(chunk []byte, eof bool) error {
			got = append(got, chunk...)
			if eof {
				eofCalls++
			}
			return nil
		},
	)
	if err != nil || result.Offset != snapshot.Fingerprint.PrefixBytes+7 ||
		result.BytesRead != snapshot.Fingerprint.PrefixBytes+7 || result.ContentBytes != 7 || result.EOF ||
		!bytes.Equal(got, content[PrefixLimitBytes:PrefixLimitBytes+7]) || eofCalls != 0 {
		t.Fatalf("ReadLimited() = %#v content=%q eofCalls=%d err=%v", result, got, eofCalls, err)
	}

	got = nil
	result, err = reader.ReadLimited(
		context.Background(), snapshot, snapshot.Fingerprint.PrefixBytes+7,
		snapshot.Fingerprint.PrefixBytes+int64(len(content))-snapshot.Fingerprint.PrefixBytes-7,
		func(chunk []byte, eof bool) error {
			got = append(got, chunk...)
			if eof {
				eofCalls++
			}
			return nil
		},
	)
	if err != nil || result.Offset != int64(len(content)) ||
		result.BytesRead != snapshot.Fingerprint.PrefixBytes+13 || result.ContentBytes != 13 || !result.EOF ||
		!bytes.Equal(got, content[PrefixLimitBytes+7:]) || eofCalls != 1 {
		t.Fatalf("ReadLimited(resume) = %#v content=%q eofCalls=%d err=%v", result, got, eofCalls, err)
	}
}

func TestSnapshotReaderReadLimitedValidatesBudgetAndExactEOF(t *testing.T) {
	t.Parallel()

	home, snapshot, content := snapshotReaderFixture(t)
	reader, err := NewSnapshotReader(home, 4)
	if err != nil {
		t.Fatalf("NewSnapshotReader() error = %v", err)
	}
	for _, limit := range []int64{0, -1} {
		calls := 0
		if _, err := reader.ReadLimited(
			context.Background(), snapshot, 0, limit,
			func([]byte, bool) error { calls++; return nil },
		); !errors.Is(err, ErrInvalidSnapshot) || calls != 0 {
			t.Fatalf("ReadLimited(limit=%d) error=%v calls=%d", limit, err, calls)
		}
	}
	eofCalls := 0
	result, err := reader.ReadLimited(
		context.Background(), snapshot, int64(len(content)), snapshot.Fingerprint.PrefixBytes,
		func(chunk []byte, eof bool) error {
			if len(chunk) != 0 || !eof {
				t.Fatalf("consume(chunk=%q, eof=%t)", chunk, eof)
			}
			eofCalls++
			return nil
		},
	)
	if err != nil || result.Offset != int64(len(content)) ||
		result.BytesRead != snapshot.Fingerprint.PrefixBytes || result.ContentBytes != 0 ||
		!result.EOF || eofCalls != 1 {
		t.Fatalf("ReadLimited(exact EOF) = %#v eofCalls=%d err=%v", result, eofCalls, err)
	}
}

func TestSnapshotReaderReadLimitedHonorsMidReadCancellation(t *testing.T) {
	t.Parallel()

	home, snapshot, _ := snapshotReaderFixture(t)
	reader, err := NewSnapshotReader(home, 4)
	if err != nil {
		t.Fatalf("NewSnapshotReader() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	result, err := reader.ReadLimited(ctx, snapshot, 0, snapshot.Fingerprint.PrefixBytes, func([]byte, bool) error {
		calls++
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || result.Offset != 4 ||
		result.BytesRead != snapshot.Fingerprint.PrefixBytes || result.ContentBytes != 4 ||
		result.EOF || calls != 1 {
		t.Fatalf("ReadLimited(cancel) = %#v calls=%d err=%v", result, calls, err)
	}
}

func TestSnapshotReaderReadLimitedCountsPrefixProofAgainstPhysicalIOBudget(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	sessions := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := bytes.Repeat([]byte("x"), int(PrefixLimitBytes+904))
	path := filepath.Join(sessions, "large.jsonl")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	discoverer, err := NewDiscoverer(home)
	if err != nil {
		t.Fatalf("NewDiscoverer() error = %v", err)
	}
	discovery, err := discoverer.Discover(context.Background())
	if err != nil || len(discovery.Snapshots) != 1 {
		t.Fatalf("Discover() = %#v, %v", discovery, err)
	}
	snapshot := discovery.Snapshots[0]
	reader, err := NewSnapshotReader(home, 64)
	if err != nil {
		t.Fatalf("NewSnapshotReader() error = %v", err)
	}
	readAt := reader.readAt
	var physicalBytes int64
	reader.readAt = func(file *os.File, buffer []byte, offset int64) (int, error) {
		count, readErr := readAt(file, buffer, offset)
		physicalBytes += int64(count)
		return count, readErr
	}
	budget := snapshot.Fingerprint.PrefixBytes + 7
	var got []byte
	result, err := reader.ReadLimited(
		context.Background(), snapshot, snapshot.Fingerprint.PrefixBytes, budget,
		func(chunk []byte, _ bool) error {
			got = append(got, chunk...)
			return nil
		},
	)
	if err != nil || physicalBytes != budget || result.BytesRead != budget || result.ContentBytes != 7 ||
		result.Offset != snapshot.Fingerprint.PrefixBytes+7 ||
		!bytes.Equal(got, content[PrefixLimitBytes:PrefixLimitBytes+7]) {
		t.Fatalf("ReadLimited() = %#v physical=%d content=%q err=%v", result, physicalBytes, got, err)
	}

	physicalBytes = 0
	if _, err := reader.ReadLimited(
		context.Background(), snapshot, 0, snapshot.Fingerprint.PrefixBytes-1,
		func([]byte, bool) error { return nil },
	); !errors.Is(err, ErrSnapshotBudgetTooSmall) || physicalBytes != 0 {
		t.Fatalf("ReadLimited(short proof budget) error=%v physical=%d", err, physicalBytes)
	}
}

func TestSnapshotReaderControlledStopStillValidatesAfterStat(t *testing.T) {
	t.Parallel()

	stopCause := errors.New("time budget")
	t.Run("stable snapshot returns controlled cause", func(t *testing.T) {
		home, snapshot, _ := snapshotReaderFixture(t)
		reader, err := NewSnapshotReader(home, 4)
		if err != nil {
			t.Fatalf("NewSnapshotReader() error = %v", err)
		}
		result, err := reader.ReadLimited(
			context.Background(), snapshot, 0, snapshot.Fingerprint.PrefixBytes,
			func([]byte, bool) error { return StopSnapshotRead(stopCause) },
		)
		if !errors.Is(err, stopCause) || errors.Is(err, ErrChangedDuringScan) ||
			result.Offset != 4 || result.ContentBytes != 4 || result.EOF {
			t.Fatalf("ReadLimited(controlled stop) = %#v, %v", result, err)
		}
	})

	t.Run("drift overrides controlled stop", func(t *testing.T) {
		home, snapshot, _ := snapshotReaderFixture(t)
		reader, err := NewSnapshotReader(home, 4)
		if err != nil {
			t.Fatalf("NewSnapshotReader() error = %v", err)
		}
		result, err := reader.ReadLimited(
			context.Background(), snapshot, 0, snapshot.Fingerprint.PrefixBytes,
			func([]byte, bool) error {
				file, openErr := os.OpenFile(snapshot.Path, os.O_APPEND|os.O_WRONLY, 0)
				if openErr != nil {
					return openErr
				}
				_, writeErr := file.WriteString("drift\n")
				closeErr := file.Close()
				if err := errors.Join(writeErr, closeErr); err != nil {
					return err
				}
				return StopSnapshotRead(stopCause)
			},
		)
		if !errors.Is(err, ErrChangedDuringScan) || errors.Is(err, stopCause) ||
			result.Offset != 4 || result.ContentBytes != 4 {
			t.Fatalf("ReadLimited(controlled stop with drift) = %#v, %v", result, err)
		}
	})
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

func snapshotReaderSizedFixture(t *testing.T, size int) (string, Snapshot, []byte) {
	t.Helper()
	home := t.TempDir()
	sessions := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := bytes.Repeat([]byte("x"), size)
	path := filepath.Join(sessions, "sized.jsonl")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	discoverer, err := NewDiscoverer(home)
	if err != nil {
		t.Fatalf("NewDiscoverer() error = %v", err)
	}
	result, err := discoverer.Discover(context.Background())
	if err != nil || len(result.Snapshots) != 1 {
		t.Fatalf("Discover() = %#v, %v", result, err)
	}
	return home, result.Snapshots[0], content
}
