package preferences

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

const maximumPreferencesBytes = 64 << 10

type publisher func(path string, content []byte) error

type publishStage string

const (
	publishStageBeforeParentDirectorySync publishStage = "before_parent_directory_sync"
	publishStageTemporaryCreated          publishStage = "temporary_created"
	publishStageContentWritten            publishStage = "content_written"
	publishStageFileSynced                publishStage = "file_synced"
	publishStageTargetLinked              publishStage = "target_linked"
	publishStageTemporaryRemoved          publishStage = "temporary_removed"
	publishStageBeforeDirectorySync       publishStage = "before_directory_sync"
)

type publishHook func(publishStage) error

// FileStore owns the minimal onboarding snapshot. Full preference updates and
// migrations intentionally remain outside this first-confirmation store.
type FileStore struct {
	path    string
	publish publisher
	mu      sync.Mutex
}

func DefaultPath() (string, error) {
	configurationDirectory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configurationDirectory, "Codex Pulse", "preferences.json"), nil
}

func NewFileStore(path string) (*FileStore, error) {
	return newFileStore(path, publishPrivateFile)
}

func newFileStore(path string, publish publisher) (*FileStore, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) == "." ||
		filepath.Base(path) == string(filepath.Separator) || publish == nil {
		return nil, ErrUnsafePath
	}
	return &FileStore{path: path, publish: publish}, nil
}

func (store *FileStore) Load(ctx context.Context) (OnboardingSnapshot, error) {
	if store == nil {
		return OnboardingSnapshot{}, ErrUnsafePath
	}
	if err := ctx.Err(); err != nil {
		return OnboardingSnapshot{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return loadSnapshot(ctx, store.path)
}

func (store *FileStore) Confirm(ctx context.Context, next OnboardingSnapshot) error {
	if store == nil {
		return ErrUnsafePath
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateSnapshot(next); err != nil {
		return err
	}
	content, err := marshalSnapshot(next)
	if err != nil {
		return err
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := loadSnapshot(ctx, store.path)
	switch {
	case err == nil && sameConfirmation(current, next):
		return nil
	case err == nil:
		return ErrAlreadyConfirmed
	case !errors.Is(err, ErrNotConfigured):
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err = store.publish(store.path, content)
	if !errors.Is(err, ErrAlreadyConfirmed) {
		return err
	}
	// Another process may have published while this instance was preparing its
	// first snapshot. Treat an exact confirmation as the same idempotent event,
	// but never overwrite a conflicting source.
	current, loadErr := loadSnapshot(ctx, store.path)
	if loadErr == nil && sameConfirmation(current, next) {
		return nil
	}
	if loadErr != nil {
		return errors.Join(ErrAlreadyConfirmed, loadErr)
	}
	return ErrAlreadyConfirmed
}

func loadSnapshot(ctx context.Context, path string) (OnboardingSnapshot, error) {
	parent, base := filepath.Dir(path), filepath.Base(path)
	root, err := openPrivateRoot(parent, false)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return OnboardingSnapshot{}, ErrNotConfigured
		}
		return OnboardingSnapshot{}, err
	}
	defer func() { _ = root.Close() }()
	if err := ctx.Err(); err != nil {
		return OnboardingSnapshot{}, err
	}
	info, err := root.Lstat(base)
	if errors.Is(err, fs.ErrNotExist) {
		return OnboardingSnapshot{}, ErrNotConfigured
	}
	if err != nil {
		return OnboardingSnapshot{}, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() < 0 ||
		info.Size() > maximumPreferencesBytes {
		return OnboardingSnapshot{}, ErrUnsafePath
	}
	file, err := root.OpenFile(base, os.O_RDONLY, 0)
	if err != nil {
		return OnboardingSnapshot{}, err
	}
	defer func() { _ = file.Close() }()
	openedInfo, err := file.Stat()
	if err != nil {
		return OnboardingSnapshot{}, err
	}
	if !os.SameFile(info, openedInfo) || !openedInfo.Mode().IsRegular() || openedInfo.Mode().Perm() != 0o600 {
		return OnboardingSnapshot{}, ErrUnsafePath
	}
	content, err := io.ReadAll(io.LimitReader(file, maximumPreferencesBytes+1))
	if err != nil {
		return OnboardingSnapshot{}, err
	}
	if len(content) > maximumPreferencesBytes {
		return OnboardingSnapshot{}, ErrUnsafePath
	}
	if err := ctx.Err(); err != nil {
		return OnboardingSnapshot{}, err
	}
	return decodeSnapshot(content)
}

func decodeSnapshot(content []byte) (OnboardingSnapshot, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var snapshot OnboardingSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return OnboardingSnapshot{}, fmt.Errorf("%w: malformed JSON", ErrInvalidPreferences)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return OnboardingSnapshot{}, fmt.Errorf("%w: trailing JSON", ErrInvalidPreferences)
	}
	if err := validateSnapshot(snapshot); err != nil {
		return OnboardingSnapshot{}, err
	}
	return snapshot, nil
}

func marshalSnapshot(snapshot OnboardingSnapshot) ([]byte, error) {
	content, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("%w: encode snapshot", ErrInvalidPreferences)
	}
	return append(content, '\n'), nil
}

func publishPrivateFile(path string, content []byte) error {
	return publishPrivateFileWithHook(path, content, nil)
}

func publishPrivateFileWithHook(path string, content []byte, hook publishHook) error {
	parent, base := filepath.Dir(path), filepath.Base(path)
	root, err := openPrivateRoot(parent, true)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := runPublishHook(hook, publishStageBeforeParentDirectorySync, false); err != nil {
		return err
	}
	if err := syncContainingDirectory(parent); err != nil {
		return err
	}
	if _, err := root.Lstat(base); err == nil {
		return ErrAlreadyConfirmed
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	temporaryName, err := privateTemporaryName()
	if err != nil {
		return err
	}
	temporary, err := root.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	temporaryPresent := true
	defer func() {
		_ = temporary.Close()
		if temporaryPresent {
			_ = root.Remove(temporaryName)
		}
	}()
	if err := runPublishHook(hook, publishStageTemporaryCreated, false); err != nil {
		return err
	}
	if err := writeAll(temporary, content); err != nil {
		return err
	}
	if err := runPublishHook(hook, publishStageContentWritten, false); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := runPublishHook(hook, publishStageFileSynced, false); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	// Link publishes without replacing a snapshot created concurrently by
	// another process. Removing the same-directory temporary link afterwards
	// leaves the confirmed file atomically visible.
	if err := root.Link(temporaryName, base); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return ErrAlreadyConfirmed
		}
		return err
	}
	if err := runPublishHook(hook, publishStageTargetLinked, true); err != nil {
		return err
	}
	if err := root.Remove(temporaryName); err != nil {
		return fmt.Errorf("%w: remove published temporary link", ErrDurabilityUnknown)
	}
	temporaryPresent = false
	if err := runPublishHook(hook, publishStageTemporaryRemoved, true); err != nil {
		return err
	}
	directory, err := root.Open(".")
	if err != nil {
		return fmt.Errorf("%w: open parent directory", ErrDurabilityUnknown)
	}
	defer func() { _ = directory.Close() }()
	if err := runPublishHook(hook, publishStageBeforeDirectorySync, true); err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("%w: sync parent directory", ErrDurabilityUnknown)
	}
	return nil
}

func runPublishHook(hook publishHook, stage publishStage, targetPublished bool) error {
	if hook == nil {
		return nil
	}
	if err := hook(stage); err != nil {
		if targetPublished {
			return ErrDurabilityUnknown
		}
		return err
	}
	return nil
}

func openPrivateRoot(path string, create bool) (*os.Root, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) && create {
		if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrUnsafePath
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	openedInfo, err := root.Stat(".")
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	if !os.SameFile(info, openedInfo) || !openedInfo.IsDir() || openedInfo.Mode().Perm() != 0o700 {
		_ = root.Close()
		return nil, ErrUnsafePath
	}
	return root, nil
}

func syncContainingDirectory(path string) error {
	containingPath := filepath.Dir(path)
	info, err := os.Lstat(containingPath)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrUnsafePath
	}
	root, err := os.OpenRoot(containingPath)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	openedInfo, err := root.Stat(".")
	if err != nil {
		return err
	}
	if !os.SameFile(info, openedInfo) || !openedInfo.IsDir() {
		return ErrUnsafePath
	}
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	return directory.Sync()
}

func privateTemporaryName() (string, error) {
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return ".preferences.partial-" + hex.EncodeToString(random[:]), nil
}

func writeAll(writer io.Writer, content []byte) error {
	for len(content) > 0 {
		written, err := writer.Write(content)
		if err != nil {
			return err
		}
		if written <= 0 {
			return io.ErrShortWrite
		}
		content = content[written:]
	}
	return nil
}
