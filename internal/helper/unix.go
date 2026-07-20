package helper

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

const maxUnixSocketPathBytes = 103

var ErrUnsafeUnixSocket = errors.New("helper unix socket is unsafe")

// ListenUnix 在已存在的私有目录中创建 UDS，并返回幂等清理函数。
func ListenUnix(socketPath string) (net.Listener, func() error, error) {
	if !filepath.IsAbs(socketPath) || filepath.Clean(socketPath) != socketPath ||
		len([]byte(socketPath)) > maxUnixSocketPathBytes {
		return nil, nil, ErrUnsafeUnixSocket
	}
	parent := filepath.Dir(socketPath)
	before, err := privateDirectoryIdentity(parent)
	if err != nil {
		return nil, nil, err
	}
	if err := removeSafeResidualSocket(socketPath); err != nil {
		return nil, nil, err
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: listen", ErrUnsafeUnixSocket)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = listener.Close()
		_ = removeCurrentSocket(socketPath)
		return nil, nil, fmt.Errorf("%w: set socket permissions", ErrUnsafeUnixSocket)
	}
	after, err := privateDirectoryIdentity(parent)
	if err != nil || before != after {
		_ = listener.Close()
		_ = removeCurrentSocket(socketPath)
		return nil, nil, fmt.Errorf("%w: parent changed", ErrUnsafeUnixSocket)
	}

	var cleanupOnce sync.Once
	var cleanupErr error
	cleanup := func() error {
		cleanupOnce.Do(func() {
			closeErr := listener.Close()
			removeErr := removeCurrentSocket(socketPath)
			cleanupErr = errors.Join(closeErr, removeErr)
		})
		return cleanupErr
	}
	return listener, cleanup, nil
}

type directoryIdentity struct {
	device uint64
	inode  uint64
}

func privateDirectoryIdentity(path string) (directoryIdentity, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return directoryIdentity{}, fmt.Errorf("%w: inspect parent", ErrUnsafeUnixSocket)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return directoryIdentity{}, fmt.Errorf("%w: invalid parent mode", ErrUnsafeUnixSocket)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) {
		return directoryIdentity{}, fmt.Errorf("%w: invalid parent owner", ErrUnsafeUnixSocket)
	}
	return directoryIdentity{device: uint64(stat.Dev), inode: stat.Ino}, nil
}

func removeSafeResidualSocket(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 {
		return ErrUnsafeUnixSocket
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) {
		return ErrUnsafeUnixSocket
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("%w: remove residual socket", ErrUnsafeUnixSocket)
	}
	return nil
}

func removeCurrentSocket(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 {
		return ErrUnsafeUnixSocket
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%w: remove socket", ErrUnsafeUnixSocket)
	}
	return nil
}
