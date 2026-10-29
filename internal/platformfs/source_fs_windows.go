//go:build windows

package platformfs

import (
	"io/fs"
	"os"
)

type windowsSourceRoot struct{ *ReadRoot }

// OpenSourceRoot pins the source directory and refuses reparse ancestors.
func OpenSourceRoot(path string) (SourceRoot, error) {
	root, err := OpenReadRoot(path)
	if err != nil {
		return nil, err
	}
	return &windowsSourceRoot{root}, nil
}

func (root *windowsSourceRoot) Lstat(name string) (fs.FileInfo, error) {
	if name == "." {
		return root.Stat()
	}
	file, err := root.OpenMetadata(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return file.Stat()
}

func (root *windowsSourceRoot) Open(name string) (SourceFile, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return root.OpenDirectory(name)
	}
	if !info.Mode().IsRegular() {
		return nil, ErrUnsafe
	}
	return root.OpenFile(name, os.O_RDONLY)
}

func (root *windowsSourceRoot) FS() fs.FS { return sourceFS{root} }
