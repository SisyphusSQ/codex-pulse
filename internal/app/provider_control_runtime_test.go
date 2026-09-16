package app

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/cursorprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/providercontrol"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
)

func TestApplicationControlRuntimeStartsWithoutCodexHome(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	preferenceStore, err := preferences.NewFileStore(filepath.Join(t.TempDir(), "private", "preferences.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	snapshot, err := preferences.NewV3Snapshot(nil, preferences.DefaultOnlinePreferences())
	if err != nil {
		t.Fatalf("NewV3Snapshot() error = %v", err)
	}
	if err := preferenceStore.InitializePreferences(ctx, snapshot); err != nil {
		t.Fatalf("InitializePreferences() error = %v", err)
	}
	controller, err := providercontrol.NewController(preferenceStore, isolatedAvailableProbes())
	if err != nil {
		t.Fatalf("NewController() error = %v", err)
	}
	runtime, err := startApplicationControlRuntime(ctx, ApplicationControlRuntimeConfig{
		Preferences: preferenceStore, Controller: controller, EventTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("startApplicationControlRuntime() error = %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	if err := runtime.syncCodexWorker(ctx); err != nil {
		t.Fatalf("syncCodexWorker() error = %v", err)
	}
	if runtime.currentWorker() != nil {
		t.Fatal("Codex worker must stay stopped when Home is missing")
	}
	state, err := controller.Snapshot(agentprovider.Codex)
	if err != nil {
		t.Fatalf("Snapshot(codex) error = %v", err)
	}
	if state.Effective != providercontrol.EffectiveUnavailable {
		t.Fatalf("codex effective = %s, want unavailable", state.Effective)
	}
	if _, err := runtime.RequestQuotaRefresh(ctx, "quota"); !errors.Is(err, basequery.ErrUnavailable) &&
		!errors.Is(err, basequery.ErrProviderDisabled) {
		t.Fatalf("RequestQuotaRefresh(no home) error = %v, want unavailable", err)
	}
}

func TestApplicationControlRuntimeDisablesProviderAdmission(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	preferenceStore, err := preferences.NewFileStore(filepath.Join(t.TempDir(), "private", "preferences.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	snapshot, err := preferences.NewV3Snapshot(nil, preferences.DefaultOnlinePreferences())
	if err != nil {
		t.Fatalf("NewV3Snapshot() error = %v", err)
	}
	if err := preferenceStore.InitializePreferences(ctx, snapshot); err != nil {
		t.Fatalf("InitializePreferences() error = %v", err)
	}
	controller, err := providercontrol.NewController(preferenceStore, isolatedAvailableProbes())
	if err != nil {
		t.Fatalf("NewController() error = %v", err)
	}
	runtime, err := startApplicationControlRuntime(ctx, ApplicationControlRuntimeConfig{
		Preferences: preferenceStore, Controller: controller, EventTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("startApplicationControlRuntime() error = %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	current, err := preferenceStore.LoadPreferences(ctx)
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}
	receipt, err := runtime.UpdateSettings(ctx, core.SettingsUpdateRequest{
		ExpectedRevision: strconv.FormatUint(current.Revision, 10),
		Providers: []core.SettingsProviderUpdate{
			{Provider: agentprovider.Codex, Intent: providercontrol.ProtoIntent(preferences.ProviderIntentDisabled)},
			{Provider: agentprovider.Cursor, Intent: providercontrol.ProtoIntent(preferences.ProviderIntentEnabled)},
			{Provider: agentprovider.Grok, Intent: providercontrol.ProtoIntent(preferences.ProviderIntentEnabled)},
		},
		Online: core.SettingsOnlineUpdate{
			QuotaEnabled: true, ResetCreditsEnabled: true, CursorOnlineEnabled: true,
			GrokQuotaEnabled: true, GrokAutoRefreshEnabled: true,
		},
		Refresh: core.SettingsRefreshUpdate{
			QuotaIntervalSeconds:        current.Refresh.QuotaIntervalSeconds,
			ResetCreditsIntervalSeconds: current.Refresh.ResetCreditsIntervalSeconds,
			ReconcileIntervalSeconds:    current.Refresh.ReconcileIntervalSeconds,
			JSONLDebounceMilliseconds:   current.Refresh.JSONLDebounceMilliseconds,
		},
		Updates: core.SettingsUpdatesUpdate{
			AutoCheckEnabled:     current.Updates.AutoCheckEnabled,
			CheckIntervalSeconds: current.Updates.CheckIntervalSeconds,
			Channel:              string(current.Updates.Channel),
		},
		UI: core.SettingsUIUpdate{
			Locale: string(current.UI.Locale), LaunchBehavior: string(current.UI.LaunchBehavior),
			OverviewRange: string(current.UI.OverviewRange),
		},
	})
	if err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}
	if receipt.Result != core.SettingsUpdateApplied {
		t.Fatalf("UpdateSettings() = %#v, want applied", receipt)
	}
	if _, err := runtime.AccountSnapshot(ctx, core.AccountSnapshotQuery{
		Scope: agentprovider.Scope{Provider: agentprovider.Codex},
	}); !errors.Is(err, basequery.ErrProviderDisabled) {
		t.Fatalf("AccountSnapshot(disabled codex) error = %v, want provider_disabled", err)
	}
	operation, err := controller.Begin(ctx, agentprovider.Cursor)
	if err != nil {
		t.Fatalf("Begin(cursor) error = %v", err)
	}
	operation.Finish()
}

func TestProviderDisableCancelsAndDrainsAccountRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	preferenceStore, err := preferences.NewFileStore(filepath.Join(t.TempDir(), "private", "preferences.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	snapshot, err := preferences.NewV3Snapshot(nil, preferences.DefaultOnlinePreferences())
	if err != nil {
		t.Fatalf("NewV3Snapshot() error = %v", err)
	}
	if err := preferenceStore.InitializePreferences(ctx, snapshot); err != nil {
		t.Fatalf("InitializePreferences() error = %v", err)
	}
	controller, err := providercontrol.NewController(preferenceStore, isolatedAvailableProbes())
	if err != nil {
		t.Fatalf("NewController() error = %v", err)
	}
	runtime, err := startApplicationControlRuntime(ctx, ApplicationControlRuntimeConfig{
		Preferences: preferenceStore, Controller: controller, EventTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("startApplicationControlRuntime() error = %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	started := make(chan struct{})
	runtime.cursorAccountReader = func(readCtx context.Context) (cursorprovider.DesktopAccountSnapshot, error) {
		close(started)
		<-readCtx.Done()
		return cursorprovider.DesktopAccountSnapshot{}, readCtx.Err()
	}
	readDone := make(chan error, 1)
	go func() {
		_, readErr := runtime.AccountSnapshot(ctx, core.AccountSnapshotQuery{
			Scope: agentprovider.Scope{Provider: agentprovider.Cursor},
		})
		readDone <- readErr
	}()
	<-started
	result, err := controller.Apply(ctx, preferences.ProviderPreferences{
		Codex:  preferences.ProviderPreference{Intent: preferences.ProviderIntentAuto},
		Cursor: preferences.ProviderPreference{Intent: preferences.ProviderIntentDisabled},
		Grok:   preferences.ProviderPreference{Intent: preferences.ProviderIntentAuto},
	})
	if err != nil || !result.Applied {
		t.Fatalf("Apply(disable cursor) = %#v, %v", result, err)
	}
	select {
	case readErr := <-readDone:
		if !errors.Is(readErr, context.Canceled) {
			t.Fatalf("AccountSnapshot() error = %v, want cancelled", readErr)
		}
	case <-time.After(time.Second):
		t.Fatal("AccountSnapshot was not drained before disable completed")
	}
}

func isolatedAvailableProbes() providercontrol.ProbeSet {
	available := func(context.Context) providercontrol.ProbeResult {
		return providercontrol.ProbeResult{
			State: providercontrol.DiscoveryAvailable, ReasonCode: providercontrol.ReasonAvailable,
		}
	}
	return providercontrol.ProbeSet{
		Codex: func(_ context.Context, home *preferences.CodexHomePreferences) providercontrol.ProbeResult {
			if home == nil {
				return providercontrol.ProbeResult{
					State: providercontrol.DiscoveryMissing, ReasonCode: providercontrol.ReasonNotFound,
				}
			}
			return available(context.Background())
		},
		Cursor: available,
		Grok:   available,
	}
}
