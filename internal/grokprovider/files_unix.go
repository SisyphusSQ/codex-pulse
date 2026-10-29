//go:build !windows

package grokprovider

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
)

func readRegularAuthFile(path string) ([]byte, error) {
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrAuthUnavailable
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		_ = unix.Close(descriptor)
		return nil, ErrAuthUnavailable
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrAuthUnavailable
	}
	content, err := io.ReadAll(io.LimitReader(file, maxAuthResponseBytes+1))
	if err != nil || len(content) > maxAuthResponseBytes {
		return nil, ErrAuthUnavailable
	}
	return content, nil
}

func openAuthLockFile(path string) (*os.File, func() error, error) {
	descriptor, err := unix.Open(
		path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600,
	)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		_ = unix.Close(descriptor)
		return nil, nil, ErrAuthRefreshFailed
	}
	info, err := file.Stat()
	stat, statOK := infoSyscallStat(info)
	if err != nil || !info.Mode().IsRegular() || !statOK || stat.Nlink != 1 {
		_ = file.Close()
		return nil, nil, ErrAuthRefreshFailed
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	return file, file.Close, nil
}

func infoSyscallStat(info os.FileInfo) (*syscall.Stat_t, bool) {
	if info == nil {
		return nil, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return stat, ok
}

func writeAuthFileAtomic(path string, content []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".auth.json.*.tmp")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer func() { _ = os.Remove(temporary) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := io.Copy(file, bytes.NewReader(content)); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("replace auth file: %w", err)
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func fileIdentity(_ platformfs.SourceFile, info os.FileInfo) (string, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("unix:%d:%d", stat.Dev, stat.Ino), true
}
