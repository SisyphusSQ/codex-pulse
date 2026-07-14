//go:build darwin

package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const sessionIndexFilename = "session_index.jsonl"

type rootVersion struct {
	deviceID string
	inode    uint64
}

// IndexFile 绑定用户已确认的 Codex Home 物理身份，只访问根级 session index。
type IndexFile struct {
	home            string
	root            rootVersion
	syncDirectory   func(string) error
	syncRoot        func(int) error
	afterFinalCheck func()
}

// IndexRead 是一次完整、无副作用的 session index 读取。
type IndexRead struct {
	Content []byte
	Version FileVersion
}

// IndexBackupReport 描述成功发布的私有 index backup 或 absence marker。
type IndexBackupReport struct {
	Path          string
	SourceExisted bool
	Bytes         int64
}

// OpenIndexFile 确认绝对、非 symlink Codex Home 的物理身份。
func OpenIndexFile(home string) (*IndexFile, error) {
	if !filepath.IsAbs(home) || filepath.Clean(home) != home {
		return nil, ErrInvalidHome
	}
	file, version, err := openRoot(home)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close confirmed Codex home: %w", err)
	}
	return &IndexFile{
		home: home, root: version, syncDirectory: syncDirectoryPath, syncRoot: unix.Fsync,
	}, nil
}

// Read 重新确认 home 后完整读取根级 index，并用前后 fstat 拒绝扫描竞态。
func (indexFile *IndexFile) Read(ctx context.Context) (IndexRead, error) {
	if err := ctx.Err(); err != nil {
		return IndexRead{}, err
	}
	root, err := indexFile.openRoot()
	if err != nil {
		return IndexRead{}, err
	}
	defer root.Close()
	return readIndexAt(ctx, int(root.Fd()))
}

// Backup 只在 source version 精确匹配时发布 byte-for-byte backup 或空 absence marker。
func (indexFile *IndexFile) Backup(
	ctx context.Context,
	expected FileVersion,
	destination string,
) (IndexBackupReport, error) {
	if !validFileVersion(expected) {
		return IndexBackupReport{}, ErrInvalidPlan
	}
	if indexFile == nil || indexFile.syncDirectory == nil {
		return IndexBackupReport{}, ErrInvalidPlan
	}
	current, err := indexFile.Read(ctx)
	if err != nil {
		return IndexBackupReport{}, err
	}
	if current.Version != expected {
		clear(current.Content)
		return IndexBackupReport{}, ErrPlanDrift
	}
	defer clear(current.Content)
	destination, err = validatePrivateDestination(destination)
	if err != nil {
		return IndexBackupReport{}, err
	}
	if err := publishPrivateBytesWithSync(destination, current.Content, indexFile.syncDirectory); err != nil {
		return IndexBackupReport{}, err
	}
	return IndexBackupReport{
		Path: destination, SourceExisted: expected.Exists, Bytes: int64(len(current.Content)),
	}, nil
}

// Append 只向确认版本追加 canonical entries；不会重写既有 rename history。
func (indexFile *IndexFile) Append(
	ctx context.Context,
	expected FileVersion,
	entries []Entry,
) (FileVersion, error) {
	if !validFileVersion(expected) || indexFile == nil || indexFile.syncRoot == nil {
		return FileVersion{}, ErrInvalidPlan
	}
	payload, err := canonicalEntries(entries)
	if err != nil {
		return FileVersion{}, err
	}
	if err := ctx.Err(); err != nil {
		return FileVersion{}, err
	}
	root, err := indexFile.openRoot()
	if err != nil {
		return FileVersion{}, err
	}
	defer root.Close()

	var proof appendProof
	if expected.Exists {
		proof, err = indexFile.appendExistingIndex(ctx, int(root.Fd()), expected, payload)
	} else {
		if err := validateAppendSize(0, len(payload)); err != nil {
			return FileVersion{}, err
		}
		proof, err = indexFile.createIndexWithAppend(ctx, int(root.Fd()), payload)
	}
	if err != nil {
		return FileVersion{}, err
	}
	read, err := indexFile.Read(ctx)
	if err != nil {
		return FileVersion{}, err
	}
	defer clear(read.Content)
	if read.Version.SizeBytes != proof.sizeBytes || read.Version.SHA256 != proof.sha256 {
		return FileVersion{}, ErrPlanDrift
	}
	return read.Version, nil
}

type appendProof struct {
	sizeBytes int64
	sha256    string
}

func (indexFile *IndexFile) openRoot() (*os.File, error) {
	if indexFile == nil || indexFile.home == "" {
		return nil, ErrInvalidHome
	}
	root, version, err := openRoot(indexFile.home)
	if err != nil {
		return nil, err
	}
	if version != indexFile.root {
		_ = root.Close()
		return nil, ErrHomeChanged
	}
	return root, nil
}

func openRoot(home string) (*os.File, rootVersion, error) {
	descriptor, err := unix.Open(home, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return nil, rootVersion{}, ErrInvalidHome
		}
		return nil, rootVersion{}, fmt.Errorf("open Codex home: %w", err)
	}
	file := os.NewFile(uintptr(descriptor), home)
	if file == nil {
		_ = unix.Close(descriptor)
		return nil, rootVersion{}, ErrInvalidHome
	}
	var stat unix.Stat_t
	if err := unix.Fstat(descriptor, &stat); err != nil {
		_ = file.Close()
		return nil, rootVersion{}, fmt.Errorf("stat Codex home: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = file.Close()
		return nil, rootVersion{}, ErrInvalidHome
	}
	return file, rootVersion{deviceID: deviceID(stat.Dev), inode: stat.Ino}, nil
}

func readIndexAt(ctx context.Context, rootDescriptor int) (IndexRead, error) {
	descriptor, err := unix.Openat(
		rootDescriptor, sessionIndexFilename,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0,
	)
	if errors.Is(err, unix.ENOENT) {
		return IndexRead{}, nil
	}
	if err != nil {
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return IndexRead{}, ErrUnsafeIndex
		}
		return IndexRead{}, fmt.Errorf("open session index: %w", err)
	}
	file := os.NewFile(uintptr(descriptor), sessionIndexFilename)
	if file == nil {
		_ = unix.Close(descriptor)
		return IndexRead{}, ErrUnsafeIndex
	}
	defer file.Close()
	return readOpenedIndex(ctx, file)
}

func readOpenedIndex(ctx context.Context, file *os.File) (IndexRead, error) {
	if err := ctx.Err(); err != nil {
		return IndexRead{}, err
	}
	var before unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &before); err != nil {
		return IndexRead{}, fmt.Errorf("stat session index: %w", err)
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Size < 0 || before.Size > maxIndexBytes || before.Ino > math.MaxInt64 {
		if before.Size > maxIndexBytes {
			return IndexRead{}, ErrIndexTooLarge
		}
		return IndexRead{}, ErrUnsafeIndex
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return IndexRead{}, fmt.Errorf("seek session index: %w", err)
	}
	content, err := io.ReadAll(io.LimitReader(file, maxIndexBytes+1))
	if err != nil {
		return IndexRead{}, fmt.Errorf("read session index: %w", err)
	}
	if len(content) > maxIndexBytes {
		clear(content)
		return IndexRead{}, ErrIndexTooLarge
	}
	var after unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &after); err != nil {
		clear(content)
		return IndexRead{}, fmt.Errorf("restat session index: %w", err)
	}
	if !sameIndexStat(before, after) || int64(len(content)) != before.Size {
		clear(content)
		return IndexRead{}, ErrPlanDrift
	}
	digest := sha256.Sum256(content)
	mtimeNS := before.Mtim.Sec*1_000_000_000 + before.Mtim.Nsec
	if mtimeNS < 0 {
		clear(content)
		return IndexRead{}, ErrUnsafeIndex
	}
	return IndexRead{
		Content: content,
		Version: FileVersion{
			Exists: true, DeviceID: deviceID(before.Dev), Inode: int64(before.Ino),
			SizeBytes: before.Size, MTimeNS: mtimeNS, SHA256: hex.EncodeToString(digest[:]),
		},
	}, nil
}

func (indexFile *IndexFile) appendExistingIndex(
	ctx context.Context,
	rootDescriptor int,
	expected FileVersion,
	payload []byte,
) (appendProof, error) {
	descriptor, err := unix.Openat(
		rootDescriptor, sessionIndexFilename,
		unix.O_RDWR|unix.O_APPEND|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0,
	)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return appendProof{}, ErrPlanDrift
		}
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return appendProof{}, ErrUnsafeIndex
		}
		return appendProof{}, fmt.Errorf("open session index for append: %w", err)
	}
	file := os.NewFile(uintptr(descriptor), sessionIndexFilename)
	if file == nil {
		_ = unix.Close(descriptor)
		return appendProof{}, ErrUnsafeIndex
	}
	defer file.Close()
	read, err := readOpenedIndex(ctx, file)
	if err != nil {
		return appendProof{}, err
	}
	if read.Version != expected {
		clear(read.Content)
		return appendProof{}, ErrPlanDrift
	}
	if len(read.Content) > 0 && read.Content[len(read.Content)-1] != '\n' {
		payload = append([]byte{'\n'}, payload...)
	}
	if err := validateAppendSize(expected.SizeBytes, len(payload)); err != nil {
		clear(read.Content)
		return appendProof{}, err
	}
	proof := appendedContentProof(read.Content, payload)
	clear(read.Content)
	if indexFile.afterFinalCheck != nil {
		indexFile.afterFinalCheck()
	}
	if err := appendAndSync(ctx, file, payload); err != nil {
		return appendProof{}, err
	}
	return proof, nil
}

func (indexFile *IndexFile) createIndexWithAppend(
	ctx context.Context,
	rootDescriptor int,
	payload []byte,
) (appendProof, error) {
	descriptor, err := unix.Openat(
		rootDescriptor, sessionIndexFilename,
		unix.O_WRONLY|unix.O_APPEND|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0o600,
	)
	if errors.Is(err, unix.EEXIST) {
		return appendProof{}, ErrPlanDrift
	}
	if err != nil {
		return appendProof{}, fmt.Errorf("create session index: %w", err)
	}
	file := os.NewFile(uintptr(descriptor), sessionIndexFilename)
	if file == nil {
		_ = unix.Close(descriptor)
		return appendProof{}, ErrUnsafeIndex
	}
	defer file.Close()
	if indexFile.afterFinalCheck != nil {
		indexFile.afterFinalCheck()
	}
	if err := appendAndSync(ctx, file, payload); err != nil {
		return appendProof{}, err
	}
	if err := indexFile.syncRoot(rootDescriptor); err != nil {
		return appendProof{}, fmt.Errorf("sync Codex home after index create: %w", err)
	}
	return appendedContentProof(nil, payload), nil
}

func appendedContentProof(prefix, payload []byte) appendProof {
	digest := sha256.New()
	_, _ = digest.Write(prefix)
	_, _ = digest.Write(payload)
	return appendProof{
		sizeBytes: int64(len(prefix) + len(payload)),
		sha256:    hex.EncodeToString(digest.Sum(nil)),
	}
}

func appendAndSync(ctx context.Context, file *os.File, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	written, err := file.Write(payload)
	if err != nil {
		return fmt.Errorf("append session index: %w", err)
	}
	if written != len(payload) {
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync session index: %w", err)
	}
	return ctx.Err()
}

func canonicalEntries(entries []Entry) ([]byte, error) {
	if len(entries) == 0 {
		return nil, ErrInvalidPlan
	}
	var payload []byte
	for _, entry := range entries {
		if _, err := uuid.Parse(entry.ID); err != nil || entry.ThreadName == "" ||
			len(entry.ThreadName) > maxThreadNameBytes {
			return nil, ErrInvalidPlan
		}
		updatedAt, err := time.Parse(time.RFC3339Nano, entry.UpdatedAt)
		if err != nil || updatedAt.UnixMilli() < 0 {
			return nil, ErrInvalidPlan
		}
		line, err := json.Marshal(entry)
		if err != nil {
			return nil, fmt.Errorf("encode session index entry: %w", err)
		}
		if len(line) > maxIndexLineBytes {
			return nil, ErrInvalidPlan
		}
		if int64(len(payload))+int64(len(line))+1 > maxIndexBytes {
			return nil, ErrIndexTooLarge
		}
		payload = append(payload, line...)
		payload = append(payload, '\n')
	}
	return payload, nil
}

func validateAppendSize(currentSize int64, payloadSize int) error {
	if currentSize < 0 || payloadSize < 0 {
		return ErrInvalidPlan
	}
	if currentSize > maxIndexBytes || int64(payloadSize) > int64(maxIndexBytes)-currentSize {
		return ErrIndexTooLarge
	}
	return nil
}

func validatePrivateDestination(destination string) (string, error) {
	if !filepath.IsAbs(destination) || filepath.Clean(destination) != destination {
		return "", ErrInvalidBackup
	}
	parent := filepath.Dir(destination)
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return "", ErrInvalidBackup
	}
	if _, err := os.Lstat(destination); err == nil || !errors.Is(err, os.ErrNotExist) {
		return "", ErrInvalidBackup
	}
	return destination, nil
}

func publishPrivateBytesWithSync(
	destination string,
	content []byte,
	syncParent func(string) error,
) error {
	if syncParent == nil {
		return ErrInvalidBackup
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create session index backup: %w", err)
	}
	written, err := file.Write(content)
	if err != nil || written != len(content) {
		writeErr := err
		if writeErr == nil {
			writeErr = io.ErrShortWrite
		}
		return errors.Join(
			fmt.Errorf("write session index backup: %w", writeErr),
			file.Close(),
			removeAndSyncPrivateFile(destination, syncParent),
		)
	}
	if err := file.Sync(); err != nil {
		return errors.Join(
			fmt.Errorf("sync session index backup: %w", err),
			file.Close(),
			removeAndSyncPrivateFile(destination, syncParent),
		)
	}
	if err := file.Close(); err != nil {
		return errors.Join(
			fmt.Errorf("close session index backup: %w", err),
			removeAndSyncPrivateFile(destination, syncParent),
		)
	}
	parent := filepath.Dir(destination)
	if err := syncParent(parent); err != nil {
		return errors.Join(
			fmt.Errorf("sync session index backup directory: %w", err),
			removeAndSyncPrivateFile(destination, syncParent),
		)
	}
	return nil
}

func removeAndSyncPrivateFile(path string, syncParent func(string) error) error {
	removeErr := os.Remove(path)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	return errors.Join(removeErr, syncParent(filepath.Dir(path)))
}

func syncDirectoryPath(path string) error {
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	syncErr := unix.Fsync(descriptor)
	closeErr := unix.Close(descriptor)
	return errors.Join(syncErr, closeErr)
}

func sameIndexStat(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino && left.Mode == right.Mode &&
		left.Size == right.Size && left.Mtim == right.Mtim && left.Ctim == right.Ctim
}

func deviceID(device int32) string {
	return strconv.FormatUint(uint64(uint32(device)), 10)
}
