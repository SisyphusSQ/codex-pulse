//go:build windows

package source

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"golang.org/x/sys/windows"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/homeidentity"
	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
)

type osScanRoot struct {
	root     *platformfs.ReadRoot
	identity rootIdentity
}

func openOSRoot(path string) (*osScanRoot, error) {
	root, err := platformfs.OpenReadRoot(path)
	if err != nil {
		return nil, err
	}
	identity, err := identityFromDescriptor(int(root.Fd()))
	if err != nil {
		root.Close()
		return nil, err
	}
	return &osScanRoot{root: root, identity: identity}, nil
}

func (root *osScanRoot) Identity() rootIdentity { return root.identity }

func (root *osScanRoot) Close() error {
	if root == nil || root.root == nil {
		return nil
	}
	err := root.root.Close()
	root.root = nil
	return err
}

func (root *osScanRoot) ReadDir(relative string) ([]fs.DirEntry, error) {
	if root == nil || root.root == nil {
		return nil, ErrHomeChanged
	}
	directory, err := root.root.OpenDirectory(relative)
	if err != nil {
		return nil, classifySourceOpenError(err)
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	stable := make([]fs.DirEntry, 0, len(entries))
	for _, entry := range entries {
		metadata, err := root.Metadata(filepath.Join(relative, entry.Name()))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrChangedDuringScan
		}
		if err != nil {
			return nil, err
		}
		stable = append(stable, metadataDirectoryEntry{DirEntry: entry, metadata: metadata})
	}
	sort.Slice(stable, func(i, j int) bool { return stable[i].Name() < stable[j].Name() })
	return stable, nil
}

func windowsMetadata(info platformfs.WindowsFileInfo) (fileMetadata, error) {
	identity, err := homeidentity.FromWindowsFileInfo(info)
	if err != nil || info.SizeBytes < 0 {
		return fileMetadata{}, ErrUnsupportedFile
	}
	return fileMetadata{
		deviceID: identity.DeviceID, inode: identity.Inode, sizeBytes: info.SizeBytes, mtimeNS: info.MTimeNS,
		regular:   info.Attributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0,
		directory: info.Attributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0,
	}, nil
}

func (root *osScanRoot) Metadata(relative string) (fileMetadata, error) {
	if root == nil || root.root == nil {
		return fileMetadata{}, ErrHomeChanged
	}
	file, err := root.root.OpenMetadata(relative)
	if err != nil {
		return fileMetadata{}, classifySourceOpenError(err)
	}
	defer file.Close()
	info, err := platformfs.InspectHandle(windows.Handle(file.Fd()))
	if err != nil {
		return fileMetadata{}, err
	}
	return windowsMetadata(info)
}

func (root *osScanRoot) Probe(relative string, limit int64) (fileProbe, error) {
	file, closeFile, err := openSnapshotFile(root, relative)
	if err != nil {
		return fileProbe{}, classifySourceOpenError(err)
	}
	defer closeFile()
	before, err := snapshotFileStat(file)
	if err != nil {
		return fileProbe{}, err
	}
	metadata, err := windowsMetadata(before)
	if err != nil || !metadata.regular || limit < 0 {
		return fileProbe{}, ErrUnsupportedFile
	}
	prefix := make([]byte, minInt64(metadata.sizeBytes, limit))
	n, err := io.ReadFull(file, prefix)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		clear(prefix)
		return fileProbe{}, err
	}
	after, statErr := snapshotFileStat(file)
	if statErr != nil || n != len(prefix) || !sameFileStat(before, after) {
		clear(prefix)
		return fileProbe{}, ErrChangedDuringScan
	}
	return fileProbe{deviceID: metadata.deviceID, inode: metadata.inode, sizeBytes: metadata.sizeBytes, mtimeNS: metadata.mtimeNS, prefix: prefix}, nil
}

func identityFromDescriptor(fd int) (rootIdentity, error) {
	identity, err := homeidentity.FromDescriptor(fd)
	if err != nil {
		return rootIdentity{}, err
	}
	return rootIdentity{deviceID: identity.DeviceID, runtimeDeviceID: identity.DeviceID, inode: identity.Inode}, nil
}

func identityFromPathInfo(path string, _ fs.FileInfo) (rootIdentity, error) {
	root, err := platformfs.OpenReadRoot(path)
	if err != nil {
		return rootIdentity{}, classifyRootOpenError(path, err)
	}
	defer root.Close()
	return identityFromDescriptor(int(root.Fd()))
}

func openSnapshotFile(root *osScanRoot, relative string) (*os.File, func() error, error) {
	if root == nil || root.root == nil {
		return nil, nil, ErrHomeChanged
	}
	file, err := root.root.OpenFile(relative, os.O_RDONLY)
	if err != nil {
		return nil, nil, err
	}
	return file.File, file.Close, nil
}

func snapshotFileStat(file *os.File) (platformfs.WindowsFileInfo, error) {
	return platformfs.InspectHandle(windows.Handle(file.Fd()))
}

func sameFileStat(left, right platformfs.WindowsFileInfo) bool { return left == right }

func snapshotMatchesStat(snapshot Snapshot, info platformfs.WindowsFileInfo) bool {
	metadata, err := windowsMetadata(info)
	return err == nil && metadata.regular && snapshot.Fingerprint.DeviceID == metadata.deviceID &&
		snapshot.Fingerprint.Inode == metadata.inode && snapshot.Fingerprint.SizeBytes == metadata.sizeBytes &&
		snapshot.Fingerprint.MTimeNS == metadata.mtimeNS
}

func classifyRootOpenError(_ string, err error) error {
	if errors.Is(err, platformfs.ErrUnsafe) {
		return ErrUnsafeHome
	}
	if errors.Is(err, fs.ErrNotExist) {
		return ErrInvalidHome
	}
	return err
}

func classifySourceOpenError(err error) error {
	if errors.Is(err, platformfs.ErrUnsafe) {
		return ErrUnsafeSource
	}
	return err
}
