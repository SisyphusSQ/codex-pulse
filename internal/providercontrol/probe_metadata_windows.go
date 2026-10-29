//go:build windows

package providercontrol

import (
	"os"
	"path/filepath"

	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
)

func probeMetadata(path string) (os.FileInfo, error) {
	root, err := platformfs.OpenReadRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.OpenMetadata(filepath.Base(path))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return file.Stat()
}
