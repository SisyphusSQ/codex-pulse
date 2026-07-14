//go:build darwin

package logs

import (
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

type fileSystem interface {
	ConfirmRoot(path string) (rootIdentity, error)
	OpenRoot(path string) (scanRoot, error)
}

type scanRoot interface {
	Identity() rootIdentity
	ReadDir(relativePath string) ([]fs.DirEntry, error)
	Probe(relativePath string, prefixLimit int64) (fileProbe, error)
	Close() error
}

type rootIdentity struct {
	deviceID string
	inode    int64
}

type fileProbe struct {
	deviceID  string
	inode     int64
	sizeBytes int64
	mtimeNS   int64
	prefix    []byte
}

type osFileSystem struct{}

func (osFileSystem) ConfirmRoot(path string) (rootIdentity, error) {
	root, err := openOSRoot(path)
	if err != nil {
		return rootIdentity{}, classifyRootOpenError(path, err)
	}
	defer func() { _ = root.Close() }()
	return root.Identity(), nil
}

func (osFileSystem) OpenRoot(path string) (scanRoot, error) {
	root, err := openOSRoot(path)
	if err != nil {
		return nil, classifyRootOpenError(path, err)
	}
	return root, nil
}

type osScanRoot struct {
	file     *os.File
	identity rootIdentity
}

func openOSRoot(path string) (*osScanRoot, error) {
	fileDescriptor, err := unix.Open(
		path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fileDescriptor), path)
	if file == nil {
		_ = unix.Close(fileDescriptor)
		return nil, ErrInvalidHome
	}
	identity, err := identityFromDescriptor(fileDescriptor)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &osScanRoot{file: file, identity: identity}, nil
}

func (root *osScanRoot) Identity() rootIdentity {
	return root.identity
}

func (root *osScanRoot) ReadDir(relativePath string) ([]fs.DirEntry, error) {
	if root == nil || root.file == nil {
		return nil, ErrHomeChanged
	}
	fileDescriptor, err := openAtNoFollow(int(root.file.Fd()), relativePath, true)
	if err != nil {
		return nil, classifySourceOpenError(err)
	}
	directory := os.NewFile(uintptr(fileDescriptor), relativePath)
	if directory == nil {
		_ = unix.Close(fileDescriptor)
		return nil, ErrChangedDuringScan
	}
	defer func() { _ = directory.Close() }()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	return entries, nil
}

func (root *osScanRoot) Probe(relativePath string, prefixLimit int64) (fileProbe, error) {
	if root == nil || root.file == nil {
		return fileProbe{}, ErrHomeChanged
	}
	fileDescriptor, err := openAtNoFollow(int(root.file.Fd()), relativePath, false)
	if err != nil {
		return fileProbe{}, classifySourceOpenError(err)
	}
	file := os.NewFile(uintptr(fileDescriptor), relativePath)
	if file == nil {
		_ = unix.Close(fileDescriptor)
		return fileProbe{}, ErrUnsupportedFile
	}
	defer func() { _ = file.Close() }()

	var before unix.Stat_t
	if err := unix.Fstat(fileDescriptor, &before); err != nil {
		return fileProbe{}, err
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Size < 0 || before.Ino > math.MaxInt64 {
		return fileProbe{}, ErrUnsupportedFile
	}

	prefix := make([]byte, minInt64(before.Size, prefixLimit))
	readBytes, err := io.ReadFull(file, prefix)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		clear(prefix)
		return fileProbe{}, err
	}
	if int64(readBytes) != int64(len(prefix)) {
		clear(prefix)
		return fileProbe{}, ErrChangedDuringScan
	}

	var after unix.Stat_t
	if err := unix.Fstat(fileDescriptor, &after); err != nil {
		clear(prefix)
		return fileProbe{}, err
	}
	if !sameFileStat(before, after) {
		clear(prefix)
		return fileProbe{}, ErrChangedDuringScan
	}

	mtimeNS := before.Mtim.Sec*1_000_000_000 + before.Mtim.Nsec
	if mtimeNS < 0 {
		clear(prefix)
		return fileProbe{}, ErrUnsupportedFile
	}
	return fileProbe{
		deviceID: strconv.FormatUint(uint64(uint32(before.Dev)), 10),
		inode:    int64(before.Ino), sizeBytes: before.Size, mtimeNS: mtimeNS, prefix: prefix,
	}, nil
}

func (root *osScanRoot) Close() error {
	if root == nil || root.file == nil {
		return nil
	}
	err := root.file.Close()
	root.file = nil
	return err
}

func identityFromDescriptor(fileDescriptor int) (rootIdentity, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fileDescriptor, &stat); err != nil {
		return rootIdentity{}, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Ino > math.MaxInt64 {
		return rootIdentity{}, ErrInvalidHome
	}
	return rootIdentity{
		deviceID: strconv.FormatUint(uint64(uint32(stat.Dev)), 10), inode: int64(stat.Ino),
	}, nil
}

func openAtNoFollow(rootDescriptor int, relativePath string, finalDirectory bool) (int, error) {
	relativePath = filepath.Clean(relativePath)
	if filepath.IsAbs(relativePath) || relativePath == ".." ||
		strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return -1, ErrUnsafeSource
	}
	current, err := unix.Dup(rootDescriptor)
	if err != nil {
		return -1, err
	}
	unix.CloseOnExec(current)
	if relativePath == "." {
		if !finalDirectory {
			_ = unix.Close(current)
			return -1, ErrUnsupportedFile
		}
		return current, nil
	}
	components := strings.Split(relativePath, string(filepath.Separator))
	for index, component := range components {
		if component == "" || component == "." || component == ".." {
			_ = unix.Close(current)
			return -1, ErrUnsafeSource
		}
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
		isLast := index == len(components)-1
		if !isLast || finalDirectory {
			flags |= unix.O_DIRECTORY
		} else {
			flags |= unix.O_NONBLOCK
		}
		next, openErr := unix.Openat(current, component, flags, 0)
		_ = unix.Close(current)
		if openErr != nil {
			return -1, openErr
		}
		current = next
	}
	return current, nil
}

func sameFileStat(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino && left.Mode == right.Mode &&
		left.Size == right.Size && left.Mtim == right.Mtim && left.Ctim == right.Ctim
}

func classifyRootOpenError(path string, err error) error {
	switch {
	case errors.Is(err, unix.ELOOP):
		return ErrUnsafeHome
	case errors.Is(err, unix.ENOTDIR):
		// Darwin reports ENOTDIR rather than ELOOP when O_NOFOLLOW and
		// O_DIRECTORY reject a final symlink. Lstat is used only to retain the
		// precise error contract; the failed descriptor is never trusted.
		info, lstatErr := os.Lstat(path)
		if lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return ErrUnsafeHome
		}
		return ErrInvalidHome
	case errors.Is(err, unix.ENOENT):
		return ErrInvalidHome
	default:
		return err
	}
}

func classifySourceOpenError(err error) error {
	switch {
	case errors.Is(err, unix.ELOOP):
		return ErrUnsafeSource
	case errors.Is(err, unix.ENOTDIR):
		return ErrChangedDuringScan
	default:
		return err
	}
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
