//go:build windows

package platformfs

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsSourceFSBoundsDirectoriesAndNeverCreatesFiles(t *testing.T) {
	rootPath := t.TempDir()
	directory := filepath.Join(rootPath, "中文 # %")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "sample.jsonl"), []byte("fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := OpenSourceRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	file, err := root.Open("中文 # %/sample.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil || string(content) != "fixture\n" {
		t.Fatal("bounded read failed")
	}
	if _, err := root.Open("missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("missing read changed classification")
	}
	for _, name := range []string{"../outside", `..\outside`, "中文 # %/sample.jsonl:stream"} {
		if _, err := root.Open(name); err == nil {
			t.Fatal("unsafe source path accepted")
		}
	}
	if _, err := os.Stat(filepath.Join(rootPath, "missing")); !os.IsNotExist(err) {
		t.Fatal("read created a file")
	}
	count := 0
	if err := fs.WalkDir(root.FS(), ".", func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			count++
		}
		return err
	}); err != nil || count != 1 {
		t.Fatal("source FS traversal failed")
	}
	outside := t.TempDir()
	junction := filepath.Join(rootPath, "escape")
	createTestJunction(t, junction, outside)
	if _, err := root.Open("escape"); !errors.Is(err, ErrUnsafe) {
		t.Fatal("source FS followed a junction")
	}
	if _, err := OpenSourceRoot(junction); !errors.Is(err, ErrUnsafe) {
		t.Fatal("configured junction root was accepted")
	}
}
