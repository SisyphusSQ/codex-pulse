package app

import (
	"encoding/json"
	"runtime"
	"testing"
)

func TestServiceBootstrap(t *testing.T) {
	t.Parallel()

	got := newBindingTestService(t, &usageCostBindingStub{}, &runtimeInfoBindingStub{}).Bootstrap()
	want := BootstrapInfo{
		Name:     "Codex Pulse",
		Locale:   "zh-CN",
		Platform: runtime.GOOS,
	}

	if got != want {
		t.Fatalf("Bootstrap() = %#v, want %#v", got, want)
	}
}

func TestBootstrapInfoJSONContract(t *testing.T) {
	t.Parallel()

	got, err := json.Marshal(BootstrapInfo{
		Name:     "Codex Pulse",
		Locale:   "zh-CN",
		Platform: "darwin",
	})
	if err != nil {
		t.Fatalf("marshal BootstrapInfo: %v", err)
	}

	want := `{"name":"Codex Pulse","locale":"zh-CN","platform":"darwin"}`
	if string(got) != want {
		t.Fatalf("BootstrapInfo JSON = %s, want %s", got, want)
	}
}
