package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRunPublishesOrAtomicallySwapsReleaseDirectory(t *testing.T) {
	dist := filepath.Join(t.TempDir(), "dist")
	if err := os.Mkdir(dist, 0o700); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(dist, ".update.staging.test")
	target := filepath.Join(dist, "update")
	writeMarker(t, staging, "new")
	if err := run(staging, target); err != nil {
		t.Fatal(err)
	}
	assertMarker(t, target, "new")

	if runtime.GOOS != "darwin" {
		return
	}
	writeMarker(t, staging, "replacement")
	if err := run(staging, target); err != nil {
		t.Fatal(err)
	}
	assertMarker(t, target, "replacement")
	assertMarker(t, staging, "new")
}

func TestRunRejectsUnexpectedPathsAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "staging")
	target := filepath.Join(dir, "update")
	writeMarker(t, source, "new")
	if err := run(source, target); err == nil {
		t.Fatal("unexpected source name accepted")
	}
	link := filepath.Join(dir, ".update.staging.link")
	if err := os.Symlink(source, link); err != nil {
		t.Fatal(err)
	}
	if err := run(link, target); err == nil {
		t.Fatal("symlink source accepted")
	}
}

func writeMarker(t *testing.T, dir, value string) {
	t.Helper()
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "marker"), []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertMarker(t *testing.T, dir, expected string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "marker"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != expected {
		t.Fatalf("marker=%q, want %q", data, expected)
	}
}
