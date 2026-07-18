//go:build !darwin && !linux

package app

import "errors"

func atomicSwapFiles(_, _ string) error {
	return errors.New("atomic migration database exchange is unsupported on this platform")
}
