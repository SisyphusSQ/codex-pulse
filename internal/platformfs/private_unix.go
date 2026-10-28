//go:build !windows

package platformfs

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func privateMode(_ string, info os.FileInfo, mode os.FileMode) bool {
	return info.Mode().Perm() == mode
}

func validateAncestors(string) error { return nil }

func UserDataDirectory() (string, error) { return os.UserConfigDir() }

// PinDirectory 的 Unix 路径继续沿用调用方的 os.Root/no-follow 保护。
func PinDirectory(string) (func(), error) { return func() {}, nil }

func RecoverExchange(string) error { return nil }

func Secure(path string, mode os.FileMode) error { return os.Chmod(path, mode) }

// OpenFile 保留 Unix 的 no-follow、nonblocking 及 close-on-exec 保护。
func OpenFile(path string, flags int, mode os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, mode)
}

func TryLock(file *os.File) (bool, error) {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}

func Unlock(file *os.File) error { return unix.Flock(int(file.Fd()), unix.LOCK_UN) }

func SyncDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}

func Rename(from, to string) error { return os.Rename(from, to) }

func RenameInRoot(root *os.Root, from, to string) error { return root.Rename(from, to) }

func PublishInRoot(root *os.Root, from, to string) (bool, error) { return false, root.Link(from, to) }

func OpenRootFile(root *os.Root, name string, flags int, mode os.FileMode) (*os.File, error) {
	return root.OpenFile(name, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK, mode)
}

// PublishNoReplace 返回临时名称是否已被移走。
func PublishNoReplace(from, to string) (bool, error) { return false, os.Link(from, to) }

func AvailableBytes(path string) (int64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

func SingleOwnedFile(file *os.File) bool {
	var stat syscall.Stat_t
	return syscall.Fstat(int(file.Fd()), &stat) == nil && stat.Nlink == 1 && stat.Uid == uint32(os.Geteuid())
}
