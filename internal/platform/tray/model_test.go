package tray

import (
	"errors"
	"strings"
	"testing"
)

func float64Pointer(value float64) *float64 { return &value }

func trustedSnapshot(primary, secondary float64) Snapshot {
	return Snapshot{Windows: []WindowSnapshot{
		{Kind: WindowPrimary, RemainingPercent: float64Pointer(primary), Freshness: FreshnessFresh},
		{Kind: WindowSecondary, RemainingPercent: float64Pointer(secondary), Freshness: FreshnessFresh},
	}}
}

func TestProjectorBuildsStableDoubleLineModel(t *testing.T) {
	t.Parallel()

	model := NewProjector().Project(trustedSnapshot(62, 71))
	if model.Width != 252 || model.Height != 54 || model.State != DisplayTrusted {
		t.Fatalf("unexpected frame/state: %#v", model)
	}
	if model.Rows[0].Label != "5 小时" || model.Rows[0].Value != "62%" || model.Rows[0].Progress != 0.62 {
		t.Fatalf("unexpected primary row: %#v", model.Rows[0])
	}
	if model.Rows[1].Label != "本周" || model.Rows[1].Value != "71%" || model.Rows[1].Progress != 0.71 {
		t.Fatalf("unexpected secondary row: %#v", model.Rows[1])
	}
	for _, expected := range []string{"5 小时剩余 62%", "本周剩余 71%", "数据可信"} {
		if !strings.Contains(model.AccessibilityLabel, expected) {
			t.Fatalf("accessibility label %q missing %q", model.AccessibilityLabel, expected)
		}
	}
}

func TestProjectorExposesMinuteBucketedResetCountdownAccessibly(t *testing.T) {
	t.Parallel()

	reset := int64(3_661_000)
	snapshot := Snapshot{Windows: []WindowSnapshot{{
		Kind: WindowSecondary, RemainingPercent: float64Pointer(71), Freshness: FreshnessFresh,
		ResetRemainingMS: &reset,
	}}}
	model := NewProjector().Project(snapshot)
	if model.Rows[0].ResetDescription != "1 小时 2 分钟后重置" ||
		!strings.Contains(model.AccessibilityLabel, "1 小时 2 分钟后重置") {
		t.Fatalf("reset countdown is missing: %#v", model)
	}
}

func TestProjectorNeverConvertsUnknownToZero(t *testing.T) {
	t.Parallel()

	model := NewProjector().Project(Snapshot{Windows: []WindowSnapshot{
		{Kind: WindowPrimary, Freshness: FreshnessNeverLoaded},
		{Kind: WindowSecondary, RemainingPercent: float64Pointer(0), Freshness: FreshnessFresh},
	}})
	if model.Rows[0].Value != "--" || model.Rows[0].Known || model.Rows[0].Progress != 0 {
		t.Fatalf("unknown primary was misrepresented: %#v", model.Rows[0])
	}
	if model.Rows[1].Value != "0%" || !model.Rows[1].Known || model.State != DisplayUnavailable {
		t.Fatalf("known zero or aggregate state is wrong: %#v", model)
	}
}

func TestProjectorHidesAbsentPrimaryAndRestoresItWhenItReturns(t *testing.T) {
	t.Parallel()

	projector := NewProjector()
	secondaryOnly := projector.Project(Snapshot{Windows: []WindowSnapshot{
		{Kind: WindowSecondary, RemainingPercent: float64Pointer(71), Freshness: FreshnessFresh},
	}})
	if secondaryOnly.State != DisplayTrusted || len(secondaryOnly.Rows) != 1 ||
		secondaryOnly.Rows[0].Kind != WindowSecondary || secondaryOnly.Rows[0].Value != "71%" ||
		strings.Contains(secondaryOnly.AccessibilityLabel, "5 小时") {
		t.Fatalf("secondary-only response is not represented safely: %#v", secondaryOnly)
	}
	restored := projector.Project(trustedSnapshot(62, 70))
	if restored.State != DisplayTrusted || len(restored.Rows) != 2 ||
		restored.Rows[0].Value != "62%" || restored.Rows[1].Value != "70%" {
		t.Fatalf("future primary window did not restore automatically: %#v", restored)
	}
}

func TestProjectorDoesNotResurrectRetiredPrimaryAfterLaterReadFailure(t *testing.T) {
	t.Parallel()

	projector := NewProjector()
	_ = projector.Project(trustedSnapshot(62, 71))
	_ = projector.Project(Snapshot{Windows: []WindowSnapshot{{
		Kind: WindowSecondary, RemainingPercent: float64Pointer(70), Freshness: FreshnessFresh,
	}}})
	failed := projector.Project(Snapshot{ReadError: errors.New("transport failed")})
	if failed.State != DisplayStale || len(failed.Rows) != 1 ||
		failed.Rows[0].Kind != WindowSecondary || failed.Rows[0].Value != "70%" {
		t.Fatalf("retired primary was resurrected: %#v", failed)
	}
}

func TestProjectorPreservesLastTrustedValueOnFailure(t *testing.T) {
	t.Parallel()

	projector := NewProjector()
	_ = projector.Project(trustedSnapshot(62, 71))
	model := projector.Project(Snapshot{ReadError: errors.New("database busy")})
	if model.State != DisplayStale || model.Rows[0].Value != "62%" || model.Rows[1].Value != "71%" {
		t.Fatalf("last trusted display was not preserved: %#v", model)
	}
	if !strings.Contains(model.AccessibilityLabel, "数据陈旧") {
		t.Fatalf("failure was not exposed accessibly: %q", model.AccessibilityLabel)
	}
}

func TestProjectorStatePrecedenceAndHealthRemainIndependent(t *testing.T) {
	t.Parallel()

	snapshot := trustedSnapshot(55, 71)
	snapshot.Windows[0].Conflict = true
	snapshot.Health = HealthBlocked
	model := NewProjector().Project(snapshot)
	if model.State != DisplayConflict || model.Health != HealthBlocked {
		t.Fatalf("quota and health states were mixed: %#v", model)
	}
	if model.HealthMarker != "!" || !strings.Contains(model.AccessibilityLabel, "健康受阻") {
		t.Fatalf("blocked health lacks non-colour signal: %#v", model)
	}

	snapshot.Health = HealthDegraded
	model = NewProjector().Project(snapshot)
	if model.HealthMarker != "△" || !strings.Contains(model.AccessibilityLabel, "健康降级") {
		t.Fatalf("degraded health lacks non-colour signal: %#v", model)
	}
}

func TestProjectorRejectsInvalidOrDuplicateWindows(t *testing.T) {
	t.Parallel()

	model := NewProjector().Project(Snapshot{Windows: []WindowSnapshot{
		{Kind: WindowPrimary, RemainingPercent: float64Pointer(101), Freshness: FreshnessFresh},
		{Kind: WindowPrimary, RemainingPercent: float64Pointer(50), Freshness: FreshnessFresh},
	}})
	if model.State != DisplayUnavailable || len(model.Rows) != 0 {
		t.Fatalf("invalid input was accepted: %#v", model)
	}
}
