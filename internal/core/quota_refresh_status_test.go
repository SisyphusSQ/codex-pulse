package core

import (
	"testing"

	corev1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/core/v1"
	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	healthmodel "github.com/SisyphusSQ/codex-pulse/internal/health"
)

func TestQuotaRuntimeStatusSurvivesProtoMapping(t *testing.T) {
	value := quotaonline.CurrentResponse{Refresh: quotaonline.CurrentRefresh{Runtime: &quotaonline.RefreshRuntimeStatus{State: "recoverable", FailureStage: "complete_claim", FailureReason: "timeout", LastFailureAtMS: new(int64(100)), DiagnosticsDropped: 2}}}
	target := new(corev1.CurrentQuota)
	if err := EncodeResponse(value, target); err != nil {
		t.Fatal(err)
	}
	status := target.GetRefresh().GetRuntime()
	if status.GetState() != "recoverable" || status.GetFailureStage() != "complete_claim" || status.GetFailureReason() != "timeout" || status.GetLastFailureAtMs() != 100 || status.GetDiagnosticsDropped() != 2 {
		t.Fatalf("runtime fields lost: %v", status)
	}
}

func TestHealthQuotaRuntimeOverlayPreservesOtherProtection(t *testing.T) {
	value := healthmodel.Projection{HasValue: true, Failure: healthmodel.FailureNone, Result: healthmodel.Result{Level: healthmodel.LevelHealthy}}
	for _, kind := range healthProjectionComponentOrder {
		value.Result.Components = append(value.Result.Components, healthmodel.ComponentStatus{Component: kind, Level: healthmodel.LevelHealthy, Evidence: healthmodel.EvidenceKnown, Reason: healthmodel.ReasonHealthy, Impact: healthmodel.ImpactNone, Protection: healthmodel.ProtectionNone, RecoveryAction: healthmodel.RecoveryNone})
	}
	degraded := healthWithQuotaRuntime(value, quotaonline.RefreshRuntimeStatus{State: "recoverable"})
	if _, err := mapHealthProjection(degraded); err != nil {
		t.Fatal(err)
	}
	if degraded.Result.Level != healthmodel.LevelDegraded || degraded.Result.Primary.Component != healthmodel.ComponentOnlineQuota || value.Result.Components[3].Level != healthmodel.LevelHealthy {
		t.Fatal("runtime overlay absent or mutated shared snapshot")
	}
	storage := &value.Result.Components[4]
	storage.Level = healthmodel.LevelBlocked
	storage.Reason = healthmodel.ReasonStoreCorrupt
	storage.Impact = healthmodel.ImpactStorageAtRisk
	storage.Protection = healthmodel.ProtectionWritesStopped
	storage.RecoveryAction = healthmodel.RecoveryRepairStore
	blocked := healthWithQuotaRuntime(value, quotaonline.RefreshRuntimeStatus{State: "blocked"})
	if _, err := mapHealthProjection(blocked); err != nil {
		t.Fatal(err)
	}
	if blocked.Result.Primary.Component != healthmodel.ComponentStorage || blocked.Result.Level != healthmodel.LevelBlocked {
		t.Fatal("quota failure weakened storage protection")
	}
}
