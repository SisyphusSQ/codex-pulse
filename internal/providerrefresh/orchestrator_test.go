package providerrefresh

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/cursorprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/grokprovider"
)

type stubProvider struct {
	result   ProviderResult
	started  chan struct{}
	release  chan struct{}
	calls    atomic.Int64
	delayErr error
}

func (stub *stubProvider) RefreshProvider(ctx context.Context, trigger string) ProviderResult {
	stub.calls.Add(1)
	if stub.started != nil {
		close(stub.started)
	}
	if stub.release != nil {
		select {
		case <-stub.release:
		case <-ctx.Done():
			return cancelledProvider(stub.result.Provider)
		}
	}
	if stub.delayErr != nil {
		return ProviderResult{Provider: stub.result.Provider, Status: StatusFailed, Components: stub.result.Components}
	}
	return stub.result
}

type recordingInvalidation struct {
	mu      sync.Mutex
	domains []core.InvalidationDomain
}

func (recorder *recordingInvalidation) Notify(_ context.Context, domain core.InvalidationDomain) error {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.domains = append(recorder.domains, domain)
	return nil
}

func TestOrchestratorReturnsStableProviderOrderAndPartialResults(t *testing.T) {
	t.Parallel()
	cursor := &stubProvider{result: ProviderResult{
		Provider: agentprovider.Cursor, Status: StatusRefreshed,
		Components: []ComponentResult{{Component: ComponentCursorDashboard, Status: StatusRefreshed, Attempted: true}},
	}}
	grok := &stubProvider{result: ProviderResult{
		Provider: agentprovider.Grok, Status: StatusSkippedNoCredentials,
		Components: []ComponentResult{{Component: ComponentGrokBilling, Status: StatusSkippedNoCredentials}},
	}}
	codex := &stubProvider{result: ProviderResult{
		Provider: agentprovider.Codex, Status: StatusFailed,
		Components: []ComponentResult{{Component: ComponentCodexQuota, Status: StatusFailed, Attempted: true, ReasonCode: ReasonNetwork}},
	}}
	orchestrator, err := New(Config{Cursor: cursor, Grok: grok, Codex: codex})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := orchestrator.Refresh(context.Background(), TriggerManual)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if receipt.Trigger != TriggerManual || len(receipt.Providers) != 3 {
		t.Fatalf("receipt = %#v", receipt)
	}
	if receipt.Providers[0].Provider != agentprovider.Codex ||
		receipt.Providers[1].Provider != agentprovider.Cursor ||
		receipt.Providers[2].Provider != agentprovider.Grok {
		t.Fatalf("provider order = %#v", receipt.Providers)
	}
	if receipt.Providers[0].Status != StatusFailed ||
		receipt.Providers[1].Status != StatusRefreshed ||
		receipt.Providers[2].Status != StatusSkippedNoCredentials {
		t.Fatalf("partial statuses = %#v", receipt.Providers)
	}
}

func TestOrchestratorDoesNotCancelSiblingsOnFailure(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	codex := &stubProvider{
		result: ProviderResult{Provider: agentprovider.Codex, Status: StatusFailed,
			Components: []ComponentResult{{Component: ComponentCodexQuota, Status: StatusFailed}}},
		started: started,
		release: release,
	}
	cursor := &stubProvider{result: ProviderResult{
		Provider: agentprovider.Cursor, Status: StatusRefreshed,
		Components: []ComponentResult{{Component: ComponentCursorLocal, Status: StatusRefreshed, Attempted: true}},
	}}
	grok := &stubProvider{result: ProviderResult{
		Provider: agentprovider.Grok, Status: StatusRefreshed,
		Components: []ComponentResult{{Component: ComponentGrokLocal, Status: StatusRefreshed, Attempted: true}},
	}}
	orchestrator, err := New(Config{Codex: codex, Cursor: cursor, Grok: grok})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan Receipt, 1)
	go func() {
		receipt, _ := orchestrator.Refresh(context.Background(), TriggerScheduled)
		done <- receipt
	}()
	<-started
	if cursor.calls.Load() == 0 && grok.calls.Load() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if cursor.calls.Load() == 0 || grok.calls.Load() == 0 {
		close(release)
		t.Fatal("sibling providers were not started while Codex was blocked")
	}
	close(release)
	receipt := <-done
	if receipt.Providers[1].Status != StatusRefreshed || receipt.Providers[2].Status != StatusRefreshed {
		t.Fatalf("sibling results = %#v", receipt.Providers)
	}
}

func TestOrchestratorMergesOverlappingRefresh(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	cursor := &stubProvider{
		result: ProviderResult{Provider: agentprovider.Cursor, Status: StatusRefreshed,
			Components: []ComponentResult{{Component: ComponentCursorLocal, Status: StatusRefreshed, Attempted: true}}},
		started: started,
		release: release,
	}
	orchestrator, err := New(Config{
		Cursor: cursor,
		Grok:   &stubProvider{result: UnavailableProvider(agentprovider.Grok, ComponentGrokLocal, ComponentGrokBilling)},
		Codex:  &stubProvider{result: UnavailableProvider(agentprovider.Codex, ComponentCodexLocal)},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan struct{})
	go func() {
		_, _ = orchestrator.Refresh(context.Background(), TriggerScheduled)
		close(firstDone)
	}()
	<-started
	secondDone := make(chan Receipt, 1)
	go func() {
		receipt, _ := orchestrator.Refresh(context.Background(), TriggerManual)
		secondDone <- receipt
	}()
	time.Sleep(20 * time.Millisecond)
	close(release)
	<-firstDone
	<-secondDone
	if cursor.calls.Load() != 1 {
		t.Fatalf("overlapping refreshes = %d, want 1", cursor.calls.Load())
	}
}

func TestOrchestratorCoalescesInvalidation(t *testing.T) {
	t.Parallel()
	invalidation := &recordingInvalidation{}
	orchestrator, err := New(Config{
		Invalidation: invalidation,
		Codex: &stubProvider{result: ProviderResult{
			Provider: agentprovider.Codex, Status: StatusRefreshed,
			Components: []ComponentResult{
				{Component: ComponentCodexLocal, Status: StatusRefreshed, Attempted: true},
				{Component: ComponentCodexQuota, Status: StatusRefreshed, Attempted: true},
			},
		}},
		Cursor: &stubProvider{result: ProviderResult{
			Provider: agentprovider.Cursor, Status: StatusRefreshed,
			Components: []ComponentResult{{Component: ComponentCursorDashboard, Status: StatusRefreshed, Attempted: true}},
		}},
		Grok: &stubProvider{result: ProviderResult{
			Provider: agentprovider.Grok, Status: StatusNotDue,
			Components: []ComponentResult{{Component: ComponentGrokBilling, Status: StatusNotDue}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orchestrator.Refresh(context.Background(), TriggerForeground); err != nil {
		t.Fatal(err)
	}
	invalidation.mu.Lock()
	defer invalidation.mu.Unlock()
	if len(invalidation.domains) != 2 {
		t.Fatalf("invalidations = %#v, want coalesced index+quota", invalidation.domains)
	}
}

func TestClassifyDoesNotConfuseSkipAndFailure(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		err       error
		attempted bool
		want      string
	}{
		{name: "not due", want: StatusNotDue},
		{name: "no cursor creds", err: cursorprovider.ErrDesktopAuthUnavailable, want: StatusSkippedNoCredentials},
		{name: "expired local token", err: grokprovider.ErrAuthExpired, want: StatusSkippedNoCredentials},
		{name: "remote reject", err: cursorprovider.ErrDashboardAuthRejected, attempted: true, want: StatusFailed},
		{name: "disabled via collector", want: StatusNotDue},
	}
	for _, testCase := range cases {
		got := ClassifyOnlineError(testCase.err, testCase.attempted)
		if got.Status != testCase.want {
			t.Fatalf("%s status = %s, want %s", testCase.name, got.Status, testCase.want)
		}
	}
	disabled := ComponentResult{Status: StatusSkippedDisabled, ReasonCode: ReasonDisabled}
	if disabled.Status == StatusFailed || disabled.Status == StatusSkippedNoCredentials {
		t.Fatal("disabled must stay distinct")
	}
}

type countingRoundTripper struct {
	calls atomic.Int64
}

func (tripper *countingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	tripper.calls.Add(1)
	return nil, errors.New("http client must not be called")
}

func TestMissingCredentialsDoNotCallHTTP(t *testing.T) {
	t.Parallel()
	transport := &countingRoundTripper{}
	client := &http.Client{Transport: transport, Timeout: time.Second}
	cursorClient, err := cursorprovider.NewDashboardClient(cursorprovider.DashboardClientConfig{
		BaseURL:     "https://api2.cursor.sh",
		HTTPClient:  client,
		TokenSource: failingTokenSource{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cursorClient.GetCurrentPeriodUsage(context.Background()); !errors.Is(err, cursorprovider.ErrDesktopAuthUnavailable) {
		t.Fatalf("GetCurrentPeriodUsage() error = %v", err)
	}
	grokClient, err := grokprovider.NewBillingClient(grokprovider.BillingClientConfig{
		BaseURL: "https://cli-chat-proxy.grok.com/v1", HTTPClient: client,
		TokenSource: failingGrokTokenSource{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := grokClient.GetCredits(context.Background()); !errors.Is(err, grokprovider.ErrAuthUnavailable) {
		t.Fatalf("GetCredits() error = %v", err)
	}
	if transport.calls.Load() != 0 {
		t.Fatalf("HTTP calls = %d, want 0", transport.calls.Load())
	}
}

type failingTokenSource struct{}

func (failingTokenSource) ReadAccessToken(context.Context) (cursorprovider.DesktopAccessToken, error) {
	return cursorprovider.DesktopAccessToken{}, cursorprovider.ErrDesktopAuthUnavailable
}

type failingGrokTokenSource struct{}

func (failingGrokTokenSource) ReadAccessToken() (grokprovider.AccessToken, error) {
	return grokprovider.AccessToken{}, grokprovider.ErrAuthUnavailable
}

func TestInvalidTriggerReturnsValidation(t *testing.T) {
	t.Parallel()
	orchestrator, err := New(Config{
		Cursor: &stubProvider{result: UnavailableProvider(agentprovider.Cursor, ComponentCursorLocal)},
		Grok:   &stubProvider{result: UnavailableProvider(agentprovider.Grok, ComponentGrokLocal)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orchestrator.Refresh(context.Background(), "all"); err == nil {
		t.Fatal("unknown trigger must fail closed")
	}
}
