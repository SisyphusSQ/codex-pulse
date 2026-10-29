//go:build !windows

package providercontrol

import "os"

func probeMetadata(path string) (os.FileInfo, error) { return os.Lstat(path) }
