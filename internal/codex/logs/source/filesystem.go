package source

import (
	"io/fs"
	"path/filepath"
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
	deviceID        string
	inode           int64
	runtimeDeviceID string
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

func samePersistentRootIdentity(left, right rootIdentity) bool {
	return left.deviceID == right.deviceID && left.inode == right.inode
}

func sameObservedRootIdentity(observed, opened rootIdentity) bool {
	return observed.runtimeDeviceID != "" &&
		observed.runtimeDeviceID == opened.runtimeDeviceID &&
		observed.inode == opened.inode
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
