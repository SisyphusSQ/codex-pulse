package app

import (
	"encoding/json"
	"errors"
	"runtime"
	"testing"
)

func TestNewServiceRequiresCompleteNormalGraph(t *testing.T) {
	if _, err := NewService(ServiceConfig{}); !errors.Is(err, ErrBindingService) {
		t.Fatalf("empty graph error = %v", err)
	}
	if _, err := NewService(ServiceConfig{
		UsageCost: &usageCostBindingStub{},
	}); !errors.Is(err, ErrBindingService) {
		t.Fatalf("incomplete graph error = %v", err)
	}
}

func TestServiceBootstrap(t *testing.T) {
	t.Parallel()

	got := newStartupService(nil).Bootstrap()
	want := BootstrapInfo{
		Name:     "Codex Pulse",
		Locale:   "zh-CN",
		Platform: runtime.GOOS,
		Mode:     ApplicationModeNormal,
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
		Mode:     ApplicationModeNormal,
	})
	if err != nil {
		t.Fatalf("marshal BootstrapInfo: %v", err)
	}

	want := `{"name":"Codex Pulse","locale":"zh-CN","platform":"darwin","mode":"normal","recovery":null}`
	if string(got) != want {
		t.Fatalf("BootstrapInfo JSON = %s, want %s", got, want)
	}
}
