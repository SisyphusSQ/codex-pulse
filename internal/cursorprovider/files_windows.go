//go:build windows

package cursorprovider

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/windows"

	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
)

// Keep the directory and existing SQLite files non-replaceable for the read
// transaction, while retaining write sharing for Cursor's WAL writer.
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
		handle, err := windows.CreateFile(wide, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
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

func walkTranscriptFiles(ctx context.Context, path string, visit func(string, *os.File) error) (bool, error) {
	root, err := platformfs.OpenReadRoot(path)
	if err != nil {
		return false, err
	}
	defer root.Close()
	var walk func(*os.File, string) error
	walk = func(directory *os.File, relative string) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		entries, err := directory.ReadDir(-1)
		if err != nil {
			return err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		var failure error
		for _, entry := range entries {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			name := filepath.Join(relative, entry.Name())
			if entry.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 {
				failure = platformfs.ErrUnsafe
				continue
			}
			if entry.IsDir() {
				child, err := root.OpenDirectory(name)
				if err != nil {
					failure = err
					continue
				}
				err = walk(child.File, name)
				_ = child.Close()
				if err != nil {
					failure = err
				}
				continue
			}
			if filepath.Ext(entry.Name()) != ".jsonl" || !strings.Contains("/"+filepath.ToSlash(name), "/agent-transcripts/") {
				continue
			}
			file, err := root.OpenFile(name, os.O_RDONLY)
			if err != nil {
				failure = err
				continue
			}
			err = visit(filepath.Join(path, name), file.File)
			_ = file.Close()
			if err != nil {
				failure = err
			}
		}
		return failure
	}
	return true, walk(root.File, "")
}
