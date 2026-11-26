package corev1_test

import (
	"testing"

	corev1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/core/v1"
)

func TestCoreProtoAGYOnlineEnabledFieldAndOptionalPresence(t *testing.T) {
	t.Parallel()

	snap := &corev1.SettingsOnlineSnapshot{AgyOnlineEnabled: true}
	if !snap.GetAgyOnlineEnabled() {
		t.Fatal("SettingsOnlineSnapshot.AgyOnlineEnabled did not round-trip")
	}

	omitted := &corev1.SettingsOnlineUpdate{}
	if omitted.AgyOnlineEnabled != nil {
		t.Fatalf("omitted SettingsOnlineUpdate.AgyOnlineEnabled = %v, want nil", omitted.AgyOnlineEnabled)
	}

	disabled := false
	explicit := &corev1.SettingsOnlineUpdate{AgyOnlineEnabled: &disabled}
	if explicit.AgyOnlineEnabled == nil || explicit.GetAgyOnlineEnabled() {
		t.Fatalf("explicit false SettingsOnlineUpdate.AgyOnlineEnabled = %v", explicit.AgyOnlineEnabled)
	}
}
