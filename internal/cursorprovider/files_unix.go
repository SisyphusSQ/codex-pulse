//go:build !windows

package cursorprovider

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func pinSourceDatabase(path string) (func(), error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrCollector
	}
	return func() {}, nil
}

func walkTranscriptFiles(ctx context.Context, root string, visit func(string, *os.File) error) (bool, error) {
	info, err := os.Stat(root)
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, ErrCollector
	}
	var failure error
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			failure = walkErr
			return nil
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || filepath.Ext(entry.Name()) != ".jsonl" || !strings.Contains(filepath.ToSlash(path), "/agent-transcripts/") {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			failure = err
			return nil
		}
		defer file.Close()
		if err = visit(path, file); err != nil {
			failure = err
		}
		return nil
	})
	if err != nil {
		return true, err
	}
	return true, failure
}
