//go:build windows

package index

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"golang.org/x/sys/windows"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/homeidentity"
	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
)

func openRoot(home string) (*platformfs.ReadRoot, rootVersion, error) {
	root, err := platformfs.OpenReadRoot(home)
	if err != nil {
		return nil, rootVersion{}, errors.Join(ErrInvalidHome, err)
	}
	identity, err := homeidentity.FromDescriptor(int(root.Fd()))
	if err != nil {
		root.Close()
		return nil, rootVersion{}, err
	}
	return root, rootVersion{deviceID: identity.DeviceID, inode: uint64(identity.Inode)}, nil
}

func (indexFile *IndexFile) openRoot() (*platformfs.ReadRoot, error) {
	if indexFile == nil || indexFile.home == "" {
		return nil, ErrInvalidHome
	}
	root, version, err := openRoot(indexFile.home)
	if err != nil {
		return nil, err
	}
	if version != indexFile.root {
		root.Close()
		return nil, ErrHomeChanged
	}
	return root, nil
}

func readIndexRoot(ctx context.Context, root *platformfs.ReadRoot) (IndexRead, error) {
	file, err := root.OpenFile(sessionIndexFilename, os.O_RDONLY)
	if errors.Is(err, fs.ErrNotExist) {
		return IndexRead{}, nil
	}
	if err != nil {
		if errors.Is(err, platformfs.ErrUnsafe) {
			return IndexRead{}, ErrUnsafeIndex
		}
		return IndexRead{}, fmt.Errorf("open session index: %w", err)
	}
	defer file.Close()
	return readOpenedIndex(ctx, file.File)
}

func statIndexFile(file *os.File) (indexFileStatus, error) {
	info, err := platformfs.InspectHandle(windows.Handle(file.Fd()))
	if err != nil {
		return indexFileStatus{}, err
	}
	identity, err := homeidentity.FromWindowsFileInfo(info)
	if err != nil {
		return indexFileStatus{}, err
	}
	return indexFileStatus{deviceID: identity.DeviceID, inode: identity.Inode, size: info.SizeBytes,
		mtimeNS: info.MTimeNS, changeNS: info.ChangeNS, attributes: info.Attributes,
		regular: info.Attributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0}, nil
}

func (indexFile *IndexFile) appendExistingIndexRoot(ctx context.Context, root *platformfs.ReadRoot, expected FileVersion, payload []byte) (appendProof, error) {
	file, err := root.OpenFile(sessionIndexFilename, os.O_RDWR|os.O_APPEND)
	if err != nil {
		if errors.Is(err, platformfs.ErrUnsafe) {
			return appendProof{}, ErrUnsafeIndex
		}
		if errors.Is(err, fs.ErrNotExist) {
			return appendProof{}, ErrPlanDrift
		}
		return appendProof{}, fmt.Errorf("open session index for append: %w", err)
	}
	defer file.Close()
	read, err := readOpenedIndex(ctx, file.File)
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
	if err := appendAndSync(ctx, file.File, payload); err != nil {
		return appendProof{}, err
	}
	return proof, nil
}

func (indexFile *IndexFile) createIndexWithAppendRoot(ctx context.Context, root *platformfs.ReadRoot, payload []byte) (appendProof, error) {
	file, err := root.OpenFile(sessionIndexFilename, os.O_WRONLY|os.O_APPEND|os.O_CREATE|os.O_EXCL)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return appendProof{}, ErrPlanDrift
		}
		return appendProof{}, fmt.Errorf("create session index: %w", err)
	}
	defer file.Close()
	if indexFile.afterFinalCheck != nil {
		indexFile.afterFinalCheck()
	}
	if err := appendAndSync(ctx, file.File, payload); err != nil {
		return appendProof{}, err
	}
	if err := indexFile.syncRoot(int(root.Fd())); err != nil {
		return appendProof{}, err
	}
	return appendedContentProof(nil, payload), nil
}

func syncDirectoryPath(path string) error { return platformfs.SyncDirectory(path) }

func syncRootDescriptor(fd int) error {
	// Windows 无目录 fsync；新 index 以 WRITE_THROUGH 创建，正文由 FlushFileBuffers
	// 持久化。这里只确认仍持有同一个无 reparse 目录，不伪造目录 flush 证据。
	info, err := platformfs.InspectHandle(windows.Handle(fd))
	if err != nil || info.Attributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return errors.Join(ErrInvalidHome, err)
	}
	return nil
}
