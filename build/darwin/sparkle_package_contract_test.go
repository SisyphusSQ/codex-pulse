package main

import (
	"os"
	"strings"
	"testing"
)

func TestPackageBuildIncludesPinnedSparkle(t *testing.T) {
	t.Parallel()

	taskfile := readContractFile(t, "Taskfile.yml")
	for _, required := range []string{
		"scripts/sparkle/prepare_framework.sh",
		"-tags \"production sparkle\"",
		"build/darwin/ensure_rpath.sh",
		"SPARKLE_FRAMEWORK:",
	} {
		if !strings.Contains(taskfile, required) {
			t.Errorf("Taskfile.yml missing %q", required)
		}
	}
}

func TestBundleAssemblyAndVerificationRequireSparkleFramework(t *testing.T) {
	t.Parallel()

	assemble := readContractFile(t, "assemble_bundle.sh")
	for _, required := range []string{
		"<Sparkle.framework>",
		"Contents/Frameworks",
		"ditto \"$sparkle_framework\"",
	} {
		if !strings.Contains(assemble, required) {
			t.Errorf("assemble_bundle.sh missing %q", required)
		}
	}

	verify := readContractFile(t, "verify_bundle.sh")
	for _, required := range []string{
		"Sparkle.framework/Versions/B/Sparkle",
		"CFBundleShortVersionString",
		"lipo -archs \"$sparkle_binary\"",
		"otool -L \"$executable_path\"",
		"@rpath/Sparkle.framework/Versions/B/Sparkle",
		"otool -l \"$executable_path\"",
		"@executable_path/../Frameworks",
	} {
		if !strings.Contains(verify, required) {
			t.Errorf("verify_bundle.sh missing %q", required)
		}
	}
}

func TestBundleAssemblySupportsValidatedPublicUpdateMetadata(t *testing.T) {
	t.Parallel()

	assemble := readContractFile(t, "assemble_bundle.sh")
	for _, required := range []string{"SUFeedURL", "SUPublicEDKey", "must decode to 32 bytes", "^https://"} {
		if !strings.Contains(assemble, required) {
			t.Errorf("assemble_bundle.sh missing %q", required)
		}
	}
}

func readContractFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
