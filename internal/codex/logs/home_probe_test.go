package logs

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

func TestHomeProbeReadsOnlyAllowlistedMetadata(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	canonicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(home) error = %v", err)
	}
	session := writeJSONLFixture(t, home, "sessions/2026/07/session.jsonl", "session-private-marker\n")
	archived := writeJSONLFixture(t, home, "archived_sessions/archive.jsonl", "archive-private-marker\n")
	index := writeJSONLFixture(t, home, "session_index.jsonl", "index-private-marker\n")
	auth := filepath.Join(home, "auth.json")
	if err := os.WriteFile(auth, []byte("auth-private-marker\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(auth) error = %v", err)
	}
	writeJSONLFixture(t, home, "outside.jsonl", "outside-private-marker\n")
	writeJSONLFixture(t, home, "sessions/ignored.txt", "ignored-private-marker\n")

	wantBytes := int64(0)
	before := make(map[string]fileObservation)
	for _, path := range []string{session, archived, index, auth} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("os.Stat(%q) error = %v", path, err)
		}
		before[path] = fileObservation{size: info.Size(), mode: info.Mode(), modTimeNS: info.ModTime().UnixNano()}
		if path != auth {
			wantBytes += info.Size()
		}
		if err := os.Chmod(path, 0); err != nil {
			t.Fatalf("os.Chmod(%q) error = %v", path, err)
		}
		observation := before[path]
		observation.mode = 0
		before[path] = observation
	}
	t.Cleanup(func() {
		for _, path := range []string{session, archived, index, auth} {
			_ = os.Chmod(path, 0o600)
		}
	})

	metadata, err := NewHomeProbe().Probe(context.Background(), home)
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if metadata.Path != canonicalHome || metadata.DeviceID == "" || metadata.Inode <= 0 {
		t.Fatalf("Probe() identity = %#v, want confirmed root identity", metadata)
	}
	if !metadata.SessionsDirectory || !metadata.ArchivedSessionsDirectory ||
		!metadata.SessionIndexFile || !metadata.AuthFile {
		t.Fatalf("Probe() structure = %#v, want all allowlisted entries", metadata)
	}
	if metadata.JSONLFiles != 3 || metadata.JSONLBytes != wantBytes {
		t.Fatalf("Probe() JSONL totals = %d/%d, want 3/%d", metadata.JSONLFiles, metadata.JSONLBytes, wantBytes)
	}
	for path, want := range before {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("os.Stat(after %q) error = %v", path, statErr)
		}
		got := fileObservation{size: info.Size(), mode: info.Mode(), modTimeNS: info.ModTime().UnixNano()}
		if got != want {
			t.Fatalf("file observation after probe for %q = %#v, want %#v", path, got, want)
		}
	}
}

func TestOpenAbsoluteDirectoryNoFollow(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	canonicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks() error = %v", err)
	}
	fileDescriptor, err := openAbsoluteDirectoryNoFollow(canonicalHome)
	if err != nil {
		t.Fatalf("openAbsoluteDirectoryNoFollow(%q) error = %T %v", canonicalHome, err, err)
	}
	if err := unix.Close(fileDescriptor); err != nil {
		t.Fatalf("unix.Close() error = %v", err)
	}
}

func TestHomeProbeTreatsMissingAllowlistedEntriesAsEmpty(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	canonicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks(home) error = %v", err)
	}
	metadata, err := NewHomeProbe().Probe(context.Background(), home)
	if err != nil {
		t.Fatalf("Probe(empty) error = %v", err)
	}
	if metadata.Path != canonicalHome || metadata.DeviceID == "" || metadata.Inode <= 0 ||
		metadata.SessionsDirectory || metadata.ArchivedSessionsDirectory ||
		metadata.SessionIndexFile || metadata.AuthFile ||
		metadata.JSONLFiles != 0 || metadata.JSONLBytes != 0 {
		t.Fatalf("Probe(empty) = %#v, want confirmed empty home", metadata)
	}
}

func TestHomeProbeRejectsUnsafeAndUnsupportedEntries(t *testing.T) {
	t.Parallel()

	t.Run("relative home", func(t *testing.T) {
		if _, err := NewHomeProbe().Probe(context.Background(), "relative/home"); !errors.Is(err, ErrInvalidHome) {
			t.Fatalf("Probe(relative) error = %v, want ErrInvalidHome", err)
		}
	})

	t.Run("nested symlink", func(t *testing.T) {
		home := t.TempDir()
		outside := t.TempDir()
		writeJSONLFixture(t, outside, "outside.jsonl", "outside-private-marker\n")
		if err := os.MkdirAll(filepath.Join(home, "sessions"), 0o700); err != nil {
			t.Fatalf("os.MkdirAll(sessions) error = %v", err)
		}
		if err := os.Symlink(outside, filepath.Join(home, "sessions", "escape")); err != nil {
			t.Fatalf("os.Symlink() error = %v", err)
		}
		if _, err := NewHomeProbe().Probe(context.Background(), home); !errors.Is(err, ErrUnsafeSource) {
			t.Fatalf("Probe(nested symlink) error = %v, want ErrUnsafeSource", err)
		}
	})

	t.Run("named pipe", func(t *testing.T) {
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, "sessions"), 0o700); err != nil {
			t.Fatalf("os.MkdirAll(sessions) error = %v", err)
		}
		if err := unix.Mkfifo(filepath.Join(home, "sessions", "pipe.jsonl"), 0o600); err != nil {
			t.Fatalf("unix.Mkfifo() error = %v", err)
		}
		if _, err := NewHomeProbe().Probe(context.Background(), home); !errors.Is(err, ErrUnsupportedFile) {
			t.Fatalf("Probe(named pipe) error = %v, want ErrUnsupportedFile", err)
		}
	})

	t.Run("directory disguised as JSONL", func(t *testing.T) {
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, "sessions", "directory.jsonl"), 0o700); err != nil {
			t.Fatalf("os.MkdirAll(disguised directory) error = %v", err)
		}
		if _, err := NewHomeProbe().Probe(context.Background(), home); !errors.Is(err, ErrUnsupportedFile) {
			t.Fatalf("Probe(disguised directory) error = %v, want ErrUnsupportedFile", err)
		}
	})

	t.Run("sessions entry is a regular file", func(t *testing.T) {
		home := t.TempDir()
		if err := os.WriteFile(filepath.Join(home, "sessions"), []byte("not-a-directory"), 0o600); err != nil {
			t.Fatalf("os.WriteFile(sessions) error = %v", err)
		}
		if _, err := NewHomeProbe().Probe(context.Background(), home); !errors.Is(err, ErrUnsupportedFile) {
			t.Fatalf("Probe(sessions file) error = %v, want ErrUnsupportedFile", err)
		}
	})
}

func TestAddJSONLMetadataRejectsOverflow(t *testing.T) {
	t.Parallel()

	for name, metadata := range map[string]HomeMetadata{
		"file count": {JSONLFiles: math.MaxInt64},
		"byte count": {JSONLBytes: math.MaxInt64},
	} {
		t.Run(name, func(t *testing.T) {
			if err := addJSONLMetadata(&metadata, 1); !errors.Is(err, ErrUnsupportedFile) {
				t.Fatalf("addJSONLMetadata() error = %v, want ErrUnsupportedFile", err)
			}
		})
	}
	if err := addJSONLMetadata(nil, 1); !errors.Is(err, ErrUnsupportedFile) {
		t.Fatalf("addJSONLMetadata(nil) error = %v, want ErrUnsupportedFile", err)
	}
	if err := addJSONLMetadata(&HomeMetadata{}, -1); !errors.Is(err, ErrUnsupportedFile) {
		t.Fatalf("addJSONLMetadata(negative) error = %v, want ErrUnsupportedFile", err)
	}
}

func TestHomeProbeHonorsCancellationWithoutMetadata(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	metadata, err := NewHomeProbe().Probe(ctx, t.TempDir())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Probe(canceled) error = %v, want context.Canceled", err)
	}
	if metadata != (HomeMetadata{}) {
		t.Fatalf("Probe(canceled) = %#v, want zero metadata", metadata)
	}
}

func TestHomeProbeRejectsAncestorReplacementBetweenCanonicalizeAndOpen(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ancestor := filepath.Join(root, "ancestor")
	home := filepath.Join(ancestor, "home")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(home) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(outside, "home"), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(outside home) error = %v", err)
	}
	filesystem := &ancestorReplaceFileSystem{
		fileSystem: osFileSystem{}, ancestor: ancestor,
		original: filepath.Join(root, "original"), outside: outside,
	}
	if _, err := newHomeProbe(filesystem).Probe(context.Background(), home); !errors.Is(err, ErrUnsafeHome) {
		t.Fatalf("Probe(ancestor replacement) error = %v, want ErrUnsafeHome", err)
	}
}

func TestHomeProbeRejectsRealDirectoryReplacementBetweenCanonicalizeAndOpen(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ancestor := filepath.Join(root, "ancestor")
	home := filepath.Join(ancestor, "home")
	replacement := filepath.Join(root, "replacement")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(home) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(replacement, "home"), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(replacement home) error = %v", err)
	}
	filesystem := &realDirectoryReplaceFileSystem{
		fileSystem: osFileSystem{}, ancestor: ancestor,
		original: filepath.Join(root, "original"), replacement: replacement,
	}
	probe := newHomeProbe(filesystem)
	for attempt := 0; attempt < 2; attempt++ {
		metadata, err := probe.Probe(context.Background(), home)
		if !errors.Is(err, ErrHomeChanged) {
			t.Fatalf("Probe(real directory replacement, attempt %d) = %#v, %v, want ErrHomeChanged",
				attempt, metadata, err)
		}
		if metadata != (HomeMetadata{}) {
			t.Fatalf("Probe(real directory replacement, attempt %d) metadata = %#v, want zero",
				attempt, metadata)
		}
	}
}

func TestHomeProbeRejectsDeterministicDirectoryRaces(t *testing.T) {
	t.Parallel()

	t.Run("same-name directory replacement", func(t *testing.T) {
		home := t.TempDir()
		writeJSONLFixture(t, home, "sessions/nested/original.jsonl", "original\n")
		writeJSONLFixture(t, home, "replacement/replaced.jsonl", "replaced\n")
		filesystem := &homeDirectoryReplaceFileSystem{fileSystem: osFileSystem{}, home: home}
		if _, err := newHomeProbe(filesystem).Probe(context.Background(), home); !errors.Is(err, ErrChangedDuringScan) {
			t.Fatalf("Probe(directory replacement) error = %v, want ErrChangedDuringScan", err)
		}
	})

	t.Run("entry added after enumeration", func(t *testing.T) {
		home := t.TempDir()
		writeJSONLFixture(t, home, "sessions/original.jsonl", "original\n")
		outside := t.TempDir()
		filesystem := &homeEntryAddFileSystem{
			fileSystem: osFileSystem{}, home: home, outside: outside,
		}
		if _, err := newHomeProbe(filesystem).Probe(context.Background(), home); !errors.Is(err, ErrChangedDuringScan) {
			t.Fatalf("Probe(entry added) error = %v, want ErrChangedDuringScan", err)
		}
	})

	t.Run("child removed after enumeration", func(t *testing.T) {
		home := t.TempDir()
		writeJSONLFixture(t, home, "sessions/nested/original.jsonl", "original\n")
		filesystem := &directoryDisappearFileSystem{fileSystem: osFileSystem{}, home: home}
		if _, err := newHomeProbe(filesystem).Probe(context.Background(), home); !errors.Is(err, ErrChangedDuringScan) {
			t.Fatalf("Probe(child removed) error = %v, want ErrChangedDuringScan", err)
		}
	})
}

type fileObservation struct {
	size      int64
	mode      os.FileMode
	modTimeNS int64
}

type ancestorReplaceFileSystem struct {
	fileSystem
	ancestor string
	original string
	outside  string
	once     sync.Once
}

type realDirectoryReplaceFileSystem struct {
	fileSystem
	ancestor    string
	original    string
	replacement string
	mu          sync.Mutex
}

func (filesystem *realDirectoryReplaceFileSystem) ConfirmRoot(
	path string,
	resolveAncestors bool,
) (rootIdentity, error) {
	filesystem.mu.Lock()
	defer filesystem.mu.Unlock()
	if err := filesystem.swapInReplacement(); err != nil {
		return rootIdentity{}, err
	}
	identity, openErr := filesystem.fileSystem.ConfirmRoot(path, resolveAncestors)
	restoreErr := filesystem.restoreOriginal()
	if openErr != nil {
		return rootIdentity{}, openErr
	}
	if restoreErr != nil {
		return rootIdentity{}, restoreErr
	}
	return identity, nil
}

func (filesystem *realDirectoryReplaceFileSystem) OpenRoot(
	path string,
	resolveAncestors bool,
) (scanRoot, error) {
	filesystem.mu.Lock()
	defer filesystem.mu.Unlock()
	if err := filesystem.swapInReplacement(); err != nil {
		return nil, err
	}
	root, openErr := filesystem.fileSystem.OpenRoot(path, resolveAncestors)
	restoreErr := filesystem.restoreOriginal()
	if openErr != nil {
		return nil, openErr
	}
	if restoreErr != nil {
		_ = root.Close()
		return nil, restoreErr
	}
	return root, nil
}

func (filesystem *realDirectoryReplaceFileSystem) swapInReplacement() error {
	if err := os.Rename(filesystem.ancestor, filesystem.original); err != nil {
		return err
	}
	if err := os.Rename(filesystem.replacement, filesystem.ancestor); err != nil {
		_ = os.Rename(filesystem.original, filesystem.ancestor)
		return err
	}
	return nil
}

func (filesystem *realDirectoryReplaceFileSystem) restoreOriginal() error {
	if err := os.Rename(filesystem.ancestor, filesystem.replacement); err != nil {
		return err
	}
	return os.Rename(filesystem.original, filesystem.ancestor)
}

func (filesystem *ancestorReplaceFileSystem) ConfirmRoot(
	path string,
	resolveAncestors bool,
) (rootIdentity, error) {
	var mutationErr error
	filesystem.once.Do(func() {
		if err := os.Rename(filesystem.ancestor, filesystem.original); err != nil {
			mutationErr = err
			return
		}
		mutationErr = os.Symlink(filesystem.outside, filesystem.ancestor)
	})
	if mutationErr != nil {
		return rootIdentity{}, mutationErr
	}
	return filesystem.fileSystem.ConfirmRoot(path, resolveAncestors)
}

type homeDirectoryReplaceFileSystem struct {
	fileSystem
	home string
}

func (filesystem *homeDirectoryReplaceFileSystem) OpenRoot(
	path string,
	resolveAncestors bool,
) (scanRoot, error) {
	root, err := filesystem.fileSystem.OpenRoot(path, resolveAncestors)
	if err != nil {
		return nil, err
	}
	return &homeDirectoryReplaceRoot{scanRoot: root, home: filesystem.home}, nil
}

type homeDirectoryReplaceRoot struct {
	scanRoot
	home string
	once sync.Once
}

func (root *homeDirectoryReplaceRoot) ReadDir(relativePath string) ([]fs.DirEntry, error) {
	entries, err := root.scanRoot.ReadDir(relativePath)
	if err != nil {
		return nil, err
	}
	if relativePath == "sessions" {
		var mutationErr error
		root.once.Do(func() {
			mutationErr = os.Rename(
				filepath.Join(root.home, "sessions", "nested"),
				filepath.Join(root.home, "original-nested"),
			)
			if mutationErr == nil {
				mutationErr = os.Rename(
					filepath.Join(root.home, "replacement"),
					filepath.Join(root.home, "sessions", "nested"),
				)
			}
		})
		if mutationErr != nil {
			return nil, mutationErr
		}
	}
	return entries, nil
}

type homeEntryAddFileSystem struct {
	fileSystem
	home    string
	outside string
}

func (filesystem *homeEntryAddFileSystem) OpenRoot(
	path string,
	resolveAncestors bool,
) (scanRoot, error) {
	root, err := filesystem.fileSystem.OpenRoot(path, resolveAncestors)
	if err != nil {
		return nil, err
	}
	return &homeEntryAddRoot{
		scanRoot: root, home: filesystem.home, outside: filesystem.outside,
	}, nil
}

type homeEntryAddRoot struct {
	scanRoot
	home    string
	outside string
	once    sync.Once
}

func (root *homeEntryAddRoot) ReadDir(relativePath string) ([]fs.DirEntry, error) {
	entries, err := root.scanRoot.ReadDir(relativePath)
	if err != nil {
		return nil, err
	}
	if relativePath == "sessions" {
		var mutationErr error
		root.once.Do(func() {
			mutationErr = os.Symlink(root.outside, filepath.Join(root.home, "sessions", "late"))
		})
		if mutationErr != nil {
			return nil, mutationErr
		}
	}
	return entries, nil
}
