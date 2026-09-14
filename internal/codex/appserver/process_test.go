package appserver

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// 测试 Finder 的最小 PATH 下仍会选择产品认可的绝对 Codex CLI 候选。（风险复现用例）
func TestResolveCodexBinaryUsesAbsoluteFallbackWithMinimalPath(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "codex")
	writeCodexVersionScript(t, binary, "0.154.0", "exit 0")
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")

	got, err := resolveCodexBinary("", []string{binary})
	if err != nil {
		t.Fatalf("resolveCodexBinary() error = %v", err)
	}
	if got != binary {
		t.Fatalf("resolveCodexBinary() = %q, want %q", got, binary)
	}
}

func TestResolveCodexBinaryPrefersStableLocalOverAlphaAndOldPATH(t *testing.T) {
	directory := t.TempDir()
	pathDir := filepath.Join(directory, "path")
	localDir := filepath.Join(directory, "local-bin")
	alphaDir := filepath.Join(directory, "ChatGPT.app", "Contents", "Resources")
	if err := os.MkdirAll(pathDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(localDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(alphaDir, 0o700); err != nil {
		t.Fatal(err)
	}
	oldPATH := filepath.Join(pathDir, "codex")
	stableLocal := filepath.Join(localDir, "codex")
	alphaApp := filepath.Join(alphaDir, "codex")
	writeCodexVersionScript(t, oldPATH, "0.150.1", "exit 0")
	writeCodexVersionScript(t, stableLocal, "0.154.0", "exit 0")
	writeCodexVersionScript(t, alphaApp, "0.154.0-alpha.6.2", "exit 0")
	t.Setenv("PATH", pathDir)

	got, err := resolveCodexBinary("", []string{stableLocal, alphaApp})
	if err != nil {
		t.Fatalf("resolveCodexBinary() error = %v", err)
	}
	if got != stableLocal {
		t.Fatalf("resolveCodexBinary() = %q, want stable %q", got, stableLocal)
	}
	inspection, inspectErr := InspectCodexBinary(got)
	if inspectErr != nil || inspection.Version != "0.154.0" ||
		inspection.CapabilityState != CodexCapabilityAccountRateLimits {
		t.Fatalf("InspectCodexBinary(%q) = %#v, %v", got, inspection, inspectErr)
	}
}

func TestResolveCodexBinaryRejectsAlphaExplicitBinary(t *testing.T) {
	directory := t.TempDir()
	alpha := filepath.Join(directory, "codex")
	writeCodexVersionScript(t, alpha, "0.154.0-alpha.6.2", "exit 0")

	got, err := resolveCodexBinary(alpha, nil)
	if !errors.Is(err, ErrCapabilityUnavailable) || got != "" {
		t.Fatalf("resolveCodexBinary(alpha) = %q, %v", got, err)
	}
	if strings.Contains(err.Error(), directory) || strings.Contains(err.Error(), "alpha") {
		t.Fatalf("capability error leaked binary details: %v", err)
	}
}

func writeCodexVersionScript(t *testing.T, path, version, body string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then\n" +
		"  printf 'codex-cli " + version + "\\n'\n" +
		"  exit 0\n" +
		"fi\n" +
		body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultCodexBinaryCandidatesPutLocalBinFirst(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Fatalf("os.UserHomeDir() error = %v", err)
	}
	candidates := defaultCodexBinaryCandidates()
	want := filepath.Join(home, ".local", "bin", "codex")
	if len(candidates) == 0 || candidates[0] != want {
		t.Fatalf("defaultCodexBinaryCandidates()[0] = %#v, want %q first", candidates, want)
	}
}

func TestIsolatedCodexEnvironmentReplacesInheritedHome(t *testing.T) {
	t.Parallel()

	got := isolatedCodexEnvironment([]string{
		"PATH=/usr/bin",
		"CODEX_HOME=/private/tmp/cp-inherited-home",
		"LANG=zh_CN.UTF-8",
		"CODEX_HOME=/another/private/home",
	}, "/private/tmp/cp-app-smoke.test/runtime/codex-home")
	want := []string{
		"PATH=/usr/bin",
		"LANG=zh_CN.UTF-8",
		"CODEX_HOME=/private/tmp/cp-app-smoke.test/runtime/codex-home",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("isolated environment = %#v, want %#v", got, want)
	}
}
