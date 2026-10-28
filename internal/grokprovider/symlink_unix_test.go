//go:build !windows

package grokprovider

import "testing"

func skipUnavailableSymlink(t *testing.T, _ error) { t.Helper() }
