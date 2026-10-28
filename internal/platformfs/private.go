// Package platformfs 封装本机私有存储的平台权限、锁与持久发布操作。
package platformfs

import (
	"errors"
	"os"
	"path/filepath"
)

var ErrUnsafe = errors.New("private storage permissions or file identity are invalid")

// ErrReplaceCommitted 表示 canonical 已更换，后续清理失败时必须要求重启而非重复提交。
var ErrReplaceCommitted = errors.New("private file replacement committed with incomplete cleanup")

// PrivateMode 在 Unix 检查 mode，在 Windows 检查句柄的 owner、DACL、类型及链接。
func PrivateMode(path string, info os.FileInfo, mode os.FileMode) bool {
	return info != nil && info.Mode()&os.ModeSymlink == 0 && privateMode(path, info, mode)
}

// MkdirAll 只对新建的目标目录建立私有权限，不修改已有目录的权限。
func MkdirAll(path string, mode os.FileMode) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := validateAncestors(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	return Secure(path, mode)
}

// Mkdir 为 Preferences 的单层创建保留并发存在错误语义。
func Mkdir(path string, mode os.FileMode) error {
	if err := validateAncestors(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.Mkdir(path, mode); err != nil {
		return err
	}
	return Secure(path, mode)
}
