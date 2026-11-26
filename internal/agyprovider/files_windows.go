//go:build windows

package agyprovider

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"

	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
)

// pinSourceDatabase 在只读事务期间钉住 SQLite 主库及 WAL/SHM/Journal 文件句柄，
// 拒绝重解析点/符号链接，同时保留 Antigravity 写入端的共享写权限。
func pinSourceDatabase(path string) (func(), error) {
	if !filepath.IsAbs(path) || strings.Contains(filepath.Base(path), ":") {
		return nil, platformfs.ErrUnsafe
	}
	root, err := platformfs.OpenReadRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	var files []*os.File
	release := func() {
		for _, file := range files {
			_ = file.Close()
		}
		files = nil
		_ = root.Close()
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		name := filepath.Clean(path + suffix)
		if !strings.HasPrefix(name, `\\?\`) {
			if strings.HasPrefix(name, `\\`) {
				name = `\\?\UNC\` + name[2:]
			} else {
				name = `\\?\` + name
			}
		}
		wide, err := windows.UTF16PtrFromString(name)
		if err != nil {
			release()
			return nil, platformfs.ErrUnsafe
		}
		handle, err := windows.CreateFile(
			wide,
			windows.GENERIC_READ,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
			nil,
			windows.OPEN_EXISTING,
			windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS,
			0,
		)
		if err != nil {
			if suffix != "" && errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
				continue
			}
			release()
			return nil, err
		}
		file := os.NewFile(uintptr(handle), path+suffix)
		files = append(files, file)
		info, err := platformfs.InspectHandle(handle)
		if err != nil || info.Attributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
			release()
			return nil, platformfs.ErrUnsafe
		}
	}
	return release, nil
}

func openRegularFileReadOnly(path string) (*os.File, os.FileInfo, error) {
	if !filepath.IsAbs(path) || strings.Contains(filepath.Base(path), ":") {
		return nil, nil, platformfs.ErrUnsafe
	}
	root, err := platformfs.OpenReadRoot(filepath.Dir(path))
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	opened, err := root.OpenFile(filepath.Base(path), os.O_RDONLY)
	if err != nil {
		return nil, nil, err
	}
	info, err := opened.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = opened.Close()
		return nil, nil, platformfs.ErrUnsafe
	}
	return opened.File, info, nil
}

func readDirSafe(path string) ([]os.DirEntry, error) {
	if !filepath.IsAbs(path) {
		return nil, platformfs.ErrUnsafe
	}
	root, err := platformfs.OpenReadRoot(path)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.File.ReadDir(-1)
}
