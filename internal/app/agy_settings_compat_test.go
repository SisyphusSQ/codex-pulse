package app

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/providercontrol"
)

func TestUpdateSettingsPreservesAGYOnlineAndIntentForLegacyClients(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	prefPath := filepath.Join(t.TempDir(), "private", "preferences.json")
	prefStore, err := preferences.NewFileStore(prefPath)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	initial, err := preferences.NewCurrentSnapshot(nil, preferences.DefaultOnlinePreferences())
	if err != nil {
		t.Fatalf("NewCurrentSnapshot() error = %v", err)
	}
	if err := prefStore.InitializePreferences(ctx, initial); err != nil {
		t.Fatalf("InitializePreferences() error = %v", err)
	}
	available := func(context.Context) providercontrol.ProbeResult {
		return providercontrol.ProbeResult{State: providercontrol.DiscoveryAvailable, ReasonCode: providercontrol.ReasonAvailable}
	}
	controller, err := providercontrol.NewController(prefStore, providercontrol.ProbeSet{
		Codex: func(context.Context, *preferences.CodexHomePreferences) providercontrol.ProbeResult {
			return available(ctx)
		},
		Cursor: available,
		Grok:   available,
		DSH:    available,
		AGY:    available,
	})
	if err != nil {
		t.Fatalf("NewController() error = %v", err)
	}
	controlRuntime, err := startApplicationControlRuntime(ctx, ApplicationControlRuntimeConfig{
		Preferences: prefStore,
		Controller:  controller,
	})
	if err != nil {
		t.Fatalf("startApplicationControlRuntime() error = %v", err)
	}
	t.Cleanup(func() { _ = controlRuntime.Close(context.Background()) })

	// 1. 旧四客户端请求不带 agy_online_enabled（nil）也不带 agy provider，应原样保留 AGYOnlineEnabled=true 与 agy=auto
	legacyReq := core.SettingsUpdateRequest{
		ExpectedRevision: strconv.FormatUint(initial.Revision, 10),
		Providers: []core.SettingsProviderUpdate{
			{Provider: agentprovider.Codex, Intent: "PROVIDER_INTENT_AUTO"},
			{Provider: agentprovider.Cursor, Intent: "PROVIDER_INTENT_AUTO"},
			{Provider: agentprovider.Grok, Intent: "PROVIDER_INTENT_AUTO"},
			{Provider: agentprovider.DSH, Intent: "PROVIDER_INTENT_AUTO"},
		},
		Online: core.SettingsOnlineUpdate{
			QuotaEnabled: true, ResetCreditsEnabled: true, CursorOnlineEnabled: true,
			GrokQuotaEnabled: true, GrokAutoRefreshEnabled: true,
			AGYOnlineEnabled: nil,
		},
		CodexAccounts: core.SettingsCodexAccountsUpdate{RetainQuotaHistory: true},
		Refresh: core.SettingsRefreshUpdate{
			QuotaIntervalSeconds: 300, ResetCreditsIntervalSeconds: 1800,
			ReconcileIntervalSeconds: 1800, JSONLDebounceMilliseconds: 4000,
		},
		Updates: core.SettingsUpdatesUpdate{AutoCheckEnabled: true, CheckIntervalSeconds: 3600, Channel: "stable"},
		UI:      core.SettingsUIUpdate{LaunchBehavior: "tray", OverviewRange: "quota_week", Locale: preferences.LocaleSystem},
	}
	receipt1, err := controlRuntime.UpdateSettings(ctx, legacyReq)
	if err != nil {
		t.Fatalf("UpdateSettings(legacy) error = %v", err)
	}
	afterLegacy, err := prefStore.LoadPreferences(ctx)
	if err != nil {
		t.Fatalf("LoadPreferences(after legacy) error = %v", err)
	}
	if !afterLegacy.Online.AGYOnlineEnabled || afterLegacy.Providers.AGY.Intent != preferences.ProviderIntentAuto {
		t.Fatalf("legacy update overwrote AGY settings: online=%#v providers=%#v", afterLegacy.Online, afterLegacy.Providers)
	}

	// 2. 新五客户端显式关闭 AGY 主开关和 agy_online_enabled=false
	disabledVal := false
	v6Req := legacyReq
	v6Req.ExpectedRevision = receipt1.Revision
	v6Req.Providers = append(v6Req.Providers, core.SettingsProviderUpdate{
		Provider: agentprovider.AGY, Intent: "PROVIDER_INTENT_DISABLED",
	})
	v6Req.Online.AGYOnlineEnabled = &disabledVal
	if _, err := controlRuntime.UpdateSettings(ctx, v6Req); err != nil {
		t.Fatalf("UpdateSettings(v6) error = %v", err)
	}
	afterV6, err := prefStore.LoadPreferences(ctx)
	if err != nil {
		t.Fatalf("LoadPreferences(after v6) error = %v", err)
	}
	if afterV6.Online.AGYOnlineEnabled || afterV6.Providers.AGY.Intent != preferences.ProviderIntentDisabled {
		t.Fatalf("v6 update did not persist AGY changes: online=%#v providers=%#v", afterV6.Online, afterV6.Providers)
	}
	agyState, err := controller.ProviderState(agentprovider.AGY)
	if err != nil || agyState.Effective != providercontrol.EffectiveDisabled {
		t.Fatalf("AGY controller state = %#v, %v, want disabled", agyState, err)
	}
}
