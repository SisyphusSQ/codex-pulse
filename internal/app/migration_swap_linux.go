//go:build linux

package app

import "golang.org/x/sys/unix"

func atomicSwapFiles(left, right string) error {
	return unix.Renameat2(unix.AT_FDCWD, left, unix.AT_FDCWD, right, unix.RENAME_EXCHANGE)
}
