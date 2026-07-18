package sparkle

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareFrameworkVerifiesAndReusesArchive(t *testing.T) {
	t.Parallel()

	archive := makeFrameworkArchive(t, "9.9.9")
	digest := fileSHA256(t, archive)
	cache := filepath.Join(t.TempDir(), "cache")
	script := filepath.Join("prepare_framework.sh")

	first := runScript(t, script, cache, "--test-fixture", "9.9.9", "file://"+archive, digest)
	want := filepath.Join(cache, "9.9.9", "framework", "Sparkle.framework")
	if strings.TrimSpace(first) != want {
		t.Fatalf("output=%q, want %q", first, want)
	}
	link, err := os.Readlink(filepath.Join(want, "Sparkle"))
	if err != nil {
		t.Fatalf("read framework symlink: %v", err)
	}
	if link != "Versions/Current/Sparkle" {
		t.Fatalf("framework symlink=%q", link)
	}
	binary := filepath.Join(want, "Versions", "B", "Sparkle")
	if err := os.WriteFile(binary, []byte("tampered"), 0o755); err != nil {
		t.Fatalf("tamper cached framework: %v", err)
	}
	second := runScript(t, script, cache, "--test-fixture", "9.9.9", "file://"+archive, digest)
	if second != first {
		t.Fatalf("cached output=%q, want %q", second, first)
	}
	restored, err := os.ReadFile(binary)
	if err != nil {
		t.Fatalf("read restored framework: %v", err)
	}
	if string(restored) != "fixture" {
		t.Fatalf("cached framework binary=%q, want trusted archive content", restored)
	}
}

func TestPrepareFrameworkRejectsWrongChecksum(t *testing.T) {
	t.Parallel()

	archive := makeFrameworkArchive(t, "9.9.9")
	cache := filepath.Join(t.TempDir(), "cache")
	command := exec.Command("bash", "prepare_framework.sh", cache, "--test-fixture", "9.9.9", "file://"+archive, strings.Repeat("0", 64))
	command.Env = os.Environ()
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("wrong checksum succeeded: %s", output)
	}
	if !strings.Contains(string(output), "checksum mismatch") {
		t.Fatalf("error=%s, want checksum mismatch", output)
	}
	if _, statErr := os.Stat(filepath.Join(cache, "9.9.9", "framework")); !os.IsNotExist(statErr) {
		t.Fatalf("invalid framework cache exists: %v", statErr)
	}
}

func TestPrepareFrameworkRejectsEnvironmentPinOverrides(t *testing.T) {
	t.Parallel()

	archive := makeFrameworkArchive(t, "9.9.9")
	cache := filepath.Join(t.TempDir(), "cache")
	command := exec.Command("bash", "prepare_framework.sh", cache)
	command.Env = append(os.Environ(),
		"SPARKLE_VERSION=9.9.9",
		"SPARKLE_URL=file://"+archive,
		"SPARKLE_SHA256="+fileSHA256(t, archive),
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("environment pin override succeeded: %s", output)
	}
	if !strings.Contains(string(output), "environment pin overrides are not supported") {
		t.Fatalf("error=%s, want environment override rejection", output)
	}
	if _, statErr := os.Stat(cache); !os.IsNotExist(statErr) {
		t.Fatalf("override created cache: %v", statErr)
	}
}

func makeFrameworkArchive(t *testing.T, version string) string {
	t.Helper()
	root := t.TempDir()
	framework := filepath.Join(root, "Sparkle.framework")
	resources := filepath.Join(framework, "Versions", "B", "Resources")
	if err := os.MkdirAll(resources, 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "https://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>CFBundleShortVersionString</key><string>%s</string></dict></plist>`, version)
	if err := os.WriteFile(filepath.Join(resources, "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}
	if err := os.WriteFile(filepath.Join(framework, "Versions", "B", "Sparkle"), []byte("fixture"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	if err := os.Symlink("B", filepath.Join(framework, "Versions", "Current")); err != nil {
		t.Fatalf("symlink current: %v", err)
	}
	if err := os.Symlink("Versions/Current/Sparkle", filepath.Join(framework, "Sparkle")); err != nil {
		t.Fatalf("symlink binary: %v", err)
	}
	archive := filepath.Join(t.TempDir(), "Sparkle.tar.xz")
	command := exec.Command("tar", "-cJf", archive, "-C", root, "./Sparkle.framework")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create archive: %v: %s", err, output)
	}
	return archive
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func runScript(t *testing.T, script, cache string, extra ...string) string {
	t.Helper()
	args := append([]string{script, cache}, extra...)
	command := exec.Command("bash", args...)
	command.Env = os.Environ()
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("prepare framework: %v: %s", err, output)
	}
	return string(output)
}
