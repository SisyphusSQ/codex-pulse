//go:build !windows

package appserver

func normalizeMetadataPath(path string) (string, error) { return path, nil }
