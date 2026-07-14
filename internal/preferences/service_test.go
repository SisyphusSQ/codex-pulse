package preferences

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
)

func TestServiceUpdateSettingsValidatesCASAndExactReplay(t *testing.T) {
	t.Parallel()

	store := confirmedPreferencesStore(t)
	service := newPreferencesService(t, store, &fakeHomeProbe{}, &fakeHomeRuntime{})
	base, err := store.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}
	request := SettingsUpdate{
		ExpectedRevision: base.Revision,
		Online:           OnlinePreferences{QuotaEnabled: false, ResetCreditsEnabled: true},
		Refresh: RefreshPreferences{
			QuotaIntervalSeconds: 120, ResetCreditsIntervalSeconds: 3600,
			ReconcileIntervalSeconds: 7200, JSONLDebounceMilliseconds: 3000,
		},
		Updates: UpdatePreferences{
			AutoCheckEnabled: false, AutoDownloadEnabled: false,
			Channel: UpdateChannelStable, CheckIntervalSeconds: 7200,
		},
		UI: UIPreferences{
			Locale: "zh-CN", LaunchBehavior: LaunchBehaviorMainWindow, OverviewRange: OverviewRangeThirtyDays,
		},
	}
	updated, err := service.UpdateSettings(context.Background(), request)
	if err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}
	if updated.Revision != base.Revision+1 || updated.Online != request.Online ||
		updated.Refresh != request.Refresh || updated.Updates != request.Updates || updated.UI != request.UI ||
		updated.CodexHome != base.CodexHome || updated.PendingSwitch != nil {
		t.Fatalf("UpdateSettings() = %#v", updated)
	}
	replayed, err := service.UpdateSettings(context.Background(), request)
	if err != nil || !reflect.DeepEqual(replayed, updated) {
		t.Fatalf("UpdateSettings(exact replay) = %#v, %v, want %#v", replayed, err, updated)
	}
	invalid := request
	invalid.ExpectedRevision = updated.Revision
	invalid.UI.Locale = "en-US"
	if _, err := service.UpdateSettings(context.Background(), invalid); !errors.Is(err, ErrInvalidPreferences) {
		t.Fatalf("UpdateSettings(invalid) error = %v, want ErrInvalidPreferences", err)
	}
	got, err := store.LoadPreferences(context.Background())
	if err != nil || !reflect.DeepEqual(got, updated) {
		t.Fatalf("LoadPreferences(after invalid) = %#v, %v, want %#v", got, err, updated)
	}
}

func TestServiceUpdateSettingsReadsBackCommittedDurabilityUnknownAfterCancellation(t *testing.T) {
	t.Parallel()

	fileStore := confirmedPreferencesStore(t)
	base, err := fileStore.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	store := &scriptedPreferencesStore{
		inner: fileStore,
		cas: func(_ int, callCtx context.Context, expected uint64, next Snapshot) error {
			if err := fileStore.CompareAndSwap(callCtx, expected, next); err != nil {
				return err
			}
			cancel()
			return ErrDurabilityUnknown
		},
	}
	service := newPreferencesService(t, store, &fakeHomeProbe{}, &fakeHomeRuntime{})
	request := SettingsUpdate{
		ExpectedRevision: base.Revision,
		Online:           OnlinePreferences{QuotaEnabled: false, ResetCreditsEnabled: true},
		Refresh:          base.Refresh,
		Updates:          base.Updates,
		UI:               base.UI,
	}
	got, err := service.UpdateSettings(ctx, request)
	if err != nil || got.Revision != base.Revision+1 || got.Online != request.Online {
		t.Fatalf("UpdateSettings(committed unknown) = %#v, %v", got, err)
	}
}

func TestServiceUpdateSettingsDurabilityReadbackStates(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name           string
		configure      func(*FileStore, *scriptedPreferencesStore, Snapshot) error
		wantConflict   bool
		wantLoadError  bool
		wantQuotaValue bool
	}{
		{
			name: "old remains visible",
			configure: func(_ *FileStore, store *scriptedPreferencesStore, _ Snapshot) error {
				store.cas = func(_ int, _ context.Context, _ uint64, _ Snapshot) error {
					return ErrDurabilityUnknown
				}
				return nil
			},
			wantQuotaValue: true,
		},
		{
			name: "conflicting winner visible",
			configure: func(fileStore *FileStore, store *scriptedPreferencesStore, base Snapshot) error {
				store.cas = func(_ int, ctx context.Context, expected uint64, _ Snapshot) error {
					winner := cloneSnapshot(base)
					winner.Revision++
					winner.UI.OverviewRange = OverviewRangeThirtyDays
					if err := fileStore.CompareAndSwap(ctx, expected, winner); err != nil {
						return err
					}
					return ErrDurabilityUnknown
				}
				return nil
			},
			wantConflict:   true,
			wantQuotaValue: true,
		},
		{
			name: "readback unavailable",
			configure: func(_ *FileStore, store *scriptedPreferencesStore, _ Snapshot) error {
				store.cas = func(_ int, _ context.Context, _ uint64, _ Snapshot) error {
					return ErrDurabilityUnknown
				}
				store.load = func(call int, ctx context.Context) (Snapshot, error) {
					if call == 2 {
						return Snapshot{}, errors.New("readback unavailable")
					}
					return store.inner.LoadPreferences(ctx)
				}
				return nil
			},
			wantLoadError:  true,
			wantQuotaValue: true,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fileStore := confirmedPreferencesStore(t)
			base, err := fileStore.LoadPreferences(context.Background())
			if err != nil {
				t.Fatalf("LoadPreferences() error = %v", err)
			}
			store := &scriptedPreferencesStore{inner: fileStore}
			if err := test.configure(fileStore, store, base); err != nil {
				t.Fatalf("configure() error = %v", err)
			}
			service := newPreferencesService(t, store, &fakeHomeProbe{}, &fakeHomeRuntime{})
			request := SettingsUpdate{
				ExpectedRevision: base.Revision,
				Online:           OnlinePreferences{QuotaEnabled: false, ResetCreditsEnabled: true},
				Refresh:          base.Refresh, Updates: base.Updates, UI: base.UI,
			}
			got, err := service.UpdateSettings(context.Background(), request)
			if !errors.Is(err, ErrDurabilityUnknown) || errors.Is(err, ErrPreferencesConflict) != test.wantConflict {
				t.Fatalf("UpdateSettings() error = %v", err)
			}
			if test.wantLoadError && !strings.Contains(err.Error(), "readback unavailable") {
				t.Fatalf("UpdateSettings() error = %v, want readback error", err)
			}
			if got.Online.QuotaEnabled != test.wantQuotaValue {
				t.Fatalf("UpdateSettings() snapshot = %#v", got)
			}
			if test.wantConflict && got.UI.OverviewRange != OverviewRangeThirtyDays {
				t.Fatalf("UpdateSettings(conflict) = %#v, want visible winner", got)
			}
		})
	}
}

func TestServiceSwitchDrainsPublishesGenerationThenStartsBootstrap(t *testing.T) {
	t.Parallel()

	store := confirmedPreferencesStore(t)
	probe := &fakeHomeProbe{metadata: logs.HomeMetadata{
		Path: filepath.Join(t.TempDir(), "new-home"), DeviceID: "device-new", Inode: 99,
	}}
	runtime := &fakeHomeRuntime{status: BootstrapStatusQueued, store: store}
	service := newPreferencesService(t, store, probe, runtime)

	plan, err := service.PlanSwitch(context.Background(), probe.metadata.Path, HomeSwitchIndependentDatabase)
	if err != nil {
		t.Fatalf("PlanSwitch() error = %v", err)
	}
	if plan.ID == "" || plan.Strategy != HomeSwitchIndependentDatabase || !plan.Impact.PreservesOldFacts ||
		plan.Impact.ClearsDerivedFacts || plan.Impact.SummaryZH == "" {
		t.Fatalf("PlanSwitch() = %#v", plan)
	}
	result, err := service.ConfirmSwitch(context.Background(), plan.ID)
	if err != nil {
		t.Fatalf("ConfirmSwitch() error = %v", err)
	}
	if result.CodexHome.Generation != 2 || result.CodexHome.Source.Path != probe.metadata.Path ||
		result.CodexHome.DataStoreKey == DefaultDataStoreKey || result.PendingSwitch != nil ||
		result.LastSwitch == nil || result.LastSwitch.Outcome != HomeSwitchCompleted ||
		len(result.DetachedHomes) != 1 || result.DetachedHomes[0].DataStoreKey != DefaultDataStoreKey ||
		result.DetachedHomes[0].Generation != 1 {
		t.Fatalf("ConfirmSwitch() = %#v", result)
	}
	wantCalls := []string{"drain:1", "start:2:" + plan.ID}
	if !reflect.DeepEqual(runtime.calls, wantCalls) {
		t.Fatalf("runtime calls = %#v, want %#v", runtime.calls, wantCalls)
	}
	if !runtime.sawPendingAtStart {
		t.Fatal("bootstrap started before pending generation was durable")
	}
	if runtime.request.Strategy != HomeSwitchIndependentDatabase ||
		runtime.request.DataStoreKey != result.CodexHome.DataStoreKey ||
		runtime.request.Source.Path != probe.metadata.Path {
		t.Fatalf("bootstrap request = %#v", runtime.request)
	}
}

func TestServiceSwitchDrainFailureLeavesOldSnapshot(t *testing.T) {
	t.Parallel()

	store := confirmedPreferencesStore(t)
	before, err := store.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences(before) error = %v", err)
	}
	probe := &fakeHomeProbe{metadata: logs.HomeMetadata{
		Path: filepath.Join(t.TempDir(), "new-home"), DeviceID: "device-new", Inode: 100,
	}}
	drainErr := errors.New("drain failed")
	runtime := &fakeHomeRuntime{drainErr: drainErr}
	service := newPreferencesService(t, store, probe, runtime)
	plan, err := service.PlanSwitch(context.Background(), probe.metadata.Path, HomeSwitchClearAndRebuild)
	if err != nil {
		t.Fatalf("PlanSwitch() error = %v", err)
	}
	if !plan.Impact.ClearsDerivedFacts || plan.Impact.PreservesOldFacts {
		t.Fatalf("clear plan impact = %#v", plan.Impact)
	}
	if _, err := service.ConfirmSwitch(context.Background(), plan.ID); !errors.Is(err, drainErr) ||
		!strings.Contains(err.Error(), "drain Home generation 1") {
		t.Fatalf("ConfirmSwitch(drain failure) error = %v, want drain error", err)
	}
	after, err := store.LoadPreferences(context.Background())
	if err != nil || after.CodexHome != before.CodexHome || after.Online != before.Online ||
		after.Refresh != before.Refresh || after.Updates != before.Updates || after.UI != before.UI ||
		after.PendingSwitch != nil || after.PendingResume != nil || after.LastSwitch == nil ||
		after.LastSwitch.Outcome != HomeSwitchRolledBack {
		t.Fatalf("preferences after drain failure = %#v, %v", after, err)
	}
	if !reflect.DeepEqual(runtime.calls, []string{"drain:1", "resume:1"}) {
		t.Fatalf("runtime calls = %#v", runtime.calls)
	}
}

func TestServiceBootstrapNotStartedRollsBackAndResumesOldGeneration(t *testing.T) {
	t.Parallel()

	store := confirmedPreferencesStore(t)
	probe := &fakeHomeProbe{metadata: logs.HomeMetadata{
		Path: filepath.Join(t.TempDir(), "new-home"), DeviceID: "device-new", Inode: 101,
	}}
	startErr := errors.New("start failed")
	runtime := &fakeHomeRuntime{startErr: startErr, status: BootstrapStatusNotStarted, store: store}
	service := newPreferencesService(t, store, probe, runtime)
	plan, err := service.PlanSwitch(context.Background(), probe.metadata.Path, HomeSwitchClearAndRebuild)
	if err != nil {
		t.Fatalf("PlanSwitch() error = %v", err)
	}
	rolledBack, err := service.ConfirmSwitch(context.Background(), plan.ID)
	if !errors.Is(err, startErr) || !strings.Contains(err.Error(), "start Home bootstrap") {
		t.Fatalf("ConfirmSwitch(start failure) error = %v, want start error", err)
	}
	if rolledBack.CodexHome.Generation != 1 || rolledBack.PendingSwitch != nil ||
		rolledBack.LastSwitch == nil || rolledBack.LastSwitch.Outcome != HomeSwitchRolledBack {
		t.Fatalf("rolled back snapshot = %#v", rolledBack)
	}
	wantCalls := []string{"drain:1", "start:2:" + plan.ID, "status:2:" + plan.ID, "resume:1"}
	if !reflect.DeepEqual(runtime.calls, wantCalls) {
		t.Fatalf("runtime calls = %#v, want %#v", runtime.calls, wantCalls)
	}
}

func TestServiceRecoverSwitchUsesDurableBootstrapStatus(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name           string
		status         BootstrapStatus
		wantGeneration uint64
		wantOutcome    HomeSwitchOutcome
		wantResume     bool
	}{
		{name: "not started rolls back", status: BootstrapStatusNotStarted, wantGeneration: 1, wantOutcome: HomeSwitchRolledBack, wantResume: true},
		{name: "safe failure rolls back", status: BootstrapStatusFailedSafeRollback, wantGeneration: 1, wantOutcome: HomeSwitchRolledBack, wantResume: true},
		{name: "queued continues", status: BootstrapStatusQueued, wantGeneration: 2, wantOutcome: HomeSwitchCompleted},
		{name: "running continues", status: BootstrapStatusRunning, wantGeneration: 2, wantOutcome: HomeSwitchCompleted},
		{name: "succeeded continues", status: BootstrapStatusSucceeded, wantGeneration: 2, wantOutcome: HomeSwitchCompleted},
		{name: "failed requiring resume continues", status: BootstrapStatusFailedNeedsResume, wantGeneration: 2, wantOutcome: HomeSwitchCompleted},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := confirmedPreferencesStore(t)
			pending := installPendingSwitch(t, store, HomeSwitchIndependentDatabase)
			runtime := &fakeHomeRuntime{status: test.status, store: store}
			service := newPreferencesService(t, store, &fakeHomeProbe{}, runtime)
			got, err := service.RecoverSwitch(context.Background())
			if err != nil {
				t.Fatalf("RecoverSwitch() error = %v", err)
			}
			if got.CodexHome.Generation != test.wantGeneration || got.PendingSwitch != nil ||
				got.LastSwitch == nil || got.LastSwitch.Outcome != test.wantOutcome {
				t.Fatalf("RecoverSwitch() = %#v", got)
			}
			wantCalls := []string{"status:2:" + pending.SwitchID}
			if test.wantResume {
				wantCalls = append(wantCalls, "resume:1")
			}
			if !reflect.DeepEqual(runtime.calls, wantCalls) {
				t.Fatalf("runtime calls = %#v, want %#v", runtime.calls, wantCalls)
			}
		})
	}
}

func TestServiceRecoverSwitchKeepsResumeMarkerUntilRuntimeResumes(t *testing.T) {
	t.Parallel()

	store := confirmedPreferencesStore(t)
	pending := installPendingSwitch(t, store, HomeSwitchClearAndRebuild)
	resumeErr := errors.New("resume unavailable")
	runtime := &fakeHomeRuntime{status: BootstrapStatusNotStarted, resumeErr: resumeErr, store: store}
	service := newPreferencesService(t, store, &fakeHomeProbe{}, runtime)
	got, err := service.RecoverSwitch(context.Background())
	if !errors.Is(err, ErrSwitchRecovery) || !errors.Is(err, resumeErr) {
		t.Fatalf("RecoverSwitch(resume failure) error = %v", err)
	}
	if got.PendingSwitch != nil || got.PendingResume == nil || got.CodexHome.Generation != 1 ||
		got.PendingResume.SwitchID != pending.SwitchID {
		t.Fatalf("RecoverSwitch(resume failure) = %#v", got)
	}
	persisted, loadErr := store.LoadPreferences(context.Background())
	if loadErr != nil || persisted.PendingResume == nil {
		t.Fatalf("LoadPreferences(resume marker) = %#v, %v", persisted, loadErr)
	}

	runtime.resumeErr = nil
	recovered, err := service.RecoverSwitch(context.Background())
	if err != nil || recovered.PendingResume != nil || recovered.CodexHome.Generation != 1 ||
		recovered.LastSwitch == nil || recovered.LastSwitch.Outcome != HomeSwitchRolledBack {
		t.Fatalf("RecoverSwitch(resume retry) = %#v, %v", recovered, err)
	}
	wantCalls := []string{
		"status:2:" + pending.SwitchID,
		"resume:1",
		"resume:1",
	}
	if !reflect.DeepEqual(runtime.calls, wantCalls) {
		t.Fatalf("runtime calls = %#v, want %#v", runtime.calls, wantCalls)
	}
}

func TestServiceRecoverSwitchUnknownStatusRetainsJournal(t *testing.T) {
	t.Parallel()

	store := confirmedPreferencesStore(t)
	pending := installPendingSwitch(t, store, HomeSwitchIndependentDatabase)
	runtime := &fakeHomeRuntime{status: BootstrapStatus("future_status"), store: store}
	service := newPreferencesService(t, store, &fakeHomeProbe{}, runtime)
	got, err := service.RecoverSwitch(context.Background())
	if !errors.Is(err, ErrSwitchRecovery) || got.PendingSwitch == nil ||
		got.PendingSwitch.SwitchID != pending.SwitchID || got.PendingResume != nil {
		t.Fatalf("RecoverSwitch(unknown status) = %#v, %v", got, err)
	}
}

func TestServiceSwitchCancellationBeforeAndAfterPendingCommit(t *testing.T) {
	t.Parallel()

	t.Run("before confirm", func(t *testing.T) {
		t.Parallel()
		store := confirmedPreferencesStore(t)
		probe := &fakeHomeProbe{metadata: logs.HomeMetadata{
			Path: filepath.Join(t.TempDir(), "target"), DeviceID: "device-target", Inode: 127,
		}}
		runtime := &fakeHomeRuntime{}
		service := newPreferencesService(t, store, probe, runtime)
		plan, err := service.PlanSwitch(context.Background(), probe.metadata.Path, HomeSwitchClearAndRebuild)
		if err != nil {
			t.Fatalf("PlanSwitch() error = %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := service.ConfirmSwitch(ctx, plan.ID); !errors.Is(err, context.Canceled) {
			t.Fatalf("ConfirmSwitch(canceled) error = %v", err)
		}
		if len(runtime.calls) != 0 {
			t.Fatalf("runtime calls = %#v, want none", runtime.calls)
		}
	})

	t.Run("after pending commit", func(t *testing.T) {
		t.Parallel()
		fileStore := confirmedPreferencesStore(t)
		ctx, cancel := context.WithCancel(context.Background())
		store := &scriptedPreferencesStore{
			inner: fileStore,
			cas: func(call int, callCtx context.Context, expected uint64, next Snapshot) error {
				if err := fileStore.CompareAndSwap(callCtx, expected, next); err != nil {
					return err
				}
				if call == 2 {
					cancel()
				}
				return nil
			},
		}
		probe := &fakeHomeProbe{metadata: logs.HomeMetadata{
			Path: filepath.Join(t.TempDir(), "target"), DeviceID: "device-target", Inode: 128,
		}}
		runtime := &fakeHomeRuntime{status: BootstrapStatusQueued, store: fileStore, respectStartContext: true}
		service := newPreferencesService(t, store, probe, runtime)
		plan, err := service.PlanSwitch(context.Background(), probe.metadata.Path, HomeSwitchIndependentDatabase)
		if err != nil {
			t.Fatalf("PlanSwitch() error = %v", err)
		}
		got, err := service.ConfirmSwitch(ctx, plan.ID)
		if err != nil || got.PendingSwitch != nil || got.PendingResume != nil || got.CodexHome.Generation != 2 {
			t.Fatalf("ConfirmSwitch(canceled after pending) = %#v, %v", got, err)
		}
		wantCalls := []string{"drain:1", "start:2:" + plan.ID, "status:2:" + plan.ID}
		if !reflect.DeepEqual(runtime.calls, wantCalls) {
			t.Fatalf("runtime calls = %#v, want %#v", runtime.calls, wantCalls)
		}
	})
}

func TestServiceRejectsNoRuntimeSameHomeAndTargetDrift(t *testing.T) {
	t.Parallel()

	store := confirmedPreferencesStore(t)
	if _, err := NewService(ServiceConfig{Store: store, Probe: &fakeHomeProbe{}}); !errors.Is(err, ErrInvalidService) {
		t.Fatalf("NewService(nil runtime) error = %v, want ErrInvalidService", err)
	}
	current, err := store.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}
	probe := &fakeHomeProbe{metadata: logs.HomeMetadata{
		Path: current.CodexHome.Source.Path, DeviceID: current.CodexHome.Source.DeviceID,
		Inode: current.CodexHome.Source.Inode,
	}}
	runtime := &fakeHomeRuntime{}
	service := newPreferencesService(t, store, probe, runtime)
	if _, err := service.PlanSwitch(context.Background(), probe.metadata.Path, HomeSwitchClearAndRebuild); !errors.Is(err, ErrHomeAlreadyActive) {
		t.Fatalf("PlanSwitch(same Home) error = %v, want ErrHomeAlreadyActive", err)
	}

	probe.metadata = logs.HomeMetadata{
		Path: filepath.Join(t.TempDir(), "target"), DeviceID: "device-target", Inode: 120,
	}
	plan, err := service.PlanSwitch(context.Background(), probe.metadata.Path, HomeSwitchClearAndRebuild)
	if err != nil {
		t.Fatalf("PlanSwitch(target) error = %v", err)
	}
	probe.metadata.Inode++
	if _, err := service.ConfirmSwitch(context.Background(), plan.ID); !errors.Is(err, ErrSwitchPlanStale) {
		t.Fatalf("ConfirmSwitch(drift) error = %v, want ErrSwitchPlanStale", err)
	}
	if len(runtime.calls) != 0 {
		t.Fatalf("runtime calls after drift = %#v, want none", runtime.calls)
	}
}

func TestServiceBootstrapAmbiguousStartFinalizesOrRetainsRecovery(t *testing.T) {
	t.Parallel()

	t.Run("queued readback finalizes", func(t *testing.T) {
		t.Parallel()
		store := confirmedPreferencesStore(t)
		probe := &fakeHomeProbe{metadata: logs.HomeMetadata{
			Path: filepath.Join(t.TempDir(), "target"), DeviceID: "device-target", Inode: 121,
		}}
		startErr := errors.New("start reply lost")
		runtime := &fakeHomeRuntime{startErr: startErr, status: BootstrapStatusQueued, store: store}
		service := newPreferencesService(t, store, probe, runtime)
		plan, err := service.PlanSwitch(context.Background(), probe.metadata.Path, HomeSwitchIndependentDatabase)
		if err != nil {
			t.Fatalf("PlanSwitch() error = %v", err)
		}
		got, err := service.ConfirmSwitch(context.Background(), plan.ID)
		if err != nil || got.PendingSwitch != nil || got.CodexHome.Generation != 2 {
			t.Fatalf("ConfirmSwitch(queued readback) = %#v, %v", got, err)
		}
	})

	t.Run("status failure retains journal", func(t *testing.T) {
		t.Parallel()
		store := confirmedPreferencesStore(t)
		probe := &fakeHomeProbe{metadata: logs.HomeMetadata{
			Path: filepath.Join(t.TempDir(), "target"), DeviceID: "device-target", Inode: 122,
		}}
		startErr := errors.New("start failed")
		statusErr := errors.New("status unavailable")
		runtime := &fakeHomeRuntime{startErr: startErr, statusErr: statusErr, store: store}
		service := newPreferencesService(t, store, probe, runtime)
		plan, err := service.PlanSwitch(context.Background(), probe.metadata.Path, HomeSwitchClearAndRebuild)
		if err != nil {
			t.Fatalf("PlanSwitch() error = %v", err)
		}
		got, err := service.ConfirmSwitch(context.Background(), plan.ID)
		if !errors.Is(err, ErrSwitchRecovery) || !errors.Is(err, startErr) || !errors.Is(err, statusErr) {
			t.Fatalf("ConfirmSwitch(status failure) error = %v", err)
		}
		if !strings.Contains(err.Error(), "read Home bootstrap status") {
			t.Fatalf("ConfirmSwitch(status failure) error = %v, want status operation context", err)
		}
		if got.PendingSwitch == nil || got.CodexHome.Generation != 2 {
			t.Fatalf("pending snapshot = %#v", got)
		}
		persisted, loadErr := store.LoadPreferences(context.Background())
		if loadErr != nil || persisted.PendingSwitch == nil {
			t.Fatalf("LoadPreferences(pending) = %#v, %v", persisted, loadErr)
		}
	})
}

func TestServiceFirstCASReadbackUsesAuthoritativeSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("pending committed despite uncertain response", func(t *testing.T) {
		t.Parallel()
		fileStore := confirmedPreferencesStore(t)
		store := &scriptedPreferencesStore{
			inner: fileStore,
			cas: func(call int, ctx context.Context, expected uint64, next Snapshot) error {
				if err := fileStore.CompareAndSwap(ctx, expected, next); err != nil {
					return err
				}
				if call == 1 {
					return ErrDurabilityUnknown
				}
				return nil
			},
		}
		probe := &fakeHomeProbe{metadata: logs.HomeMetadata{
			Path: filepath.Join(t.TempDir(), "target"), DeviceID: "device-target", Inode: 123,
		}}
		runtime := &fakeHomeRuntime{status: BootstrapStatusQueued, store: fileStore}
		service := newPreferencesService(t, store, probe, runtime)
		plan, err := service.PlanSwitch(context.Background(), probe.metadata.Path, HomeSwitchIndependentDatabase)
		if err != nil {
			t.Fatalf("PlanSwitch() error = %v", err)
		}
		got, err := service.ConfirmSwitch(context.Background(), plan.ID)
		if err != nil || got.PendingSwitch != nil || got.CodexHome.Generation != 2 {
			t.Fatalf("ConfirmSwitch(committed readback) = %#v, %v", got, err)
		}
		if !runtime.sawPendingAtStart {
			t.Fatal("bootstrap started without authoritative pending readback")
		}
	})

	t.Run("old snapshot remains authoritative", func(t *testing.T) {
		t.Parallel()
		fileStore := confirmedPreferencesStore(t)
		store := &scriptedPreferencesStore{
			inner: fileStore,
			cas: func(_ int, _ context.Context, _ uint64, _ Snapshot) error {
				return ErrPreferencesConflict
			},
		}
		probe := &fakeHomeProbe{metadata: logs.HomeMetadata{
			Path: filepath.Join(t.TempDir(), "target"), DeviceID: "device-target", Inode: 124,
		}}
		runtime := &fakeHomeRuntime{}
		service := newPreferencesService(t, store, probe, runtime)
		plan, err := service.PlanSwitch(context.Background(), probe.metadata.Path, HomeSwitchClearAndRebuild)
		if err != nil {
			t.Fatalf("PlanSwitch() error = %v", err)
		}
		got, err := service.ConfirmSwitch(context.Background(), plan.ID)
		if !errors.Is(err, ErrPreferencesConflict) || errors.Is(err, ErrSwitchRecovery) {
			t.Fatalf("ConfirmSwitch(old readback) error = %v, want conflict without recovery", err)
		}
		if got.CodexHome.Generation != 1 || got.PendingSwitch != nil || len(runtime.calls) != 0 {
			t.Fatalf("ConfirmSwitch(old readback) = %#v calls=%#v", got, runtime.calls)
		}
	})

	t.Run("unreadable snapshot retains recovery", func(t *testing.T) {
		t.Parallel()
		fileStore := confirmedPreferencesStore(t)
		readbackErr := errors.New("readback unavailable")
		store := &scriptedPreferencesStore{
			inner: fileStore,
			cas: func(_ int, _ context.Context, _ uint64, _ Snapshot) error {
				return ErrPreferencesConflict
			},
			load: func(call int, ctx context.Context) (Snapshot, error) {
				if call == 3 {
					return Snapshot{}, readbackErr
				}
				return fileStore.LoadPreferences(ctx)
			},
		}
		probe := &fakeHomeProbe{metadata: logs.HomeMetadata{
			Path: filepath.Join(t.TempDir(), "target"), DeviceID: "device-target", Inode: 125,
		}}
		runtime := &fakeHomeRuntime{}
		service := newPreferencesService(t, store, probe, runtime)
		plan, err := service.PlanSwitch(context.Background(), probe.metadata.Path, HomeSwitchClearAndRebuild)
		if err != nil {
			t.Fatalf("PlanSwitch() error = %v", err)
		}
		if _, err := service.ConfirmSwitch(context.Background(), plan.ID); !errors.Is(err, ErrSwitchRecovery) || !errors.Is(err, ErrPreferencesConflict) {
			t.Fatalf("ConfirmSwitch(unreadable readback) error = %v", err)
		}
		if len(runtime.calls) != 0 {
			t.Fatalf("runtime calls = %#v, want no guessed resume", runtime.calls)
		}
	})
}

func TestServiceFinalizeCASReadbackAcceptsCommittedResolution(t *testing.T) {
	t.Parallel()

	fileStore := confirmedPreferencesStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	store := &scriptedPreferencesStore{
		inner: fileStore,
		cas: func(call int, ctx context.Context, expected uint64, next Snapshot) error {
			if err := fileStore.CompareAndSwap(ctx, expected, next); err != nil {
				return err
			}
			if call == 3 {
				cancel()
				return ErrDurabilityUnknown
			}
			return nil
		},
	}
	probe := &fakeHomeProbe{metadata: logs.HomeMetadata{
		Path: filepath.Join(t.TempDir(), "target"), DeviceID: "device-target", Inode: 126,
	}}
	runtime := &fakeHomeRuntime{status: BootstrapStatusQueued, store: fileStore}
	service := newPreferencesService(t, store, probe, runtime)
	plan, err := service.PlanSwitch(context.Background(), probe.metadata.Path, HomeSwitchIndependentDatabase)
	if err != nil {
		t.Fatalf("PlanSwitch() error = %v", err)
	}
	got, err := service.ConfirmSwitch(ctx, plan.ID)
	if err != nil || got.PendingSwitch != nil || got.LastSwitch == nil || got.LastSwitch.SwitchID != plan.ID {
		t.Fatalf("ConfirmSwitch(finalize readback) = %#v, %v", got, err)
	}
}

func TestServiceRealProbeAndStoreNeverReadHomeContent(t *testing.T) {
	t.Parallel()

	store := confirmedPreferencesStore(t)
	home := filepath.Join(t.TempDir(), "synthetic-codex-home")
	if err := os.MkdirAll(filepath.Join(home, "sessions", "2026", "07"), 0o700); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	sessionPath := filepath.Join(home, "sessions", "2026", "07", "sentinel.jsonl")
	authPath := filepath.Join(home, "auth.json")
	sessionBytes := []byte("SENTINEL_SESSION_CONTENT_MUST_NOT_BE_READ\n")
	authBytes := []byte("SENTINEL_AUTH_TOKEN_MUST_NOT_BE_READ\n")
	if err := os.WriteFile(sessionPath, sessionBytes, 0o600); err != nil {
		t.Fatalf("os.WriteFile(session) error = %v", err)
	}
	if err := os.WriteFile(authPath, authBytes, 0o600); err != nil {
		t.Fatalf("os.WriteFile(auth) error = %v", err)
	}
	sessionBefore, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatalf("os.Stat(session before) error = %v", err)
	}
	authBefore, err := os.Stat(authPath)
	if err != nil {
		t.Fatalf("os.Stat(auth before) error = %v", err)
	}
	if err := os.Chmod(sessionPath, 0); err != nil {
		t.Fatalf("os.Chmod(session) error = %v", err)
	}
	if err := os.Chmod(authPath, 0); err != nil {
		t.Fatalf("os.Chmod(auth) error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(sessionPath, 0o600)
		_ = os.Chmod(authPath, 0o600)
	})

	runtime := &fakeHomeRuntime{status: BootstrapStatusQueued, store: store}
	service := newPreferencesService(t, store, logs.NewHomeProbe(), runtime)
	plan, err := service.PlanSwitch(context.Background(), home, HomeSwitchClearAndRebuild)
	if err != nil {
		t.Fatalf("PlanSwitch(real probe) error = %v", err)
	}
	if _, err := service.ConfirmSwitch(context.Background(), plan.ID); err != nil {
		t.Fatalf("ConfirmSwitch(real probe) error = %v", err)
	}
	sessionAfter, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatalf("os.Stat(session after) error = %v", err)
	}
	authAfter, err := os.Stat(authPath)
	if err != nil {
		t.Fatalf("os.Stat(auth after) error = %v", err)
	}
	if sessionAfter.Size() != sessionBefore.Size() || !sessionAfter.ModTime().Equal(sessionBefore.ModTime()) ||
		authAfter.Size() != authBefore.Size() || !authAfter.ModTime().Equal(authBefore.ModTime()) {
		t.Fatalf("content metadata changed: session %d/%v -> %d/%v; auth %d/%v -> %d/%v",
			sessionBefore.Size(), sessionBefore.ModTime(), sessionAfter.Size(), sessionAfter.ModTime(),
			authBefore.Size(), authBefore.ModTime(), authAfter.Size(), authAfter.ModTime())
	}
	if err := os.Chmod(sessionPath, 0o600); err != nil {
		t.Fatalf("os.Chmod(session restore) error = %v", err)
	}
	if err := os.Chmod(authPath, 0o600); err != nil {
		t.Fatalf("os.Chmod(auth restore) error = %v", err)
	}
	gotSession, err := os.ReadFile(sessionPath)
	if err != nil || !reflect.DeepEqual(gotSession, sessionBytes) {
		t.Fatalf("session bytes = %q, %v", gotSession, err)
	}
	gotAuth, err := os.ReadFile(authPath)
	if err != nil || !reflect.DeepEqual(gotAuth, authBytes) {
		t.Fatalf("auth bytes = %q, %v", gotAuth, err)
	}
}

func TestServiceIndependentSwitchBackReusesDetachedDataStore(t *testing.T) {
	t.Parallel()

	store := confirmedPreferencesStore(t)
	initial, err := store.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences(initial) error = %v", err)
	}
	probe := &fakeHomeProbe{metadata: logs.HomeMetadata{
		Path: filepath.Join(t.TempDir(), "home-b"), DeviceID: "device-b", Inode: 201,
	}}
	runtime := &fakeHomeRuntime{status: BootstrapStatusQueued, store: store}
	service := newPreferencesService(t, store, probe, runtime)
	toB, err := service.PlanSwitch(context.Background(), probe.metadata.Path, HomeSwitchIndependentDatabase)
	if err != nil {
		t.Fatalf("PlanSwitch(B) error = %v", err)
	}
	activeB, err := service.ConfirmSwitch(context.Background(), toB.ID)
	if err != nil {
		t.Fatalf("ConfirmSwitch(B) error = %v", err)
	}
	if activeB.CodexHome.DataStoreKey == DefaultDataStoreKey || len(activeB.DetachedHomes) != 1 {
		t.Fatalf("active B = %#v", activeB)
	}

	probe.metadata = logs.HomeMetadata{
		Path: initial.CodexHome.Source.Path, DeviceID: initial.CodexHome.Source.DeviceID,
		Inode: initial.CodexHome.Source.Inode,
	}
	toA, err := service.PlanSwitch(context.Background(), probe.metadata.Path, HomeSwitchIndependentDatabase)
	if err != nil {
		t.Fatalf("PlanSwitch(A) error = %v", err)
	}
	if toA.Target.DataStoreKey != DefaultDataStoreKey {
		t.Fatalf("PlanSwitch(A) data store = %q, want reuse %q", toA.Target.DataStoreKey, DefaultDataStoreKey)
	}
	activeA, err := service.ConfirmSwitch(context.Background(), toA.ID)
	if err != nil {
		t.Fatalf("ConfirmSwitch(A) error = %v", err)
	}
	if activeA.CodexHome.Generation != 3 || activeA.CodexHome.DataStoreKey != DefaultDataStoreKey ||
		len(activeA.DetachedHomes) != 1 || activeA.DetachedHomes[0].DataStoreKey != activeB.CodexHome.DataStoreKey ||
		!sameSourceIdentity(activeA.DetachedHomes[0].Source, activeB.CodexHome.Source) {
		t.Fatalf("active A after switch-back = %#v", activeA)
	}
}

func TestServiceConfirmSwitchReplaysCompletedPlanOnSameService(t *testing.T) {
	t.Parallel()

	store := confirmedPreferencesStore(t)
	runtime := &fakeHomeRuntime{status: BootstrapStatusQueued, store: store}
	metadata := logs.HomeMetadata{
		Path: filepath.Join(t.TempDir(), "home-b"), DeviceID: "device-b", Inode: 300,
	}
	service := newPreferencesService(t, store, &fakeHomeProbe{metadata: metadata}, runtime)
	plan, err := service.PlanSwitch(
		context.Background(), metadata.Path, HomeSwitchIndependentDatabase,
	)
	if err != nil {
		t.Fatalf("PlanSwitch() error = %v", err)
	}
	confirmed, err := service.ConfirmSwitch(context.Background(), plan.ID)
	if err != nil {
		t.Fatalf("ConfirmSwitch(first) error = %v", err)
	}
	replayed, err := service.ConfirmSwitch(context.Background(), plan.ID)
	if err != nil || !reflect.DeepEqual(replayed, confirmed) {
		t.Fatalf("ConfirmSwitch(replay) = %#v, %v, want %#v", replayed, err, confirmed)
	}
	persisted, err := store.LoadPreferences(context.Background())
	if err != nil || !reflect.DeepEqual(persisted, confirmed) {
		t.Fatalf("LoadPreferences() = %#v, %v, want %#v", persisted, err, confirmed)
	}
	assertRuntimeCallCount(t, runtime, "drain:", 1)
	assertRuntimeCallCount(t, runtime, "start:", 1)
}

func TestServiceConcurrentConfirmReplaysCompletedPlanOnSameService(t *testing.T) {
	t.Parallel()

	store := confirmedPreferencesStore(t)
	runtime := &fakeHomeRuntime{status: BootstrapStatusQueued, store: store}
	metadata := logs.HomeMetadata{
		Path: filepath.Join(t.TempDir(), "home-b"), DeviceID: "device-b", Inode: 300,
	}
	service := newPreferencesService(t, store, &fakeHomeProbe{metadata: metadata}, runtime)
	plan, err := service.PlanSwitch(
		context.Background(), metadata.Path, HomeSwitchIndependentDatabase,
	)
	if err != nil {
		t.Fatalf("PlanSwitch() error = %v", err)
	}
	results, errs := confirmSwitchesConcurrently(
		[]*Service{service, service},
		[]SwitchPlan{plan, plan},
	)
	for index, err := range errs {
		if err != nil {
			t.Fatalf("ConfirmSwitch(%d) error = %v", index, err)
		}
	}
	if !reflect.DeepEqual(results[0], results[1]) {
		t.Fatalf("ConfirmSwitch() results differ: %#v / %#v", results[0], results[1])
	}
	persisted, err := store.LoadPreferences(context.Background())
	if err != nil || !reflect.DeepEqual(persisted, results[0]) {
		t.Fatalf("LoadPreferences() = %#v, %v, want %#v", persisted, err, results[0])
	}
	assertRuntimeCallCount(t, runtime, "drain:", 1)
	assertRuntimeCallCount(t, runtime, "start:", 1)
}

func TestServiceConcurrentSwitchesCommitOnlyOneGeneration(t *testing.T) {
	t.Parallel()

	store := confirmedPreferencesStore(t)
	runtime := &fakeHomeRuntime{status: BootstrapStatusQueued, store: store}
	probes := []*fakeHomeProbe{
		{metadata: logs.HomeMetadata{Path: filepath.Join(t.TempDir(), "home-b"), DeviceID: "device-b", Inode: 301}},
		{metadata: logs.HomeMetadata{Path: filepath.Join(t.TempDir(), "home-c"), DeviceID: "device-c", Inode: 302}},
	}
	services := []*Service{
		newPreferencesService(t, store, probes[0], runtime),
		newPreferencesService(t, store, probes[1], runtime),
	}
	plans := make([]SwitchPlan, 2)
	for index := range services {
		plan, err := services[index].PlanSwitch(
			context.Background(), probes[index].metadata.Path, HomeSwitchIndependentDatabase,
		)
		if err != nil {
			t.Fatalf("PlanSwitch(%d) error = %v", index, err)
		}
		plans[index] = plan
	}
	start := make(chan struct{})
	results := make([]Snapshot, 2)
	errs := make([]error, 2)
	var wait sync.WaitGroup
	for index := range services {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results[index], errs[index] = services[index].ConfirmSwitch(context.Background(), plans[index].ID)
		}()
	}
	close(start)
	wait.Wait()
	winners, losers := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, ErrSwitchPlanStale):
			losers++
		default:
			t.Fatalf("ConfirmSwitch() unexpected error = %v", err)
		}
	}
	if winners != 1 || losers != 1 {
		t.Fatalf("winners/losers = %d/%d, want 1/1; results=%#v errors=%#v", winners, losers, results, errs)
	}
	persisted, err := store.LoadPreferences(context.Background())
	if err != nil || persisted.CodexHome.Generation != 2 || persisted.PendingSwitch != nil {
		t.Fatalf("LoadPreferences() = %#v, %v", persisted, err)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	starts := 0
	for _, call := range runtime.calls {
		if len(call) >= len("start:") && call[:len("start:")] == "start:" {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("runtime calls = %#v, want exactly one start", runtime.calls)
	}
}

func TestServiceConcurrentSameTargetSwitchesHaveOneRuntimeOwner(t *testing.T) {
	fileStore := confirmedPreferencesStore(t)
	guardCommitted := make(chan struct{})
	releaseOwner := make(chan struct{})
	ownerStore := &scriptedPreferencesStore{
		inner: fileStore,
		cas: func(call int, ctx context.Context, expected uint64, next Snapshot) error {
			if err := fileStore.CompareAndSwap(ctx, expected, next); err != nil {
				return err
			}
			if call == 1 {
				close(guardCommitted)
				<-releaseOwner
			}
			return nil
		},
	}
	runtime := &fakeHomeRuntime{status: BootstrapStatusQueued, store: fileStore}
	metadata := logs.HomeMetadata{
		Path: filepath.Join(t.TempDir(), "home-b"), DeviceID: "device-b", Inode: 401,
	}
	probe := &fakeHomeProbe{metadata: metadata}
	services := []*Service{
		newPreferencesServiceWithAttemptID(t, ownerStore, probe, runtime, strings.Repeat("a", 32)),
		newPreferencesServiceWithAttemptID(t, fileStore, probe, runtime, strings.Repeat("b", 32)),
	}
	plans := make([]SwitchPlan, len(services))
	for index := range services {
		plan, err := services[index].PlanSwitch(
			context.Background(), metadata.Path, HomeSwitchIndependentDatabase,
		)
		if err != nil {
			t.Fatalf("PlanSwitch(%d) error = %v", index, err)
		}
		plans[index] = plan
	}
	if plans[0].ID != plans[1].ID {
		t.Fatalf("same target plan IDs = %q/%q, want equal", plans[0].ID, plans[1].ID)
	}

	type confirmResult struct {
		snapshot Snapshot
		err      error
	}
	ownerResult := make(chan confirmResult, 1)
	go func() {
		value, err := services[0].ConfirmSwitch(context.Background(), plans[0].ID)
		ownerResult <- confirmResult{snapshot: value, err: err}
	}()
	<-guardCommitted

	contenderCtx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	contenderSnapshot, contenderErr := services[1].ConfirmSwitch(contenderCtx, plans[1].ID)
	cancel()
	if !errors.Is(contenderErr, context.DeadlineExceeded) {
		close(releaseOwner)
		t.Fatalf("ConfirmSwitch(contender with live owner) = %#v, %v, want context deadline",
			contenderSnapshot, contenderErr)
	}
	assertRuntimeCallCount(t, runtime, "drain:", 0)
	assertRuntimeCallCount(t, runtime, "resume:", 0)
	close(releaseOwner)
	confirmed := <-ownerResult
	if confirmed.err != nil || confirmed.snapshot.CodexHome.Generation != 2 {
		t.Fatalf("ConfirmSwitch(owner) = %#v, %v", confirmed.snapshot, confirmed.err)
	}
	idempotent, err := services[1].ConfirmSwitch(context.Background(), plans[1].ID)
	if err != nil || idempotent.CodexHome != confirmed.snapshot.CodexHome {
		t.Fatalf("ConfirmSwitch(after owner complete) = %#v, %v", idempotent, err)
	}
	assertRuntimeCallCount(t, runtime, "drain:", 1)
	assertRuntimeCallCount(t, runtime, "start:", 1)
	assertRuntimeCallCount(t, runtime, "resume:", 0)
}

func TestServiceConcurrentSameTargetDrainFailureHasOneRuntimeOwner(t *testing.T) {
	t.Parallel()

	store := confirmedPreferencesStore(t)
	drainErr := errors.New("drain failed")
	runtime := &fakeHomeRuntime{status: BootstrapStatusQueued, store: store, drainErr: drainErr}
	metadata := logs.HomeMetadata{
		Path: filepath.Join(t.TempDir(), "home-b"), DeviceID: "device-b", Inode: 402,
	}
	probe := &fakeHomeProbe{metadata: metadata}
	services := []*Service{
		newPreferencesService(t, store, probe, runtime),
		newPreferencesService(t, store, probe, runtime),
	}
	plans := make([]SwitchPlan, len(services))
	for index := range services {
		plan, err := services[index].PlanSwitch(
			context.Background(), metadata.Path, HomeSwitchIndependentDatabase,
		)
		if err != nil {
			t.Fatalf("PlanSwitch(%d) error = %v", index, err)
		}
		plans[index] = plan
	}
	_, errs := confirmSwitchesConcurrently(services, plans)
	drainFailures, staleFailures := 0, 0
	for _, err := range errs {
		switch {
		case errors.Is(err, drainErr):
			drainFailures++
		case errors.Is(err, ErrSwitchPlanStale):
			staleFailures++
		default:
			t.Fatalf("ConfirmSwitch() unexpected error = %v", err)
		}
	}
	if drainFailures != 1 || staleFailures != 1 {
		t.Fatalf("drain/stale failures = %d/%d, want 1/1; errors=%#v", drainFailures, staleFailures, errs)
	}
	assertRuntimeCallCount(t, runtime, "drain:", 1)
	assertRuntimeCallCount(t, runtime, "resume:", 1)
	assertRuntimeCallCount(t, runtime, "start:", 0)
	persisted, err := store.LoadPreferences(context.Background())
	if err != nil || persisted.CodexHome.Generation != 1 || persisted.PendingResume != nil ||
		persisted.PendingSwitch != nil || persisted.LastSwitch == nil ||
		persisted.LastSwitch.Outcome != HomeSwitchRolledBack {
		t.Fatalf("LoadPreferences() = %#v, %v", persisted, err)
	}
}

func TestServiceRecoverCannotPreemptLiveConfirmOwner(t *testing.T) {
	fileStore := confirmedPreferencesStore(t)
	guardCommitted := make(chan struct{})
	releaseOwner := make(chan struct{})
	ownerStore := &scriptedPreferencesStore{
		inner: fileStore,
		cas: func(call int, ctx context.Context, expected uint64, next Snapshot) error {
			if err := fileStore.CompareAndSwap(ctx, expected, next); err != nil {
				return err
			}
			if call == 1 {
				close(guardCommitted)
				<-releaseOwner
			}
			return nil
		},
	}
	probe := &fakeHomeProbe{metadata: logs.HomeMetadata{
		Path: filepath.Join(t.TempDir(), "home-b"), DeviceID: "device-b", Inode: 403,
	}}
	runtime := &fakeHomeRuntime{status: BootstrapStatusQueued, store: fileStore}
	owner := newPreferencesService(t, ownerStore, probe, runtime)
	recovery := newPreferencesService(t, fileStore, probe, runtime)
	plan, err := owner.PlanSwitch(context.Background(), probe.metadata.Path, HomeSwitchIndependentDatabase)
	if err != nil {
		t.Fatalf("PlanSwitch() error = %v", err)
	}

	type confirmResult struct {
		snapshot Snapshot
		err      error
	}
	ownerResult := make(chan confirmResult, 1)
	go func() {
		value, confirmErr := owner.ConfirmSwitch(context.Background(), plan.ID)
		ownerResult <- confirmResult{snapshot: value, err: confirmErr}
	}()
	<-guardCommitted

	recoveryCtx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	recovered, recoveryErr := recovery.RecoverSwitch(recoveryCtx)
	cancel()
	close(releaseOwner)
	confirmed := <-ownerResult

	if !errors.Is(recoveryErr, context.DeadlineExceeded) {
		t.Fatalf("RecoverSwitch(live owner) = %#v, %v, want context deadline", recovered, recoveryErr)
	}
	if confirmed.err != nil || confirmed.snapshot.CodexHome.Generation != 2 ||
		confirmed.snapshot.PendingResume != nil || confirmed.snapshot.PendingSwitch != nil ||
		confirmed.snapshot.LastSwitch == nil || confirmed.snapshot.LastSwitch.Outcome != HomeSwitchCompleted {
		t.Fatalf("ConfirmSwitch(owner) = %#v, %v", confirmed.snapshot, confirmed.err)
	}
	assertRuntimeCallCount(t, runtime, "resume:", 0)
	assertRuntimeCallCount(t, runtime, "drain:", 1)
	assertRuntimeCallCount(t, runtime, "start:", 1)
}

func confirmSwitchesConcurrently(services []*Service, plans []SwitchPlan) ([]Snapshot, []error) {
	start := make(chan struct{})
	results := make([]Snapshot, len(services))
	errs := make([]error, len(services))
	var wait sync.WaitGroup
	for index := range services {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results[index], errs[index] = services[index].ConfirmSwitch(context.Background(), plans[index].ID)
		}()
	}
	close(start)
	wait.Wait()
	return results, errs
}

func assertRuntimeCallCount(t *testing.T, runtime *fakeHomeRuntime, prefix string, want int) {
	t.Helper()
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	got := 0
	for _, call := range runtime.calls {
		if strings.HasPrefix(call, prefix) {
			got++
		}
	}
	if got != want {
		t.Fatalf("runtime calls with prefix %q = %d, want %d; calls=%#v", prefix, got, want, runtime.calls)
	}
}

type fakeHomeProbe struct {
	mu       sync.Mutex
	metadata logs.HomeMetadata
	err      error
}

type scriptedPreferencesStore struct {
	mu        sync.Mutex
	inner     PreferencesStore
	loadCalls int
	casCalls  int
	load      func(int, context.Context) (Snapshot, error)
	cas       func(int, context.Context, uint64, Snapshot) error
}

func (store *scriptedPreferencesStore) LoadPreferences(ctx context.Context) (Snapshot, error) {
	store.mu.Lock()
	store.loadCalls++
	call, load := store.loadCalls, store.load
	store.mu.Unlock()
	if load != nil {
		return load(call, ctx)
	}
	return store.inner.LoadPreferences(ctx)
}

func (store *scriptedPreferencesStore) CompareAndSwap(
	ctx context.Context,
	expected uint64,
	next Snapshot,
) error {
	store.mu.Lock()
	store.casCalls++
	call, compare := store.casCalls, store.cas
	store.mu.Unlock()
	if compare != nil {
		return compare(call, ctx, expected, next)
	}
	return store.inner.CompareAndSwap(ctx, expected, next)
}

func (store *scriptedPreferencesStore) AcquireSwitchLease(ctx context.Context) (SwitchExecutionLease, error) {
	return store.inner.AcquireSwitchLease(ctx)
}

func (probe *fakeHomeProbe) Probe(ctx context.Context, path string) (logs.HomeMetadata, error) {
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return logs.HomeMetadata{}, err
	}
	if probe.err != nil {
		return logs.HomeMetadata{}, probe.err
	}
	value := probe.metadata
	if value.Path == "" {
		value = logs.HomeMetadata{Path: path, DeviceID: "device-probe", Inode: 500}
	}
	return value, nil
}

type fakeHomeRuntime struct {
	mu                  sync.Mutex
	calls               []string
	drainErr            error
	startErr            error
	statusErr           error
	resumeErr           error
	status              BootstrapStatus
	request             BootstrapRequest
	store               *FileStore
	respectStartContext bool
	sawPendingAtStart   bool
}

func (runtime *fakeHomeRuntime) Drain(_ context.Context, generation uint64) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.calls = append(runtime.calls, fmt.Sprintf("drain:%d", generation))
	return runtime.drainErr
}

func (runtime *fakeHomeRuntime) StartBootstrap(ctx context.Context, request BootstrapRequest) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.calls = append(runtime.calls, fmt.Sprintf("start:%d:%s", request.Generation, request.SwitchID))
	runtime.request = request
	if runtime.store != nil {
		value, err := runtime.store.LoadPreferences(ctx)
		runtime.sawPendingAtStart = err == nil && value.PendingSwitch != nil &&
			value.PendingSwitch.SwitchID == request.SwitchID && value.CodexHome.Generation == request.Generation
	}
	if runtime.respectStartContext {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return runtime.startErr
}

func (runtime *fakeHomeRuntime) BootstrapStatus(_ context.Context, switchID string, generation uint64) (BootstrapStatus, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.calls = append(runtime.calls, fmt.Sprintf("status:%d:%s", generation, switchID))
	return runtime.status, runtime.statusErr
}

func (runtime *fakeHomeRuntime) Resume(_ context.Context, generation uint64) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.calls = append(runtime.calls, fmt.Sprintf("resume:%d", generation))
	return runtime.resumeErr
}

func confirmedPreferencesStore(t *testing.T) *FileStore {
	t.Helper()
	store, err := NewFileStore(filepath.Join(t.TempDir(), "private", "preferences.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	if err := store.Confirm(context.Background(), validSnapshot(filepath.Join(t.TempDir(), "active-home"))); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	return store
}

func newPreferencesService(t *testing.T, store PreferencesStore, probe HomeProbe, runtime HomeRuntime) *Service {
	return newPreferencesServiceWithAttemptID(t, store, probe, runtime, "")
}

func newPreferencesServiceWithAttemptID(
	t *testing.T,
	store PreferencesStore,
	probe HomeProbe,
	runtime HomeRuntime,
	attemptID string,
) *Service {
	t.Helper()
	var newAttemptID func() (string, error)
	if attemptID != "" {
		newAttemptID = func() (string, error) { return attemptID, nil }
	}
	service, err := NewService(ServiceConfig{
		Store: store, Probe: probe, Runtime: runtime,
		Clock: func() time.Time { return time.UnixMilli(1_720_000_100_000) }, NewAttemptID: newAttemptID,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func installPendingSwitch(t *testing.T, store *FileStore, strategy HomeSwitchStrategy) HomeSwitchJournal {
	t.Helper()
	base, err := store.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}
	target := CodexHomePreferences{
		Source: ConfirmedSource{
			Path: filepath.Join(t.TempDir(), "pending-home"), DeviceID: "device-pending", Inode: 900,
			ConfirmedAtMS: 1_720_000_100_000,
		},
		Generation: 2, DataStoreKey: "home-pending",
	}
	if strategy == HomeSwitchClearAndRebuild {
		target.DataStoreKey = base.CodexHome.DataStoreKey
	}
	pending := HomeSwitchJournal{
		SwitchID: "home-switch:pending", AttemptID: strings.Repeat("a", 32),
		Previous: base.CodexHome, Target: target,
		Strategy: strategy, StartedAtMS: 1_720_000_100_000,
	}
	next := base
	next.Revision++
	next.CodexHome = target
	next.PendingSwitch = &pending
	if err := store.CompareAndSwap(context.Background(), base.Revision, next); err != nil {
		t.Fatalf("CompareAndSwap(pending) error = %v", err)
	}
	return pending
}
