package platformfs

import (
	"io"
	"io/fs"
)

// SourceFile is a read-only Agent source with an identity-bearing OS handle.
type SourceFile interface {
	fs.File
	io.Seeker
	Fd() uintptr
}

// SourceRoot exposes bounded relative source reads and no-follow metadata.
// It never applies Pulse's private ACL to an Agent's files.
type SourceRoot interface {
	Open(string) (SourceFile, error)
	Lstat(string) (fs.FileInfo, error)
	FS() fs.FS
	Close() error
}

type sourceFS struct{ root SourceRoot }

func (view sourceFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: ErrUnsafe}
	}
	return view.root.Open(name)
}
