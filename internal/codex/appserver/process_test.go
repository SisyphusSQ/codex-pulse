package appserver

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// 测试 Finder 的最小 PATH 下仍会选择产品认可的绝对 Codex CLI 候选。（风险复现用例）
func TestResolveCodexBinaryUsesAbsoluteFallbackWithMinimalPath(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "codex")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")

	got, err := resolveCodexBinary("", []string{binary})
	if err != nil {
		t.Fatalf("resolveCodexBinary() error = %v", err)
	}
	if got != binary {
		t.Fatalf("resolveCodexBinary() = %q, want %q", got, binary)
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
