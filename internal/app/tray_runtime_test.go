package app

import (
	"context"
	"errors"
	"testing"

	quotaquery "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	platformtray "github.com/SisyphusSQ/codex-pulse/internal/platform/tray"
	"github.com/SisyphusSQ/codex-pulse/internal/query/runtimeinfo"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type trayQueryStub struct {
	quota     runtimeinfo.QuotaCurrentResponse
	quotaErr  error
	health    HealthProjectionResponse
	healthErr error
}

func (stub trayQueryStub) QuotaCurrent(context.Context, int64) (runtimeinfo.QuotaCurrentResponse, error) {
	return stub.quota, stub.quotaErr
}

func (stub trayQueryStub) HealthProjection(context.Context) (HealthProjectionResponse, error) {
	return stub.health, stub.healthErr
}

func TestTraySnapshotReaderMapsSecondaryOnlyWithoutInventingPrimary(t *testing.T) {
	t.Parallel()

	remaining := 71.0
	level := HealthProjectionHealthy
	reader := traySnapshotReader{query: trayQueryStub{
		quota: runtimeinfo.QuotaCurrentResponse{Current: quotaquery.CurrentResponse{
			Windows: []quotaquery.CurrentWindow{{
				WindowKind: store.QuotaWindowSecondary, RemainingPercent: &remaining,
				Freshness: store.QuotaCurrentFresh, Conflict: store.QuotaConflictNone,
			}},
		}},
		health: HealthProjectionResponse{HasValue: true, Level: &level},
	}}
	snapshot, err := reader.Read(context.Background(), 123)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Windows) != 1 || snapshot.Windows[0].Kind != platformtray.WindowSecondary ||
		snapshot.Windows[0].RemainingPercent == nil || *snapshot.Windows[0].RemainingPercent != 71 {
		t.Fatalf("unexpected secondary-only mapping: %#v", snapshot)
	}
}

func TestTraySnapshotReaderMapsConflictFreshnessAndHealth(t *testing.T) {
	t.Parallel()

	remaining := 55.0
	level := HealthProjectionBlocked
	reader := traySnapshotReader{query: trayQueryStub{
		quota: runtimeinfo.QuotaCurrentResponse{Current: quotaquery.CurrentResponse{
			Windows: []quotaquery.CurrentWindow{{
				WindowKind: store.QuotaWindowPrimary, RemainingPercent: &remaining,
				Freshness: store.QuotaCurrentStale, Conflict: store.QuotaConflictPresent,
			}},
		}},
		health: HealthProjectionResponse{HasValue: true, Level: &level},
	}}
	snapshot, err := reader.Read(context.Background(), 123)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Windows[0].Freshness != platformtray.FreshnessStale || !snapshot.Windows[0].Conflict ||
		snapshot.Health != platformtray.HealthBlocked {
		t.Fatalf("semantic mapping was lost: %#v", snapshot)
	}
}

func TestTraySnapshotReaderKeepsQuotaFailureAndDegradesMissingHealth(t *testing.T) {
	t.Parallel()

	quotaErr := errors.New("quota unavailable")
	healthErr := errors.New("health unavailable")
	reader := traySnapshotReader{query: trayQueryStub{quotaErr: quotaErr, healthErr: healthErr}}
	snapshot, err := reader.Read(context.Background(), 123)
	if !errors.Is(err, quotaErr) || errors.Is(err, healthErr) || snapshot.Health != platformtray.HealthDegraded {
		t.Fatalf("failures were not preserved: snapshot=%#v err=%v", snapshot, err)
	}
}
