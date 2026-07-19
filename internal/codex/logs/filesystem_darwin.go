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
	"syscall"

	"golang.org/x/sys/unix"
)

type fileSystem interface {
	ConfirmRoot(path string, resolveAncestors bool) (rootIdentity, error)
	OpenRoot(path string, resolveAncestors bool) (scanRoot, error)
}

type scanRoot interface {
	Identity() rootIdentity
	ReadDir(relativePath string) ([]fs.DirEntry, error)
	Metadata(relativePath string) (fileMetadata, error)
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

type fileMetadata struct {
	deviceID  string
	inode     int64
	sizeBytes int64
	mtimeNS   int64
	regular   bool
	directory bool
	symlink   bool
}

type metadataDirectoryEntry struct {
	fs.DirEntry
	metadata fileMetadata
}

func (entry metadataDirectoryEntry) snapshotMetadata() fileMetadata {
	return entry.metadata
}

type osFileSystem struct{}

func (osFileSystem) ConfirmRoot(path string, resolveAncestors bool) (rootIdentity, error) {
	path, err := rootPathForOpen(path, resolveAncestors)
	if err != nil {
		return rootIdentity{}, err
	}
	root, err := openOSRoot(path)
	if err != nil {
		return rootIdentity{}, classifyRootOpenError(path, err)
	}
	defer func() { _ = root.Close() }()
	return root.Identity(), nil
}

func (osFileSystem) OpenRoot(path string, resolveAncestors bool) (scanRoot, error) {
	path, err := rootPathForOpen(path, resolveAncestors)
	if err != nil {
		return nil, err
	}
	root, err := openOSRoot(path)
	if err != nil {
		return nil, classifyRootOpenError(path, err)
	}
	return root, nil
}

func rootPathForOpen(path string, resolveAncestors bool) (string, error) {
	if resolveAncestors {
		resolved, _, err := canonicalProbeHome(path)
		return resolved, err
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return "", ErrInvalidHome
	}
	return path, nil
}

type osScanRoot struct {
	file     *os.File
	identity rootIdentity
}

func openOSRoot(path string) (*osScanRoot, error) {
	fileDescriptor, err := openAbsoluteDirectoryNoFollow(path)
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
	stableEntries := make([]fs.DirEntry, 0, len(entries))
	for _, entry := range entries {
		metadata, metadataErr := metadataAt(fileDescriptor, entry.Name())
		if metadataErr != nil {
			if errors.Is(metadataErr, fs.ErrNotExist) {
				return nil, ErrChangedDuringScan
			}
			return nil, metadataErr
		}
		stableEntries = append(stableEntries, metadataDirectoryEntry{
			DirEntry: entry, metadata: metadata,
		})
	}
	sort.Slice(stableEntries, func(left, right int) bool {
		return stableEntries[left].Name() < stableEntries[right].Name()
	})
	return stableEntries, nil
}

func (root *osScanRoot) Metadata(relativePath string) (fileMetadata, error) {
	if root == nil || root.file == nil {
		return fileMetadata{}, ErrHomeChanged
	}
	relativePath = filepath.Clean(relativePath)
	if filepath.IsAbs(relativePath) || relativePath == "." || relativePath == ".." ||
		strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return fileMetadata{}, ErrUnsafeSource
	}
	parentPath := filepath.Dir(relativePath)
	baseName := filepath.Base(relativePath)
	parentDescriptor, err := openAtNoFollow(int(root.file.Fd()), parentPath, true)
	if err != nil {
		return fileMetadata{}, classifySourceOpenError(err)
	}
	defer func() { _ = unix.Close(parentDescriptor) }()

	metadata, err := metadataAt(parentDescriptor, baseName)
	if err != nil {
		return fileMetadata{}, classifySourceOpenError(err)
	}
	if metadata.symlink {
		return fileMetadata{}, ErrUnsafeSource
	}
	return metadata, nil
}

func metadataAt(parentDescriptor int, baseName string) (fileMetadata, error) {
	var stat unix.Stat_t
	if err := unix.Fstatat(parentDescriptor, baseName, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fileMetadata{}, err
	}
	if stat.Size < 0 || stat.Ino > math.MaxInt64 {
		return fileMetadata{}, ErrUnsupportedFile
	}
	fileType := stat.Mode & unix.S_IFMT
	mtimeNS := stat.Mtim.Sec*1_000_000_000 + stat.Mtim.Nsec
	if mtimeNS < 0 {
		return fileMetadata{}, ErrUnsupportedFile
	}
	return fileMetadata{
		deviceID:  strconv.FormatUint(uint64(uint32(stat.Dev)), 10),
		inode:     int64(stat.Ino),
		sizeBytes: stat.Size,
		mtimeNS:   mtimeNS,
		regular:   fileType == unix.S_IFREG,
		directory: fileType == unix.S_IFDIR,
		symlink:   fileType == unix.S_IFLNK,
	}, nil
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

func identityFromFileInfo(info fs.FileInfo) (rootIdentity, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Ino > math.MaxInt64 {
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

func openAbsoluteDirectoryNoFollow(path string) (int, error) {
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return -1, ErrUnsafeHome
	}
	rootDescriptor, err := unix.Open(
		string(filepath.Separator), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0,
	)
	if err != nil {
		return -1, err
	}
	relativePath := strings.TrimPrefix(path, string(filepath.Separator))
	fileDescriptor, openErr := openAtNoFollow(rootDescriptor, relativePath, true)
	_ = unix.Close(rootDescriptor)
	return fileDescriptor, openErr
}

func sameFileStat(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino && left.Mode == right.Mode &&
		left.Size == right.Size && left.Mtim == right.Mtim && left.Ctim == right.Ctim
}

func classifyRootOpenError(path string, err error) error {
	switch {
	case errors.Is(err, ErrUnsafeSource), errors.Is(err, ErrUnsafeHome):
		return ErrUnsafeHome
	case errors.Is(err, unix.ELOOP):
		return ErrUnsafeHome
	case errors.Is(err, unix.ENOTDIR):
		// Darwin reports ENOTDIR when any O_NOFOLLOW|O_DIRECTORY component
		// becomes a symlink between canonicalization and descriptor walking.
		return ErrUnsafeHome
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
