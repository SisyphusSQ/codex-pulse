package tray

import (
	"errors"
	"testing"
)

func TestLockedCapabilities(t *testing.T) {
	t.Parallel()

	if LockedWailsVersion != "v3.0.0-alpha2.117" {
		t.Fatalf("LockedWailsVersion = %q", LockedWailsVersion)
	}
	capabilities := LockedCapabilities()
	if err := ValidateCapabilities(capabilities); err != nil {
		t.Fatalf("ValidateCapabilities() error = %v", err)
	}
	status := make(map[string]Status, len(capabilities))
	for _, capability := range capabilities {
		status[capability.ID] = capability.Status
	}
	if status["multi-display-anchor"] != StatusSupported {
		t.Fatalf("multi-display anchor = %q", status["multi-display-anchor"])
	}
	for _, id := range []string{"native-status-item-custom-view", "status-item-accessibility", "platform-change-recovery"} {
		if status[id] != StatusSupported {
			t.Fatalf("%s = %q", id, status[id])
		}
	}
	if status["native-nspopover"] != StatusAdapterRequired {
		t.Fatalf("native-nspopover = %q", status["native-nspopover"])
	}
}

func TestValidateCapabilitiesRejectsDrift(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		edit func([]Capability) []Capability
	}{
		{name: "empty", edit: func([]Capability) []Capability { return nil }},
		{name: "duplicate", edit: func(value []Capability) []Capability { return append(value, value[0]) }},
		{name: "missing", edit: func(value []Capability) []Capability { return value[1:] }},
		{name: "unknown status", edit: func(value []Capability) []Capability { value[0].Status = "unknown"; return value }},
		{name: "missing evidence", edit: func(value []Capability) []Capability { value[0].Evidence = ""; return value }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateCapabilities(test.edit(LockedCapabilities())); !errors.Is(err, ErrCapabilityContract) {
				t.Fatalf("ValidateCapabilities() error = %v", err)
			}
		})
	}
}
