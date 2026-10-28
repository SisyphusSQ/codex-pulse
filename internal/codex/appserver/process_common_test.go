package appserver

import (
	"reflect"
	"strings"
	"testing"
)

func TestAppServerStderrHintIsBoundedAndNeverReturnsBody(t *testing.T) {
	writer := new(boundedDiagnosticStderr)
	body := []byte("secret-marker HTTP 429 token=private" + strings.Repeat("x", 128<<10))
	if n, err := writer.Write(body); err != nil || n != len(body) {
		t.Fatal("stderr write failed")
	}
	if writer.content.Len() != 64<<10 || writer.reason() != "http_429" {
		t.Fatal("stderr hint unbounded or unclassified")
	}
}

// 测试 Finder 的最小 PATH 下仍会选择产品认可的绝对 Codex CLI 候选。（风险复现用例）
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
