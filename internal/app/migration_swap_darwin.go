//go:build darwin

package app

import "golang.org/x/sys/unix"

func atomicSwapFiles(left, right string) error {
	return unix.RenameatxNp(unix.AT_FDCWD, left, unix.AT_FDCWD, right, unix.RENAME_SWAP)
}
