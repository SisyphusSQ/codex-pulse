package appserver

import (
	"reflect"
	"testing"
)

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
