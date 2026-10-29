//go:build windows

package appserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsMetadataPathsShareConfirmedHomeNamespace(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	home := filepath.Join(root, "中文 Home # %")
	rollout := filepath.Join(home, "sessions", strings.Repeat("长路径", 55), "rollout.jsonl")
	if err := os.MkdirAll(filepath.Dir(rollout), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rollout, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, homePath := range []string{home, `\\?\` + home} {
		confirmed, err := canonicalConfirmedHome(homePath)
		if err != nil || confirmed != home {
			t.Fatalf("confirmed namespace mismatch: %v", err)
		}
		for _, sourcePath := range []string{rollout, `\\?\` + rollout} {
			actual, code := validatedRolloutPath(confirmed, sourcePath)
			if code != "" || actual != rollout {
				t.Fatalf("same physical Home rollout rejected: code=%q", code)
			}
		}
	}
	outside := filepath.Join(root, "outside.jsonl")
	if err := os.WriteFile(outside, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, code := validatedRolloutPath(home, `\\?\`+outside); code != "rollout_path_outside_home" {
		t.Fatalf("extended prefix bypassed Home boundary: %q", code)
	}
}

func TestWindowsMetadataRejectsNetworkAndDeviceNamespaces(t *testing.T) {
	t.Parallel()
	for _, candidate := range []string{`\\server\share\rollout.jsonl`, `\\?\UNC\server\share\rollout.jsonl`, `\\?\GLOBALROOT\Device\rollout.jsonl`, `\\.\pipe\rollout.jsonl`, `C:\rollout.jsonl:stream`} {
		if _, err := normalizeMetadataPath(candidate); err == nil {
			t.Fatal("non-local metadata path accepted")
		}
	}
}
