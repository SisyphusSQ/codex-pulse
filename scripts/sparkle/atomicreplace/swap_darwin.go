//go:build darwin

package main

import "golang.org/x/sys/unix"

func atomicSwap(source, target string) error {
	return unix.RenameatxNp(unix.AT_FDCWD, source, unix.AT_FDCWD, target, unix.RENAME_SWAP)
}
