package index

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

var errDirectorySyncInjected = errors.New("directory sync injected failure")

func TestIndexFileBacksUpAndAppendsWithoutTouchingSessionJSONL(t *testing.T) {
	t.Parallel()

	home := newPrivateDirectory(t, "codex-home")
	sessionDirectory := filepath.Join(home, "sessions", "2026", "07", "14")
	if err := os.MkdirAll(sessionDirectory, 0o700); err != nil {
		t.Fatalf("MkdirAll(session directory): %v", err)
	}
	sessionPath := filepath.Join(sessionDirectory, "rollout-a.jsonl")
	writePrivateFile(t, sessionPath, []byte("session-private-bytes\n"))
	sessionBefore := fileState(t, sessionPath)

	indexPath := filepath.Join(home, sessionIndexFilename)
	original := []byte(`{"id":"019d4b69-25d1-7be2-b5f4-8c4234ed5dee","thread_name":"first","updated_at":"2026-04-01T00:00:00Z"}` + "\n")
	writePrivateFile(t, indexPath, original)
	indexFile, err := OpenIndexFile(home)
	if err != nil {
		t.Fatalf("OpenIndexFile() error = %v", err)
	}

	read, err := indexFile.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(read.Content) != string(original) || !read.Version.Exists || read.Version.SHA256 == "" {
		t.Fatalf("Read() = %#v", read)
	}
	backupDirectory := newPrivateDirectory(t, "backups")
	backupPath := filepath.Join(backupDirectory, "session_index.before.jsonl")
	report, err := indexFile.Backup(context.Background(), read.Version, backupPath)
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	if !report.SourceExisted || report.Path != backupPath || string(readFile(t, backupPath)) != string(original) {
		t.Fatalf("Backup() report = %#v", report)
	}
	if got := fileMode(t, backupPath); got != 0o600 {
		t.Fatalf("backup mode = %#o, want 0600", got)
	}

	appended := Entry{
		ID: "019d4b69-25d1-7be2-b5f4-8c4234ed5def", ThreadName: "second",
		UpdatedAt: "2026-04-02T00:00:00Z",
	}
	result, err := indexFile.Append(context.Background(), read.Version, []Entry{appended})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if !result.Exists || result.SizeBytes <= read.Version.SizeBytes || result.SHA256 == read.Version.SHA256 {
		t.Fatalf("Append() version = %#v", result)
	}
	parsed, err := Parse(readFile(t, indexPath))
	if err != nil {
		t.Fatalf("Parse(appended) error = %v", err)
	}
	latest, found := parsed.Latest(appended.ID)
	if !found || latest.ThreadName != appended.ThreadName {
		t.Fatalf("latest appended entry = %#v, %v", latest, found)
	}
	if got := fileState(t, sessionPath); got != sessionBefore {
		t.Fatalf("session JSONL changed: before=%#v after=%#v", sessionBefore, got)
	}
}

func TestIndexFileRejectsSymlinkAndExpectedVersionDrift(t *testing.T) {
	t.Parallel()

	t.Run("symlink", func(t *testing.T) {
		home := newPrivateDirectory(t, "codex-home")
		target := filepath.Join(t.TempDir(), "external.jsonl")
		writePrivateFile(t, target, []byte("external\n"))
		if err := os.Symlink(target, filepath.Join(home, sessionIndexFilename)); err != nil {
			t.Fatalf("Symlink(): %v", err)
		}
		indexFile, err := OpenIndexFile(home)
		if err != nil {
			t.Fatalf("OpenIndexFile() error = %v", err)
		}
		if _, err := indexFile.Read(context.Background()); !errors.Is(err, ErrUnsafeIndex) {
			t.Fatalf("Read(symlink) error = %v, want ErrUnsafeIndex", err)
		}
	})

	t.Run("drift", func(t *testing.T) {
		home := newPrivateDirectory(t, "codex-home")
		indexPath := filepath.Join(home, sessionIndexFilename)
		writePrivateFile(t, indexPath, []byte("before\n"))
		indexFile, err := OpenIndexFile(home)
		if err != nil {
			t.Fatalf("OpenIndexFile() error = %v", err)
		}
		read, err := indexFile.Read(context.Background())
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		if err := os.WriteFile(indexPath, []byte("after\n"), 0o600); err != nil {
			t.Fatalf("WriteFile(drift): %v", err)
		}
		backupPath := filepath.Join(newPrivateDirectory(t, "backups"), "before.jsonl")
		if _, err := indexFile.Backup(context.Background(), read.Version, backupPath); !errors.Is(err, ErrPlanDrift) {
			t.Fatalf("Backup(drift) error = %v, want ErrPlanDrift", err)
		}
		if _, err := os.Lstat(backupPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("backup exists after drift: %v", err)
		}
	})
}

func TestIndexFileRepresentsMissingBackupAndCreatesPrivateIndex(t *testing.T) {
	t.Parallel()

	home := newPrivateDirectory(t, "codex-home")
	indexFile, err := OpenIndexFile(home)
	if err != nil {
		t.Fatalf("OpenIndexFile() error = %v", err)
	}
	read, err := indexFile.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if read.Version.Exists || len(read.Content) != 0 {
		t.Fatalf("missing Read() = %#v", read)
	}
	markerPath := filepath.Join(newPrivateDirectory(t, "backups"), "session_index.absent")
	report, err := indexFile.Backup(context.Background(), read.Version, markerPath)
	if err != nil {
		t.Fatalf("Backup(missing) error = %v", err)
	}
	if report.SourceExisted || len(readFile(t, markerPath)) != 0 || fileMode(t, markerPath) != 0o600 {
		t.Fatalf("missing backup report = %#v", report)
	}
	entry := Entry{
		ID: "019d4b69-25d1-7be2-b5f4-8c4234ed5dee", ThreadName: "created",
		UpdatedAt: "2026-04-01T00:00:00Z",
	}
	if _, err := indexFile.Append(context.Background(), read.Version, []Entry{entry}); err != nil {
		t.Fatalf("Append(missing) error = %v", err)
	}
	indexPath := filepath.Join(home, sessionIndexFilename)
	if got := fileMode(t, indexPath); got != 0o600 {
		t.Fatalf("created index mode = %#o, want 0600", got)
	}
}

func TestIndexFileAddsSeparatorBeforeAppendingToUnterminatedIndex(t *testing.T) {
	t.Parallel()

	home := newPrivateDirectory(t, "codex-home")
	indexPath := filepath.Join(home, sessionIndexFilename)
	writePrivateFile(t, indexPath, []byte(
		`{"id":"019d4b69-25d1-7be2-b5f4-8c4234ed5dee","thread_name":"first","updated_at":"2026-04-01T00:00:00Z"}`,
	))
	indexFile, err := OpenIndexFile(home)
	if err != nil {
		t.Fatalf("OpenIndexFile() error = %v", err)
	}
	read, err := indexFile.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	appended := Entry{
		ID: "019d4b69-25d1-7be2-b5f4-8c4234ed5def", ThreadName: "second",
		UpdatedAt: "2026-04-02T00:00:00Z",
	}
	if _, err := indexFile.Append(context.Background(), read.Version, []Entry{appended}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	parsed, err := Parse(readFile(t, indexPath))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	latest, found := parsed.Latest(appended.ID)
	if !found || latest.ThreadName != appended.ThreadName {
		t.Fatalf("latest appended entry = %#v, %v", latest, found)
	}
}

func TestValidateAppendSizeRejectsOverflowBeforeWrite(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		currentSize int64
		payloadSize int
	}{
		{name: "existing-at-limit", currentSize: maxIndexBytes, payloadSize: 1},
		{name: "payload-over-limit", currentSize: 0, payloadSize: maxIndexBytes + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateAppendSize(test.currentSize, test.payloadSize); !errors.Is(err, ErrIndexTooLarge) {
				t.Fatalf("validateAppendSize() error = %v, want ErrIndexTooLarge", err)
			}
		})
	}
}

func TestIndexFileDetectsConcurrentAppendAfterFinalCheck(t *testing.T) {
	t.Parallel()

	home := newPrivateDirectory(t, "codex-home")
	indexPath := filepath.Join(home, sessionIndexFilename)
	writePrivateFile(t, indexPath, []byte(
		`{"id":"019d4b69-25d1-7be2-b5f4-8c4234ed5dee","thread_name":"first","updated_at":"2026-04-01T00:00:00Z"}`+"\n",
	))
	indexFile, err := OpenIndexFile(home)
	if err != nil {
		t.Fatalf("OpenIndexFile() error = %v", err)
	}
	read, err := indexFile.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	concurrent := []byte(
		`{"id":"019d4b69-25d1-7be2-b5f4-8c4234ed5def","thread_name":"concurrent","updated_at":"2026-04-03T00:00:00Z"}` + "\n",
	)
	indexFile.afterFinalCheck = func() {
		file, openErr := os.OpenFile(indexPath, os.O_APPEND|os.O_WRONLY, 0)
		if openErr != nil {
			t.Fatalf("OpenFile(concurrent): %v", openErr)
		}
		if _, writeErr := file.Write(concurrent); writeErr != nil {
			t.Fatalf("Write(concurrent): %v", writeErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			t.Fatalf("Close(concurrent): %v", closeErr)
		}
	}
	_, err = indexFile.Append(context.Background(), read.Version, []Entry{{
		ID: "019d4b69-25d1-7be2-b5f4-8c4234ed5df0", ThreadName: "repair",
		UpdatedAt: "2026-04-02T00:00:00Z",
	}})
	if !errors.Is(err, ErrPlanDrift) {
		t.Fatalf("Append(concurrent) error = %v, want ErrPlanDrift", err)
	}
	if got := readFile(t, indexPath); !bytes.Contains(got, concurrent) {
		t.Fatal("concurrent append was not preserved")
	}
}

func TestIndexBackupAndNewIndexRequireDirectorySync(t *testing.T) {
	t.Parallel()

	t.Run("backup", func(t *testing.T) {
		home := newPrivateDirectory(t, "codex-home")
		indexPath := filepath.Join(home, sessionIndexFilename)
		writePrivateFile(t, indexPath, []byte("existing\n"))
		indexFile, err := OpenIndexFile(home)
		if err != nil {
			t.Fatalf("OpenIndexFile() error = %v", err)
		}
		read, err := indexFile.Read(context.Background())
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		calls := 0
		indexFile.syncDirectory = func(string) error {
			calls++
			return errDirectorySyncInjected
		}
		backupPath := filepath.Join(newPrivateDirectory(t, "backups"), "index.jsonl")
		if _, err := indexFile.Backup(context.Background(), read.Version, backupPath); !errors.Is(err, errDirectorySyncInjected) {
			t.Fatalf("Backup() error = %v, want sync failure", err)
		}
		if calls == 0 {
			t.Fatal("Backup() did not sync parent directory")
		}
		if _, err := os.Lstat(backupPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("backup remained after sync failure: %v", err)
		}
	})

	t.Run("new-index", func(t *testing.T) {
		home := newPrivateDirectory(t, "codex-home")
		indexFile, err := OpenIndexFile(home)
		if err != nil {
			t.Fatalf("OpenIndexFile() error = %v", err)
		}
		read, err := indexFile.Read(context.Background())
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		calls := 0
		indexFile.syncRoot = func(int) error {
			calls++
			return errDirectorySyncInjected
		}
		_, err = indexFile.Append(context.Background(), read.Version, []Entry{{
			ID: "019d4b69-25d1-7be2-b5f4-8c4234ed5dee", ThreadName: "created",
			UpdatedAt: "2026-04-01T00:00:00Z",
		}})
		if !errors.Is(err, errDirectorySyncInjected) {
			t.Fatalf("Append(missing) error = %v, want sync failure", err)
		}
		if calls == 0 {
			t.Fatal("Append(missing) did not sync Codex Home directory")
		}
	})
}

type testFileState struct {
	Content string
	Mode    os.FileMode
	Size    int64
	MTimeNS int64
}

func newPrivateDirectory(t *testing.T, name string) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("MkdirAll(%s): %v", name, err)
	}
	return directory
}

func writePrivateFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return content
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s): %v", path, err)
	}
	return info.Mode().Perm()
}

func fileState(t *testing.T, path string) testFileState {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s): %v", path, err)
	}
	return testFileState{
		Content: string(readFile(t, path)), Mode: info.Mode().Perm(), Size: info.Size(),
		MTimeNS: info.ModTime().UnixNano(),
	}
}
