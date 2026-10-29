//go:build windows

package appserver

import (
	"errors"
	"path/filepath"
	"strings"
)

// normalizeMetadataPath puts DOS paths returned by App Server in the same
// namespace as the confirmed Home. Go's Windows filesystem calls retain long
// path support without the display prefix. UNC and device namespaces are not
// local Codex Homes and must not trigger network/device metadata probes.
func normalizeMetadataPath(path string) (string, error) {
	if strings.HasPrefix(path, `\\?\`) {
		path = path[4:]
	}
	if !filepath.IsAbs(path) || len(filepath.VolumeName(path)) != 2 ||
		path[1] != ':' || strings.Contains(path[2:], ":") {
		return "", errors.New("unsupported App Server metadata path")
	}
	return filepath.Clean(path), nil
}
