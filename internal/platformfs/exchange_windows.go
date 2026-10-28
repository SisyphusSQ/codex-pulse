//go:build windows

package platformfs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var replaceFile = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReplaceFileW")

// Exchange 使用 NTFS ReplaceFile 的原子替换及独立备份，绝不先移走 canonical。
// 调用方已经关闭 SQLite 并持久保存完整备份；旧文件先留在同目录私有临时名。
func Exchange(canonical, ready string) error {
	if filepath.Dir(canonical) != filepath.Dir(ready) || canonical == ready {
		return ErrUnsafe
	}
	for _, path := range []string{canonical, ready} {
		info, err := os.Lstat(path)
		if err != nil || !PrivateMode(path, info, 0o600) {
			return ErrUnsafe
		}
	}
	before, err := os.Stat(ready)
	if err != nil {
		return err
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	backup := filepath.Join(filepath.Dir(canonical), exchangePrefix(canonical)+hex.EncodeToString(nonce[:])+".db")
	canonicalName, err := nativePath(canonical)
	if err != nil {
		return err
	}
	readyName, err := nativePath(ready)
	if err != nil {
		return err
	}
	backupName, err := nativePath(backup)
	if err != nil {
		return err
	}
	result, _, callErr := replaceFile.Call(uintptr(unsafe.Pointer(canonicalName)), uintptr(unsafe.Pointer(readyName)), uintptr(unsafe.Pointer(backupName)), 0, 0, 0)
	runtime.KeepAlive(canonicalName)
	runtime.KeepAlive(readyName)
	runtime.KeepAlive(backupName)
	if result == 0 {
		if after, err := os.Stat(canonical); err == nil && os.SameFile(before, after) {
			return errors.Join(ErrReplaceCommitted, callErr)
		}
		// ReplaceFile 的 1177 明确可能已把原文件移动到 backup；按实际存在性
		// 恢复旧 canonical，失败时保留可在下次 Open 恢复的私有备份名称。
		if errors.Is(callErr, windows.ERROR_UNABLE_TO_MOVE_REPLACEMENT_2) {
			return errors.Join(callErr, RecoverExchange(canonical))
		}
		return callErr
	}
	file, err := OpenFile(canonical, os.O_RDWR, 0o600)
	if err != nil {
		return errors.Join(ErrReplaceCommitted, err)
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		return errors.Join(ErrReplaceCommitted, err)
	}
	if err := Rename(backup, ready); err != nil {
		return errors.Join(ErrReplaceCommitted, err)
	}
	return nil
}

func exchangePrefix(canonical string) string {
	return ".migration-swap-" + filepath.Base(canonical) + "-"
}

// RecoverExchange 只在 canonical 缺失且存在唯一私有替换备份时恢复原文件。
// 已存在的数据库永不覆盖，多份或不安全的残留必须停在 recovery 边界。
func RecoverExchange(canonical string) error {
	if _, err := os.Lstat(canonical); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(canonical)
	info, err := os.Stat(parent)
	if err != nil || !PrivateMode(parent, info, 0o700) {
		return ErrUnsafe
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return err
	}
	var backup string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), exchangePrefix(canonical)) || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		path := filepath.Join(parent, entry.Name())
		info, err := entry.Info()
		if backup != "" || err != nil || !PrivateMode(path, info, 0o600) {
			return ErrUnsafe
		}
		backup = path
	}
	if backup == "" {
		return nil
	}
	_, err = PublishNoReplace(backup, canonical)
	return err
}
