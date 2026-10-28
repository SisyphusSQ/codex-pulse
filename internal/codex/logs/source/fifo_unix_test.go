//go:build !windows

package source

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func makeFIFOFixture(_ *testing.T, path string, mode uint32) error { return unix.Mkfifo(path, mode) }

func restrictFixtureContentRead(_ *testing.T, path string) error { return os.Chmod(path, 0) }
