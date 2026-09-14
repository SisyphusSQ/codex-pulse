package quota

import (
	"context"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestCurrentQueryUnconfirmedStatesReturnEmptyOnlineFacts(t *testing.T) {
	t.Parallel()

	repository, service := newCurrentQueryTestService(t)
	nowMS := time.Now().UnixMilli()
	resetAtMS := nowMS + 5*60*60*1_000
	recordCurrentQueryWham(t, repository, "legacy-default", nowMS, 88, 11, resetAtMS, resetAtMS+7*24*60*60*1_000)

	assertEmptyBinding := func(state store.CodexAccountBindingState) {
		t.Helper()
		response := queryCurrentAt(t, service, nowMS)
		if response.Binding.State != state || len(response.Windows) != 0 ||
			response.ResetCredits.AvailableCount != nil ||
			response.Sources[0].UnknownReason == nil ||
			*response.Sources[0].UnknownReason != CurrentUnknownBindingUnavailable {
			t.Fatalf("state %s leaked online facts: %#v", state, response)
		}
		if response.Windows == nil {
			t.Fatalf("state %s windows = nil, want empty slice", state)
		}
	}

	assertEmptyBinding(store.CodexAccountBindingUnknown)
	if _, err := repository.MarkCodexAccountBindingPending(
		context.Background(), nowMS+1, store.CodexAccountBindingReasonStartup,
	); err != nil {
		t.Fatalf("MarkCodexAccountBindingPending() error = %v", err)
	}
	assertEmptyBinding(store.CodexAccountBindingPending)
	if _, _, err := repository.SetCodexAccountBindingUnavailable(
		context.Background(), store.CodexAccountBindingSignedOut, nowMS+2, store.CodexAccountBindingReasonSignedOut,
	); err != nil {
		t.Fatalf("SetCodexAccountBindingUnavailable(signed_out) error = %v", err)
	}
	assertEmptyBinding(store.CodexAccountBindingSignedOut)
	if _, _, err := repository.SetCodexAccountBindingUnavailable(
		context.Background(), store.CodexAccountBindingIdentityUnavailable, nowMS+3,
		store.CodexAccountBindingReasonMissingAccountID,
	); err != nil {
		t.Fatalf("SetCodexAccountBindingUnavailable(identity_unavailable) error = %v", err)
	}
	assertEmptyBinding(store.CodexAccountBindingIdentityUnavailable)

	confirmCurrentQueryAccount(t, repository, "acct-test-a", nowMS+4)
	first := queryCurrentAt(t, service, nowMS)
	if first.Binding.State != store.CodexAccountBindingConfirmed || len(first.Windows) != 0 ||
		first.ResetCredits.AvailableCount != nil {
		t.Fatalf("first confirmed query reused legacy default: %#v", first)
	}
}

func TestCurrentQueryAccountSwitchABARestoresOriginalObservationTimestamp(t *testing.T) {
	t.Parallel()

	repository, service := newCurrentQueryTestService(t)
	nowMS := time.Now().UnixMilli()
	resetA := nowMS + 5*60*60*1_000
	resetB := nowMS + 4*60*60*1_000
	scopeA, generationA := confirmCurrentQueryAccount(t, repository, "acct-test-a", nowMS)
	recordCurrentQueryAppServer(t, repository, "binding-a", nowMS, 40, -1, resetA, 0)
	first := queryCurrentAt(t, service, nowMS+2)
	if first.AccountScope != scopeA || first.Binding.BindingGeneration != generationA ||
		len(first.Windows) != 1 || first.Windows[0].UsedPercent == nil || *first.Windows[0].UsedPercent != 40 ||
		len(first.Windows[0].Explanations) != 1 || first.Windows[0].Explanations[0].ObservedAtMS != nowMS {
		t.Fatalf("first A current = %#v", first)
	}

	scopeB, generationB := confirmCurrentQueryAccount(t, repository, "acct-test-b", nowMS+1)
	recordCurrentQueryAppServer(t, repository, "binding-b", nowMS+1, 77, -1, resetB, 0)
	activeB := queryCurrentAt(t, service, nowMS+2)
	if activeB.AccountScope != scopeB || activeB.Binding.BindingGeneration != generationB ||
		len(activeB.Windows) != 1 || activeB.Windows[0].UsedPercent == nil || *activeB.Windows[0].UsedPercent != 77 ||
		activeB.Windows[0].Explanations[0].ObservedAtMS != nowMS+1 {
		t.Fatalf("B current leaked A: %#v", activeB)
	}

	scopeA2, generationA2 := confirmCurrentQueryAccount(t, repository, "acct-test-a", nowMS+2)
	restored := queryCurrentAt(t, service, nowMS+3)
	if restored.AccountScope != scopeA2 || restored.Binding.BindingGeneration != generationA2 ||
		generationA2 <= generationB || len(restored.Windows) != 1 ||
		restored.Windows[0].UsedPercent == nil || *restored.Windows[0].UsedPercent != 40 ||
		restored.Windows[0].Explanations[0].ObservedAtMS != nowMS {
		t.Fatalf("restored A current = %#v", restored)
	}
}

func TestCurrentQueryStaleHistoryKeepsOriginalTimestamp(t *testing.T) {
	t.Parallel()

	repository, service := newCurrentQueryTestService(t)
	nowMS := time.Now().UnixMilli()
	resetAtMS := nowMS + 5*60*60*1_000
	recordCurrentQueryAppServer(t, repository, "stale-history", nowMS, 40, -1, resetAtMS, 0)
	staleAtMS := nowMS + store.DefaultQuotaArbitrationRule().FreshForMS + 1
	response := queryCurrentAt(t, service, staleAtMS)
	if len(response.Windows) != 1 || response.Windows[0].Freshness != store.QuotaCurrentStale ||
		response.Windows[0].UsedPercent == nil || *response.Windows[0].UsedPercent != 40 ||
		response.Windows[0].Explanations[0].ObservedAtMS != nowMS {
		t.Fatalf("stale history was rewritten: %#v", response)
	}
}
