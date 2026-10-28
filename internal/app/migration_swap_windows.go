//go:build windows

package app

import "github.com/SisyphusSQ/codex-pulse/internal/platformfs"

func atomicSwapFiles(canonical, ready string) error { return platformfs.Exchange(canonical, ready) }
