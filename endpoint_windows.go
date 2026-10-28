//go:build windows

package main

import (
	"fmt"
	"path/filepath"
)

func platformEndpoint(socket, pipe, runtimeDirectory string) (string, error) {
	if socket != "" || pipe == "" || !filepath.IsAbs(runtimeDirectory) || filepath.Clean(runtimeDirectory) != runtimeDirectory {
		return "", fmt.Errorf("invalid private Windows endpoint")
	}
	return pipe, nil
}
