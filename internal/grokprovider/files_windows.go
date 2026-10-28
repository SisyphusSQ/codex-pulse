//go:build windows

package grokprovider

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/sys/windows"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/homeidentity"
	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
)

func fileIdentity(path string, _ os.FileInfo) (string, bool) {
	root, err := platformfs.OpenReadRoot(filepath.Dir(path))
	if err != nil {
		return "", false
	}
	defer root.Close()
	file, err := root.OpenMetadata(filepath.Base(path))
	if err != nil {
		return "", false
	}
	defer file.Close()
	info, err := platformfs.InspectHandle(windows.Handle(file.Fd()))
	if err != nil {
		return "", false
	}
	identity, err := homeidentity.FromWindowsFileInfo(info)
	return identity.DeviceID, err == nil
}

func readRegularAuthFile(path string) ([]byte, error) {
	root, err := platformfs.OpenReadRoot(filepath.Dir(path))
	if err != nil {
		return nil, ErrAuthUnavailable
	}
	defer root.Close()
	file, err := root.OpenFile(filepath.Base(path), os.O_RDONLY)
	if err != nil {
		return nil, ErrAuthUnavailable
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxAuthResponseBytes+1))
	if err != nil || len(content) > maxAuthResponseBytes {
		clear(content)
		return nil, ErrAuthUnavailable
	}
	return content, nil
}

func openAuthLockFile(path string) (*os.File, func() error, error) {
	root, err := platformfs.OpenReadRoot(filepath.Dir(path))
	if err != nil {
		return nil, nil, err
	}
	file, err := root.OpenFile(filepath.Base(path), os.O_RDWR|os.O_CREATE)
	if err != nil {
		root.Close()
		return nil, nil, err
	}
	closeFile := func() error { return errors.Join(file.Close(), root.Close()) }
	if err := platformfs.Secure(path, 0o600); err != nil || !platformfs.SingleOwnedFile(file.File) {
		closeFile()
		return nil, nil, ErrAuthRefreshFailed
	}
	return file.File, closeFile, nil
}

func writeAuthFileAtomic(path string, content []byte) error {
	root, err := platformfs.OpenReadRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	temporary := filepath.Join(root.Path, ".auth.json."+uuid.NewString()+".tmp")
	file, err := platformfs.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(temporary)
	if count, err := file.Write(content); err != nil || count != len(content) {
		file.Close()
		if err == nil {
			err = io.ErrShortWrite
		}
		return err
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		return err
	}
	from, err := authNativePath(temporary)
	if err != nil {
		return err
	}
	to, err := authNativePath(path)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func authNativePath(path string) (*uint16, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrAuthRefreshFailed
	}
	path = filepath.Clean(path)
	if !strings.HasPrefix(path, `\\?\`) {
		if strings.HasPrefix(path, `\\`) {
			path = `\\?\UNC\` + path[2:]
		} else {
			path = `\\?\` + path
		}
	}
	return windows.UTF16PtrFromString(path)
}
