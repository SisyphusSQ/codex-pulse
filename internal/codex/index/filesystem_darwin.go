//go:build darwin

package index

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"

	"golang.org/x/sys/unix"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/homeidentity"
)

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
	identity, err := homeidentity.FromDescriptor(descriptor)
	if err != nil {
		_ = file.Close()
		return nil, rootVersion{}, fmt.Errorf("identify Codex home: %w", err)
	}
	return file, rootVersion{deviceID: identity.DeviceID, inode: uint64(identity.Inode)}, nil
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

func readIndexRoot(ctx context.Context, root *os.File) (IndexRead, error) {
	return readIndexAt(ctx, int(root.Fd()))
}
func (indexFile *IndexFile) appendExistingIndexRoot(ctx context.Context, root *os.File, expected FileVersion, payload []byte) (appendProof, error) {
	return indexFile.appendExistingIndex(ctx, int(root.Fd()), expected, payload)
}
func (indexFile *IndexFile) createIndexWithAppendRoot(ctx context.Context, root *os.File, payload []byte) (appendProof, error) {
	return indexFile.createIndexWithAppend(ctx, int(root.Fd()), payload)
}
func syncRootDescriptor(fd int) error { return unix.Fsync(fd) }
func statIndexFile(file *os.File) (indexFileStatus, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return indexFileStatus{}, err
	}
	if stat.Ino > math.MaxInt64 {
		return indexFileStatus{}, ErrUnsafeIndex
	}
	return indexFileStatus{deviceID: deviceID(stat.Dev), inode: int64(stat.Ino), size: stat.Size,
		mtimeNS: stat.Mtim.Sec*1_000_000_000 + stat.Mtim.Nsec, changeNS: stat.Ctim.Sec*1_000_000_000 + stat.Ctim.Nsec,
		attributes: uint32(stat.Mode), regular: stat.Mode&unix.S_IFMT == unix.S_IFREG}, nil
}
