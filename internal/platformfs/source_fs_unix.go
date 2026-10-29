//go:build !windows

package platformfs

import (
	"io/fs"
	"os"
	"path/filepath"
)

type unixSourceRoot struct{ *os.Root }

// OpenSourceRoot opens an absolute read-only source boundary without following
// a link at the configured root.
func OpenSourceRoot(path string) (SourceRoot, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrUnsafe
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrUnsafe
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	return &unixSourceRoot{root}, nil
}

func (root *unixSourceRoot) Open(name string) (SourceFile, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
		return nil, ErrUnsafe
	}
	return root.Root.Open(name)
}

func (root *unixSourceRoot) FS() fs.FS { return sourceFS{root} }
