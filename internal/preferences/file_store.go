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
	"reflect"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const maximumPreferencesBytes = 64 << 10

type publisher func(path string, content []byte) error
type replacer func(path string, content []byte) error

type publishStage string

const (
	publishStageBeforeParentDirectorySync publishStage = "before_parent_directory_sync"
	publishStageTemporaryCreated          publishStage = "temporary_created"
	publishStageContentWritten            publishStage = "content_written"
	publishStageFileSynced                publishStage = "file_synced"
	publishStageTargetLinked              publishStage = "target_linked"
	publishStageTargetReplaced            publishStage = "target_replaced"
	publishStageTemporaryRemoved          publishStage = "temporary_removed"
	publishStageBeforeDirectorySync       publishStage = "before_directory_sync"
)

type publishHook func(publishStage) error

// FileStore 持有私有 Preferences 文件，统一提供首次 no-overwrite 确认、版本迁移与 revision CAS。
type FileStore struct {
	path    string
	publish publisher
	replace replacer
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
	return newFileStoreWithReplace(path, publishPrivateFile, replacePrivateFile)
}

func newFileStore(path string, publish publisher) (*FileStore, error) {
	return newFileStoreWithReplace(path, publish, replacePrivateFile)
}

func newFileStoreWithReplace(path string, publish publisher, replace replacer) (*FileStore, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) == "." ||
		filepath.Base(path) == string(filepath.Separator) || publish == nil || replace == nil {
		return nil, ErrUnsafePath
	}
	return &FileStore{path: path, publish: publish, replace: replace}, nil
}

func (store *FileStore) Load(ctx context.Context) (OnboardingSnapshot, error) {
	value, err := store.LoadPreferences(ctx)
	if err != nil {
		return OnboardingSnapshot{}, err
	}
	return onboardingFromPreferences(value)
}

// LoadPreferences 读取 current typed snapshot；legacy v1 onboarding 文件会在 CAS 共用的
// 跨实例锁内最多迁移一次。
func (store *FileStore) LoadPreferences(ctx context.Context) (Snapshot, error) {
	if store == nil {
		return Snapshot{}, ErrUnsafePath
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	value, migrated, err := loadPreferencesFromPath(ctx, store.path)
	if err != nil || !migrated {
		return value, err
	}
	lock, err := acquirePreferencesLock(ctx, store.path)
	if err != nil {
		return Snapshot{}, err
	}
	defer lock.Release()
	// 获得锁前，其他实例可能已经完成迁移，因此必须在锁内重新读取。
	current, stillLegacy, err := loadPreferencesFromPath(ctx, store.path)
	if err != nil {
		return Snapshot{}, err
	}
	if !stillLegacy {
		return current, nil
	}
	content, err := marshalPreferences(current)
	if err != nil {
		return Snapshot{}, err
	}
	replaceErr := store.replace(store.path, content)
	if replaceErr != nil && !errors.Is(replaceErr, ErrDurabilityUnknown) {
		return Snapshot{}, replaceErr
	}
	readbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	readback, legacy, err := loadPreferencesFromPath(readbackCtx, store.path)
	if err != nil || legacy || !reflect.DeepEqual(readback, current) {
		if err != nil {
			return Snapshot{}, errors.Join(ErrDurabilityUnknown, replaceErr, err)
		}
		return Snapshot{}, errors.Join(ErrDurabilityUnknown, replaceErr)
	}
	return readback, nil
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
	preferences, err := preferencesFromOnboarding(next)
	if err != nil {
		return err
	}
	content, err := marshalPreferences(preferences)
	if err != nil {
		return err
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	currentPreferences, _, err := loadPreferencesFromPath(ctx, store.path)
	current, viewErr := onboardingFromPreferences(currentPreferences)
	switch {
	case err == nil && viewErr == nil && sameConfirmation(current, next):
		return nil
	case err == nil && viewErr == nil:
		return ErrAlreadyConfirmed
	case err == nil:
		return viewErr
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
	// 本实例准备首次快照时，其他进程可能已经发布。精确相同的确认按幂等成功处理，
	// 但绝不覆盖冲突来源。
	currentPreferences, _, loadErr := loadPreferencesFromPath(ctx, store.path)
	current, viewErr = onboardingFromPreferences(currentPreferences)
	if loadErr == nil && viewErr != nil {
		loadErr = viewErr
	}
	if loadErr == nil && sameConfirmation(current, next) {
		return nil
	}
	if loadErr != nil {
		return errors.Join(ErrAlreadyConfirmed, loadErr)
	}
	return ErrAlreadyConfirmed
}

// CompareAndSwap 仅在 expectedRevision 仍为权威值时替换完整快照；next.Revision 必须精确加一。
func (store *FileStore) CompareAndSwap(ctx context.Context, expectedRevision uint64, next Snapshot) error {
	if store == nil {
		return ErrUnsafePath
	}
	wantRevision, err := nextRevision(expectedRevision)
	if err != nil || next.Revision != wantRevision {
		return ErrInvalidPreferences
	}
	if err := validatePreferences(next); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	content, err := marshalPreferences(next)
	if err != nil {
		return err
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	lock, err := acquirePreferencesLock(ctx, store.path)
	if err != nil {
		return err
	}
	defer lock.Release()
	current, _, err := loadPreferencesFromPath(ctx, store.path)
	if err != nil {
		if errors.Is(err, ErrNotConfigured) {
			return ErrPreferencesConflict
		}
		return err
	}
	if current.Revision != expectedRevision {
		return ErrPreferencesConflict
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := store.replace(store.path, content); err != nil {
		return err
	}
	readback, legacy, err := loadPreferencesFromPath(ctx, store.path)
	if err != nil || legacy || !reflect.DeepEqual(readback, next) {
		if err != nil {
			return errors.Join(ErrDurabilityUnknown, err)
		}
		return ErrDurabilityUnknown
	}
	return nil
}

func loadPreferencesFromPath(ctx context.Context, path string) (Snapshot, bool, error) {
	content, err := loadPreferencesContent(ctx, path)
	if err != nil {
		return Snapshot{}, false, err
	}
	return decodePreferences(content)
}

func loadPreferencesContent(ctx context.Context, path string) ([]byte, error) {
	parent, base := filepath.Dir(path), filepath.Base(path)
	root, err := openPrivateRoot(parent, false)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotConfigured
		}
		return nil, err
	}
	defer func() { _ = root.Close() }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := root.Lstat(base)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotConfigured
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() < 0 ||
		info.Size() > maximumPreferencesBytes {
		return nil, ErrUnsafePath
	}
	file, err := root.OpenFile(base, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, openedInfo) || !openedInfo.Mode().IsRegular() || openedInfo.Mode().Perm() != 0o600 {
		return nil, ErrUnsafePath
	}
	content, err := io.ReadAll(io.LimitReader(file, maximumPreferencesBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maximumPreferencesBytes {
		return nil, ErrUnsafePath
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return content, nil
}

func decodeSnapshot(content []byte) (OnboardingSnapshot, error) {
	if err := validateLegacyJSONShape(content); err != nil {
		return OnboardingSnapshot{}, err
	}
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
	// hard link 发布不会覆盖其他进程并发创建的快照；随后移除同目录临时链接，
	// 即可只留下原子可见的确认文件。
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

func replacePrivateFile(path string, content []byte) error {
	return replacePrivateFileWithHook(path, content, nil)
}

func replacePrivateFileWithHook(path string, content []byte, hook publishHook) error {
	parent, base := filepath.Dir(path), filepath.Base(path)
	root, err := openPrivateRoot(parent, false)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	current, err := root.Lstat(base)
	if err != nil {
		return err
	}
	if !current.Mode().IsRegular() || current.Mode().Perm() != 0o600 {
		return ErrUnsafePath
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
	if err := root.Rename(temporaryName, base); err != nil {
		return err
	}
	temporaryPresent = false
	if err := runPublishHook(hook, publishStageTargetReplaced, true); err != nil {
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

type preferencesLock struct {
	file *os.File
}

func acquirePreferencesLock(ctx context.Context, path string) (*preferencesLock, error) {
	return acquireNamedPreferencesLock(ctx, path, ".preferences.lock")
}

// AcquireSwitchLease 串行化跨进程的 Confirm/Recover runtime side effects。
// 独立 lock file 避免与每次 Preferences CAS 的短锁自冲突；进程退出会由 OS 释放 flock。
func (store *FileStore) AcquireSwitchLease(ctx context.Context) (SwitchExecutionLease, error) {
	if store == nil {
		return nil, ErrUnsafePath
	}
	lock, err := acquireNamedPreferencesLock(ctx, store.path, ".preferences.switch.lock")
	if err != nil {
		return nil, err
	}
	return lock, nil
}

func acquireNamedPreferencesLock(ctx context.Context, path, lockName string) (*preferencesLock, error) {
	root, err := openPrivateRoot(filepath.Dir(path), false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	before, err := root.Lstat(lockName)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if before != nil && (!before.Mode().IsRegular() || before.Mode().Perm() != 0o600) {
		return nil, ErrUnsafePath
	}
	var file *os.File
	if errors.Is(err, fs.ErrNotExist) {
		file, err = root.OpenFile(lockName, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			before, err = root.Lstat(lockName)
			if err == nil {
				file, err = root.OpenFile(lockName, os.O_RDWR, 0)
			}
		}
	} else {
		file, err = root.OpenFile(lockName, os.O_RDWR, 0)
	}
	if err != nil {
		return nil, err
	}
	closeWithError := func(value error) (*preferencesLock, error) {
		_ = file.Close()
		return nil, value
	}
	after, err := root.Lstat(lockName)
	if err != nil {
		return closeWithError(err)
	}
	opened, err := file.Stat()
	if err != nil {
		return closeWithError(err)
	}
	if !after.Mode().IsRegular() || after.Mode().Perm() != 0o600 ||
		!opened.Mode().IsRegular() || opened.Mode().Perm() != 0o600 || !os.SameFile(after, opened) ||
		(before != nil && !os.SameFile(before, after)) {
		return closeWithError(ErrUnsafePath)
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return &preferencesLock{file: file}, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return closeWithError(err)
		}
		select {
		case <-ctx.Done():
			return closeWithError(ctx.Err())
		case <-ticker.C:
		}
	}
}

func (lock *preferencesLock) Release() {
	if lock == nil || lock.file == nil {
		return
	}
	_ = unix.Flock(int(lock.file.Fd()), unix.LOCK_UN)
	_ = lock.file.Close()
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
