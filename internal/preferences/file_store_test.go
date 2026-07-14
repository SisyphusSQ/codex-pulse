package preferences

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestFileStoreConfirmLoadPermissionsAndIdempotence(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "private")
	path := filepath.Join(root, "preferences.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	if _, err := store.Load(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Load(missing) error = %v, want ErrNotConfigured", err)
	}
	want := validSnapshot(filepath.Join(t.TempDir(), "codex-home"))
	if err := store.Confirm(context.Background(), want); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	directoryInfo, err := os.Lstat(root)
	if err != nil {
		t.Fatalf("os.Lstat(directory) error = %v", err)
	}
	if !directoryInfo.IsDir() || directoryInfo.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %v, want directory 0700", directoryInfo.Mode())
	}
	fileInfo, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("os.Lstat(file) error = %v", err)
	}
	if !fileInfo.Mode().IsRegular() || fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, want regular 0600", fileInfo.Mode())
	}
	beforeBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(before replay) error = %v", err)
	}
	beforeModTime := fileInfo.ModTime()

	reopened, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore(reopen) error = %v", err)
	}
	got, err := reopened.Load(context.Background())
	if err != nil {
		t.Fatalf("Load(reopen) error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(reopen) = %#v, want %#v", got, want)
	}
	replay := want
	replay.CodexHome.ConfirmedAtMS++
	if err := reopened.Confirm(context.Background(), replay); err != nil {
		t.Fatalf("Confirm(replay) error = %v", err)
	}
	afterBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(after replay) error = %v", err)
	}
	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat(after replay) error = %v", err)
	}
	if string(afterBytes) != string(beforeBytes) || !afterInfo.ModTime().Equal(beforeModTime) {
		t.Fatalf("idempotent replay rewrote preferences: bytes_equal=%v mtime_equal=%v",
			string(afterBytes) == string(beforeBytes), afterInfo.ModTime().Equal(beforeModTime))
	}
}

func TestFileStoreRejectsConflictingConfirmedSource(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "private", "preferences.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	first := validSnapshot(filepath.Join(t.TempDir(), "first-home"))
	if err := store.Confirm(context.Background(), first); err != nil {
		t.Fatalf("Confirm(first) error = %v", err)
	}
	second := validSnapshot(filepath.Join(t.TempDir(), "second-home"))
	if err := store.Confirm(context.Background(), second); !errors.Is(err, ErrAlreadyConfirmed) {
		t.Fatalf("Confirm(conflict) error = %v, want ErrAlreadyConfirmed", err)
	}
	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load(after conflict) error = %v", err)
	}
	if !reflect.DeepEqual(got, first) {
		t.Fatalf("Load(after conflict) = %#v, want first %#v", got, first)
	}
}

func TestFileStoreRejectsUnsafeOrInvalidState(t *testing.T) {
	t.Parallel()

	t.Run("relative path", func(t *testing.T) {
		if _, err := NewFileStore("relative/preferences.json"); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("NewFileStore(relative) error = %v, want ErrUnsafePath", err)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Chmod(root, 0o700); err != nil {
			t.Fatalf("os.Chmod(root) error = %v", err)
		}
		path := filepath.Join(root, "preferences.json")
		if err := os.WriteFile(path, []byte("{invalid\n"), 0o600); err != nil {
			t.Fatalf("os.WriteFile() error = %v", err)
		}
		store, err := NewFileStore(path)
		if err != nil {
			t.Fatalf("NewFileStore() error = %v", err)
		}
		if _, err := store.Load(context.Background()); !errors.Is(err, ErrInvalidPreferences) {
			t.Fatalf("Load(invalid JSON) error = %v, want ErrInvalidPreferences", err)
		}
	})

	t.Run("symlink file", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Chmod(root, 0o700); err != nil {
			t.Fatalf("os.Chmod(root) error = %v", err)
		}
		target := filepath.Join(root, "target.json")
		if err := os.WriteFile(target, []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("os.WriteFile(target) error = %v", err)
		}
		path := filepath.Join(root, "preferences.json")
		if err := os.Symlink(target, path); err != nil {
			t.Fatalf("os.Symlink() error = %v", err)
		}
		store, err := NewFileStore(path)
		if err != nil {
			t.Fatalf("NewFileStore() error = %v", err)
		}
		if _, err := store.Load(context.Background()); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("Load(symlink) error = %v, want ErrUnsafePath", err)
		}
	})

	t.Run("symlink directory", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "target")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatalf("os.Mkdir(target) error = %v", err)
		}
		link := filepath.Join(root, "private")
		if err := os.Symlink(target, link); err != nil {
			t.Fatalf("os.Symlink(directory) error = %v", err)
		}
		store, err := NewFileStore(filepath.Join(link, "preferences.json"))
		if err != nil {
			t.Fatalf("NewFileStore() error = %v", err)
		}
		if err := store.Confirm(context.Background(), validSnapshot(filepath.Join(root, "home"))); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("Confirm(symlink directory) error = %v, want ErrUnsafePath", err)
		}
	})

	t.Run("broad directory permissions", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "private")
		if err := os.Mkdir(root, 0o755); err != nil {
			t.Fatalf("os.Mkdir(root) error = %v", err)
		}
		store, err := NewFileStore(filepath.Join(root, "preferences.json"))
		if err != nil {
			t.Fatalf("NewFileStore() error = %v", err)
		}
		if err := store.Confirm(context.Background(), validSnapshot(filepath.Join(root, "home"))); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("Confirm(broad directory) error = %v, want ErrUnsafePath", err)
		}
	})

	t.Run("broad file permissions", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Chmod(root, 0o700); err != nil {
			t.Fatalf("os.Chmod(root) error = %v", err)
		}
		path := filepath.Join(root, "preferences.json")
		content, err := marshalSnapshot(validSnapshot(filepath.Join(root, "home")))
		if err != nil {
			t.Fatalf("marshalSnapshot() error = %v", err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("os.WriteFile() error = %v", err)
		}
		store, err := NewFileStore(path)
		if err != nil {
			t.Fatalf("NewFileStore() error = %v", err)
		}
		if _, err := store.Load(context.Background()); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("Load(broad file) error = %v, want ErrUnsafePath", err)
		}
	})
}

func TestFileStoreRejectsInvalidSnapshots(t *testing.T) {
	t.Parallel()

	valid := validSnapshot(filepath.Join(t.TempDir(), "home"))
	tests := map[string]func(*OnboardingSnapshot){
		"schema version":     func(snapshot *OnboardingSnapshot) { snapshot.SchemaVersion++ },
		"onboarding version": func(snapshot *OnboardingSnapshot) { snapshot.OnboardingVersion++ },
		"not completed":      func(snapshot *OnboardingSnapshot) { snapshot.OnboardingCompleted = false },
		"relative home":      func(snapshot *OnboardingSnapshot) { snapshot.CodexHome.Path = "relative" },
		"unclean home": func(snapshot *OnboardingSnapshot) {
			snapshot.CodexHome.Path += string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(snapshot.CodexHome.Path)
		},
		"empty device":   func(snapshot *OnboardingSnapshot) { snapshot.CodexHome.DeviceID = "" },
		"zero inode":     func(snapshot *OnboardingSnapshot) { snapshot.CodexHome.Inode = 0 },
		"zero timestamp": func(snapshot *OnboardingSnapshot) { snapshot.CodexHome.ConfirmedAtMS = 0 },
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "private", "preferences.json")
			store, err := NewFileStore(path)
			if err != nil {
				t.Fatalf("NewFileStore() error = %v", err)
			}
			next := valid
			mutate(&next)
			if err := store.Confirm(context.Background(), next); !errors.Is(err, ErrInvalidPreferences) {
				t.Fatalf("Confirm(invalid) error = %v, want ErrInvalidPreferences", err)
			}
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("os.Lstat(after invalid) error = %v, want not exist", err)
			}
		})
	}
}

func TestFileStoreRejectsUnknownAndTrailingJSON(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatalf("os.Chmod(root) error = %v", err)
	}
	valid := validSnapshot(filepath.Join(root, "home"))
	content, err := marshalSnapshot(valid)
	if err != nil {
		t.Fatalf("marshalSnapshot() error = %v", err)
	}
	for name, body := range map[string]string{
		"unknown field":  strings.Replace(string(content), "\n}", ",\n  \"future\": true\n}\n", 1),
		"trailing value": string(content) + "{}\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, strings.ReplaceAll(name, " ", "-")+".json")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatalf("os.WriteFile() error = %v", err)
			}
			store, err := NewFileStore(path)
			if err != nil {
				t.Fatalf("NewFileStore() error = %v", err)
			}
			if _, err := store.Load(context.Background()); !errors.Is(err, ErrInvalidPreferences) {
				t.Fatalf("Load() error = %v, want ErrInvalidPreferences", err)
			}
		})
	}
}

func TestFileStorePublishFailureLeavesNoConfiguration(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "private", "preferences.json")
	publishErr := errors.New("publish failed")
	store, err := newFileStore(path, func(string, []byte) error { return publishErr })
	if err != nil {
		t.Fatalf("newFileStore() error = %v", err)
	}
	if err := store.Confirm(context.Background(), validSnapshot(filepath.Join(t.TempDir(), "home"))); !errors.Is(err, publishErr) {
		t.Fatalf("Confirm(publish failure) error = %v, want injected error", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("os.Lstat(after failure) error = %v, want not exist", err)
	}
	if _, err := store.Load(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Load(after failure) error = %v, want ErrNotConfigured", err)
	}
}

func TestFileStoreDurabilityUnknownRequiresReadback(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "private", "preferences.json")
	store, err := newFileStore(path, func(path string, content []byte) error {
		if err := publishPrivateFile(path, content); err != nil {
			return err
		}
		return ErrDurabilityUnknown
	})
	if err != nil {
		t.Fatalf("newFileStore() error = %v", err)
	}
	want := validSnapshot(filepath.Join(t.TempDir(), "home"))
	if err := store.Confirm(context.Background(), want); !errors.Is(err, ErrDurabilityUnknown) {
		t.Fatalf("Confirm() error = %v, want ErrDurabilityUnknown", err)
	}
	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load(after uncertain durability) error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(after uncertain durability) = %#v, want %#v", got, want)
	}
}

func TestPublishPrivateFileFaultStages(t *testing.T) {
	t.Parallel()

	injected := errors.New("injected publish interruption")
	stages := []struct {
		stage     publishStage
		published bool
	}{
		{publishStageBeforeParentDirectorySync, false},
		{publishStageTemporaryCreated, false},
		{publishStageContentWritten, false},
		{publishStageFileSynced, false},
		{publishStageTargetLinked, true},
		{publishStageTemporaryRemoved, true},
		{publishStageBeforeDirectorySync, true},
	}
	for _, test := range stages {
		test := test
		t.Run(string(test.stage), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "private", "preferences.json")
			want := validSnapshot(filepath.Join(t.TempDir(), "home"))
			content, err := marshalSnapshot(want)
			if err != nil {
				t.Fatalf("marshalSnapshot() error = %v", err)
			}
			err = publishPrivateFileWithHook(path, content, func(stage publishStage) error {
				if stage == test.stage {
					return injected
				}
				return nil
			})
			if test.published {
				if !errors.Is(err, ErrDurabilityUnknown) {
					t.Fatalf("publish error = %v, want ErrDurabilityUnknown", err)
				}
				store, storeErr := NewFileStore(path)
				if storeErr != nil {
					t.Fatalf("NewFileStore() error = %v", storeErr)
				}
				got, loadErr := store.Load(context.Background())
				if loadErr != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("Load(published) = %#v, %v, want %#v", got, loadErr, want)
				}
			} else {
				if !errors.Is(err, injected) {
					t.Fatalf("publish error = %v, want injected interruption", err)
				}
				store, storeErr := NewFileStore(path)
				if storeErr != nil {
					t.Fatalf("NewFileStore() error = %v", storeErr)
				}
				if _, loadErr := store.Load(context.Background()); !errors.Is(loadErr, ErrNotConfigured) {
					t.Fatalf("Load(unpublished) error = %v, want ErrNotConfigured", loadErr)
				}
			}
			entries, readErr := os.ReadDir(filepath.Dir(path))
			if readErr != nil {
				t.Fatalf("os.ReadDir() error = %v", readErr)
			}
			for _, entry := range entries {
				if strings.Contains(entry.Name(), ".partial-") {
					t.Fatalf("temporary file remains after %s: %s", test.stage, entry.Name())
				}
			}
		})
	}
}

func TestFileStoreConcurrentExactConfirmationIsIdempotent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "private", "preferences.json")
	want := validSnapshot(filepath.Join(t.TempDir(), "home"))
	stores := make([]*FileStore, 2)
	for index := range stores {
		store, err := NewFileStore(path)
		if err != nil {
			t.Fatalf("NewFileStore(%d) error = %v", index, err)
		}
		stores[index] = store
	}
	start := make(chan struct{})
	errorsByStore := make([]error, len(stores))
	var wait sync.WaitGroup
	for index, store := range stores {
		wait.Add(1)
		go func(index int, store *FileStore) {
			defer wait.Done()
			<-start
			errorsByStore[index] = store.Confirm(context.Background(), want)
		}(index, store)
	}
	close(start)
	wait.Wait()
	for index, err := range errorsByStore {
		if err != nil {
			t.Fatalf("Confirm(%d) error = %v", index, err)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("os.ReadDir() error = %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".partial-") {
			t.Fatalf("temporary file remains after success: %s", entry.Name())
		}
	}
}

func TestFileStoreHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "private", "preferences.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Confirm(ctx, validSnapshot(filepath.Join(t.TempDir(), "home"))); !errors.Is(err, context.Canceled) {
		t.Fatalf("Confirm(canceled) error = %v, want context.Canceled", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("os.Lstat(after cancel) error = %v, want not exist", err)
	}
}

func validSnapshot(home string) OnboardingSnapshot {
	return OnboardingSnapshot{
		SchemaVersion:       CurrentSchemaVersion,
		OnboardingVersion:   CurrentOnboardingVersion,
		OnboardingCompleted: true,
		CodexHome: ConfirmedSource{
			Path: home, DeviceID: "device-1", Inode: 42, ConfirmedAtMS: 1_720_000_000_000,
		},
		OnlineQuotaEnabled:  true,
		ResetCreditsEnabled: false,
	}
}
