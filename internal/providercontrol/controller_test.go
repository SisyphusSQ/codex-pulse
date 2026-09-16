package providercontrol

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
)

type staticPreferences struct {
	snapshot preferences.Snapshot
}

func (store staticPreferences) LoadPreferences(context.Context) (preferences.Snapshot, error) {
	return store.snapshot, nil
}

type recordingProbes struct {
	mu     sync.Mutex
	codex  int
	cursor int
	grok   int
	result ProbeResult
}

func (probes *recordingProbes) set() ProbeSet {
	if probes.result.State == "" {
		probes.result = ProbeResult{State: DiscoveryAvailable, ReasonCode: ReasonAvailable}
	}
	return ProbeSet{
		Codex: func(context.Context, *preferences.CodexHomePreferences) ProbeResult {
			probes.mu.Lock()
			defer probes.mu.Unlock()
			probes.codex++
			return probes.result
		},
		Cursor: func(context.Context) ProbeResult {
			probes.mu.Lock()
			defer probes.mu.Unlock()
			probes.cursor++
			return probes.result
		},
		Grok: func(context.Context) ProbeResult {
			probes.mu.Lock()
			defer probes.mu.Unlock()
			probes.grok++
			return probes.result
		},
	}
}

func testPreferences(intents preferences.ProviderPreferences) staticPreferences {
	return staticPreferences{snapshot: preferences.Snapshot{
		SchemaVersion: preferences.CurrentPreferencesSchemaVersion,
		Revision:      1,
		Onboarding:    preferences.OnboardingPreferences{Version: preferences.CurrentOnboardingVersion, Completed: true},
		Providers:     intents,
		Online:        preferences.DefaultOnlinePreferences(),
		Refresh:       preferences.DefaultRefreshPreferences(),
		Updates:       preferences.DefaultUpdatePreferences(),
		UI:            preferences.DefaultUIPreferences(),
	}}
}

func TestEffectiveMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		intent    preferences.ProviderIntent
		discovery DiscoveryState
		disabling bool
		effective EffectiveState
		reason    string
	}{
		{preferences.ProviderIntentAuto, DiscoveryAvailable, false, EffectiveEnabled, ReasonAvailable},
		{preferences.ProviderIntentAuto, DiscoveryMissing, false, EffectiveUnavailable, string(DiscoveryMissing)},
		{preferences.ProviderIntentEnabled, DiscoveryAvailable, false, EffectiveEnabled, ReasonAvailable},
		{preferences.ProviderIntentEnabled, DiscoveryInaccessible, false, EffectiveUnavailable, string(DiscoveryInaccessible)},
		{preferences.ProviderIntentDisabled, DiscoveryAvailable, false, EffectiveDisabled, ReasonDisabled},
		{preferences.ProviderIntentDisabled, DiscoveryMissing, true, EffectiveDisabling, ReasonDisabling},
	}
	for _, test := range tests {
		got, reason := EffectiveFor(test.intent, test.discovery, test.disabling)
		if got != test.effective || reason != test.reason {
			t.Fatalf("EffectiveFor(%s,%s,%v) = %s/%s, want %s/%s",
				test.intent, test.discovery, test.disabling, got, reason, test.effective, test.reason)
		}
	}
}

func TestBeginRejectsDisabledAndUnavailable(t *testing.T) {
	t.Parallel()
	probes := &recordingProbes{result: ProbeResult{State: DiscoveryMissing, ReasonCode: ReasonNotFound}}
	controller, err := NewController(testPreferences(preferences.DefaultProviderPreferences()), probes.set())
	if err != nil {
		t.Fatalf("NewController() error = %v", err)
	}
	if _, err := controller.RefreshDiscovery(context.Background(), "startup"); err != nil {
		t.Fatalf("RefreshDiscovery() error = %v", err)
	}
	if _, err := controller.Begin(context.Background(), agentprovider.Cursor); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Begin(auto missing) error = %v, want unavailable", err)
	}
	result, err := controller.Apply(context.Background(), preferences.ProviderPreferences{
		Codex:  preferences.ProviderPreference{Intent: preferences.ProviderIntentDisabled},
		Cursor: preferences.ProviderPreference{Intent: preferences.ProviderIntentDisabled},
		Grok:   preferences.ProviderPreference{Intent: preferences.ProviderIntentDisabled},
	})
	if err != nil || !result.Applied {
		t.Fatalf("Apply(disabled) = %#v, %v", result, err)
	}
	if _, err := controller.Begin(context.Background(), agentprovider.Grok); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Begin(disabled) error = %v, want disabled", err)
	}
	if got := controller.EnabledProviders(); len(got) != 0 {
		t.Fatalf("EnabledProviders() = %v, want empty", got)
	}
}

func TestLateWriterCannotCommitAfterDisable(t *testing.T) {
	t.Parallel()
	probes := &recordingProbes{}
	controller, err := NewController(testPreferences(preferences.DefaultProviderPreferences()), probes.set())
	if err != nil {
		t.Fatalf("NewController() error = %v", err)
	}
	if _, err := controller.RefreshDiscovery(context.Background(), "startup"); err != nil {
		t.Fatalf("RefreshDiscovery() error = %v", err)
	}
	operation, err := controller.Begin(context.Background(), agentprovider.Cursor)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	entered := make(chan struct{})
	committed := make(chan error, 1)
	go func() {
		close(entered)
		time.Sleep(50 * time.Millisecond)
		_, commitErr := operation.BeginCommit()
		committed <- commitErr
		operation.Finish()
	}()
	<-entered
	result, err := controller.Apply(context.Background(), preferences.ProviderPreferences{
		Codex:  preferences.ProviderPreference{Intent: preferences.ProviderIntentAuto},
		Cursor: preferences.ProviderPreference{Intent: preferences.ProviderIntentDisabled},
		Grok:   preferences.ProviderPreference{Intent: preferences.ProviderIntentAuto},
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !result.Applied {
		t.Fatalf("Apply() = %#v, want applied after operation drain", result)
	}
	if commitErr := <-committed; !errors.Is(commitErr, ErrStaleGeneration) {
		t.Fatalf("BeginCommit(after disable) = %v, want stale generation", commitErr)
	}
	if _, err := controller.Begin(context.Background(), agentprovider.Cursor); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Begin(after disable) = %v, want disabled", err)
	}
}

func TestDisableWaitsForHeldCommitLease(t *testing.T) {
	t.Parallel()
	probes := &recordingProbes{}
	controller, err := NewController(testPreferences(preferences.DefaultProviderPreferences()), probes.set())
	if err != nil {
		t.Fatalf("NewController() error = %v", err)
	}
	if _, err := controller.RefreshDiscovery(context.Background(), "startup"); err != nil {
		t.Fatalf("RefreshDiscovery() error = %v", err)
	}
	operation, err := controller.Begin(context.Background(), agentprovider.Grok)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	finish, err := operation.BeginCommit()
	if err != nil {
		t.Fatalf("BeginCommit() error = %v", err)
	}
	done := make(chan TransitionResult, 1)
	go func() {
		result, applyErr := controller.Apply(context.Background(), preferences.ProviderPreferences{
			Codex:  preferences.ProviderPreference{Intent: preferences.ProviderIntentAuto},
			Cursor: preferences.ProviderPreference{Intent: preferences.ProviderIntentAuto},
			Grok:   preferences.ProviderPreference{Intent: preferences.ProviderIntentDisabled},
		})
		if applyErr != nil {
			t.Errorf("Apply() error = %v", applyErr)
		}
		done <- result
	}()
	select {
	case result := <-done:
		t.Fatalf("Apply returned before commit lease released: %#v", result)
	case <-time.After(80 * time.Millisecond):
	}
	finish()
	operation.Finish()
	select {
	case result := <-done:
		if !result.Applied || result.ReconcileRequired {
			t.Fatalf("Apply() = %#v, want applied", result)
		}
		state, err := controller.ProviderState(agentprovider.Grok)
		if err != nil || state.Effective != EffectiveDisabled {
			t.Fatalf("Grok state = %#v, %v", state, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Apply did not return after commit lease release")
	}
}

func TestDisableTimeoutContinuesDrainInBackground(t *testing.T) {
	t.Parallel()
	probes := &recordingProbes{}
	controller, err := NewController(testPreferences(preferences.DefaultProviderPreferences()), probes.set())
	if err != nil {
		t.Fatalf("NewController() error = %v", err)
	}
	if _, err := controller.RefreshDiscovery(context.Background(), "startup"); err != nil {
		t.Fatalf("RefreshDiscovery() error = %v", err)
	}
	operation, err := controller.Begin(context.Background(), agentprovider.Cursor)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	applyCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result, err := controller.Apply(applyCtx, preferences.ProviderPreferences{
		Codex:  preferences.ProviderPreference{Intent: preferences.ProviderIntentAuto},
		Cursor: preferences.ProviderPreference{Intent: preferences.ProviderIntentDisabled},
		Grok:   preferences.ProviderPreference{Intent: preferences.ProviderIntentAuto},
	})
	if err != nil || result.Applied || !result.ReconcileRequired {
		t.Fatalf("Apply(timeout) = %#v, %v, want reconcile required", result, err)
	}
	state, err := controller.ProviderState(agentprovider.Cursor)
	if err != nil || state.Effective != EffectiveDisabling {
		t.Fatalf("Cursor state during drain = %#v, %v", state, err)
	}
	operation.Finish()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state, err = controller.ProviderState(agentprovider.Cursor)
		if err == nil && state.Effective == EffectiveDisabled {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("Cursor state after background drain = %#v, %v", state, err)
}

func TestDiscoveryReasonAndGenerationChangeWithVisibleState(t *testing.T) {
	t.Parallel()
	probes := &recordingProbes{result: ProbeResult{
		State: DiscoveryMissing, ReasonCode: ReasonNotFound,
	}}
	controller, err := NewController(testPreferences(preferences.ProviderPreferences{
		Codex:  preferences.ProviderPreference{Intent: preferences.ProviderIntentDisabled},
		Cursor: preferences.ProviderPreference{Intent: preferences.ProviderIntentDisabled},
		Grok:   preferences.ProviderPreference{Intent: preferences.ProviderIntentDisabled},
	}), probes.set())
	if err != nil {
		t.Fatalf("NewController() error = %v", err)
	}
	if _, err := controller.RefreshDiscovery(context.Background(), "startup"); err != nil {
		t.Fatalf("RefreshDiscovery() error = %v", err)
	}
	before, err := controller.ProviderState(agentprovider.Cursor)
	if err != nil {
		t.Fatalf("ProviderState() error = %v", err)
	}
	probes.mu.Lock()
	probes.result = ProbeResult{State: DiscoveryInaccessible, ReasonCode: ReasonPermissionDenied}
	probes.mu.Unlock()
	if _, err := controller.RefreshDiscovery(context.Background(), "settings"); err != nil {
		t.Fatalf("RefreshDiscovery(update) error = %v", err)
	}
	after, err := controller.ProviderState(agentprovider.Cursor)
	if err != nil || after.ReasonCode != ReasonDisabled || after.Discovery != DiscoveryInaccessible ||
		after.Generation <= before.Generation {
		t.Fatalf("visible discovery update = before %#v after %#v err=%v", before, after, err)
	}
}

func TestExplicitDisabledSourceReappearingStaysDisabled(t *testing.T) {
	t.Parallel()
	probes := &recordingProbes{result: ProbeResult{State: DiscoveryMissing, ReasonCode: ReasonNotFound}}
	controller, err := NewController(testPreferences(preferences.ProviderPreferences{
		Codex:  preferences.ProviderPreference{Intent: preferences.ProviderIntentDisabled},
		Cursor: preferences.ProviderPreference{Intent: preferences.ProviderIntentDisabled},
		Grok:   preferences.ProviderPreference{Intent: preferences.ProviderIntentDisabled},
	}), probes.set())
	if err != nil {
		t.Fatalf("NewController() error = %v", err)
	}
	if _, err := controller.Apply(context.Background(), preferences.ProviderPreferences{
		Codex:  preferences.ProviderPreference{Intent: preferences.ProviderIntentDisabled},
		Cursor: preferences.ProviderPreference{Intent: preferences.ProviderIntentDisabled},
		Grok:   preferences.ProviderPreference{Intent: preferences.ProviderIntentDisabled},
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	probes.result = ProbeResult{State: DiscoveryAvailable, ReasonCode: ReasonAvailable}
	if _, err := controller.RefreshDiscovery(context.Background(), "wake"); err != nil {
		t.Fatalf("RefreshDiscovery() error = %v", err)
	}
	state, err := controller.ProviderState(agentprovider.Cursor)
	if err != nil || state.Intent != preferences.ProviderIntentDisabled ||
		state.Discovery != DiscoveryAvailable || state.Effective != EffectiveDisabled {
		t.Fatalf("rediscovered disabled = %#v, %v", state, err)
	}
}

func TestDiscoveryProbesDoNotInvokeCollectors(t *testing.T) {
	t.Parallel()
	var collectorCalls, networkCalls, credentialCalls, sessionCalls int
	probes := ProbeSet{
		Codex: func(context.Context, *preferences.CodexHomePreferences) ProbeResult {
			return ProbeResult{State: DiscoveryAvailable, ReasonCode: ReasonAvailable}
		},
		Cursor: func(context.Context) ProbeResult {
			return ProbeResult{State: DiscoveryAvailable, ReasonCode: ReasonAvailable}
		},
		Grok: func(context.Context) ProbeResult {
			return ProbeResult{State: DiscoveryAvailable, ReasonCode: ReasonAvailable}
		},
	}
	controller, err := NewController(testPreferences(preferences.DefaultProviderPreferences()), probes)
	if err != nil {
		t.Fatalf("NewController() error = %v", err)
	}
	if _, err := controller.RefreshDiscovery(context.Background(), "settings"); err != nil {
		t.Fatalf("RefreshDiscovery() error = %v", err)
	}
	if collectorCalls != 0 || networkCalls != 0 || credentialCalls != 0 || sessionCalls != 0 {
		t.Fatalf("discovery invoked business readers: collector=%d network=%d credential=%d session=%d",
			collectorCalls, networkCalls, credentialCalls, sessionCalls)
	}
}

func TestDefaultPathProbeClassifiesMetadataOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "home")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	file := filepath.Join(root, "state.vscdb")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if got := probeDirectory(dir); got.State != DiscoveryAvailable {
		t.Fatalf("probeDirectory(existing) = %#v", got)
	}
	if got := probeFile(file); got.State != DiscoveryAvailable {
		t.Fatalf("probeFile(existing) = %#v", got)
	}
	if got := probeDirectory(filepath.Join(root, "missing")); got.State != DiscoveryMissing || got.ReasonCode != ReasonNotFound {
		t.Fatalf("probeDirectory(missing) = %#v", got)
	}
	if got := probeDirectory("relative"); got.State != DiscoveryInvalid || got.ReasonCode != ReasonUnsafePath {
		t.Fatalf("probeDirectory(relative) = %#v", got)
	}
	if got := probeDirectory(file); got.State != DiscoveryInvalid || got.ReasonCode != ReasonInvalidType {
		t.Fatalf("probeDirectory(file) = %#v", got)
	}
}
