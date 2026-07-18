//go:build !darwin

package main

import "errors"

func atomicSwap(_, _ string) error {
	return errors.New("atomic directory swap requires macOS")
}
