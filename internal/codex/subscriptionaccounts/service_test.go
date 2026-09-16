package subscriptionaccounts

import (
	"strings"
	"testing"
	"time"
)

func TestProjectKeepsSameEmailDetectedAccountsIndependent(t *testing.T) {
	t.Parallel()
	email := "shared@example.com"
	key := "shared@example.com"
	scopeA := strings.Repeat("a", 64)
	scopeB := strings.Repeat("b", 64)
	snapshot, err := Project(Records{
		Binding: Binding{State: BindingPending},
		Detected: []DetectedAccount{
			{
				AccountScope: scopeA, DetectedAccountID: "11111111-1111-4111-8111-111111111111",
				DetectedEmail: &email, EmailMatchKey: &key,
				AutomaticPlanState: AutomaticPlanUnknown, Revision: 1,
			},
			{
				AccountScope: scopeB, DetectedAccountID: "22222222-2222-4222-8222-222222222222",
				DetectedEmail: &email, EmailMatchKey: &key,
				AutomaticPlanState: AutomaticPlanUnknown, Revision: 1,
			},
		},
		Manual: []ManualEntry{{
			ManualEntryID: "33333333-3333-4333-8333-333333333333",
			Email:         &email, EmailMatchKey: &key, Revision: 1,
			CreatedAtMS: 1, UpdatedAtMS: 1,
		}},
	}, 0, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Accounts) != 3 {
		t.Fatalf("accounts = %d, want 3 independent rows", len(snapshot.Accounts))
	}
	if len(snapshot.LinkCandidates) != 2 {
		t.Fatalf("candidates = %#v, want both detected to the standalone manual", snapshot.LinkCandidates)
	}
	for _, account := range snapshot.Accounts {
		if account.Linked || account.Current {
			t.Fatalf("unexpected merge or current: %#v", account)
		}
		if account.accountScope == scopeA && account.AccountID != "11111111-1111-4111-8111-111111111111" {
			t.Fatalf("scope A public id changed: %#v", account)
		}
	}
}

func TestProjectLinkedItemHidesStandaloneManualAndMarksCurrent(t *testing.T) {
	t.Parallel()
	scope := strings.Repeat("c", 64)
	detectedID := "44444444-4444-4444-8444-444444444444"
	manualID := "55555555-5555-4555-8555-555555555555"
	detectedEmail := "detected@example.com"
	manualEmail := "manual@example.com"
	alias := "Home"
	plus := PlanPlus
	pro := PlanPro20X
	date := "2099-12-31"
	kind := DateKindNextRenewal
	snapshot, err := Project(Records{
		Binding: Binding{State: BindingConfirmed, AccountScope: &scope, BindingGeneration: 4},
		Detected: []DetectedAccount{{
			AccountScope: scope, DetectedAccountID: detectedID,
			DetectedEmail: &detectedEmail, EmailMatchKey: pointer("detected@example.com"),
			AutomaticPlan: clonePlan(pro), AutomaticPlanState: AutomaticPlanKnown,
			AutomaticPlanObservedAtMS: pointer[int64](10), Revision: 2,
		}},
		Manual: []ManualEntry{{
			ManualEntryID: manualID, Email: &manualEmail, EmailMatchKey: pointer("manual@example.com"),
			Alias: &alias, ManualPlan: &plus, MembershipDate: &date, DateKind: &kind,
			Revision: 3, CreatedAtMS: 1, UpdatedAtMS: 2,
		}},
		Links: []Link{{AccountScope: scope, ManualEntryID: manualID, Revision: 7, LinkedAtMS: 1, UpdatedAtMS: 1}},
	}, time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC).UnixMilli(), "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Accounts) != 1 || len(snapshot.LinkCandidates) != 0 {
		t.Fatalf("linked snapshot = %#v", snapshot)
	}
	account := snapshot.Accounts[0]
	if !account.Current || !account.Detected || !account.HasManual || !account.Linked {
		t.Fatalf("flags = %#v", account)
	}
	if account.AccountID != detectedID || account.DisplayEmail == nil || *account.DisplayEmail != manualEmail {
		t.Fatalf("identity/display = %#v", account)
	}
	if account.ResolvedPlan == nil || *account.ResolvedPlan != PlanPlus || account.ResolvedPlanSource != ValueSourceManual {
		t.Fatalf("resolved plan = %#v", account)
	}
	if account.DateState != DateStateFuture || account.DayDelta == nil || account.DateSource != ValueSourceManual {
		t.Fatalf("date = %#v", account)
	}
	if snapshot.AutomaticDateCapability != AutomaticDateCapabilityManualOnly {
		t.Fatalf("capability = %s", snapshot.AutomaticDateCapability)
	}
}

func TestProjectSortsCurrentFirstThenInsensitiveKeys(t *testing.T) {
	t.Parallel()
	scope := strings.Repeat("d", 64)
	snapshot, err := Project(Records{
		Binding: Binding{State: BindingConfirmed, AccountScope: &scope, BindingGeneration: 1},
		Detected: []DetectedAccount{{
			AccountScope: scope, DetectedAccountID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
			AutomaticPlanState: AutomaticPlanUnavailable, Revision: 1,
		}},
		Manual: []ManualEntry{
			{ManualEntryID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Alias: pointer("zeta"), Revision: 1, CreatedAtMS: 1, UpdatedAtMS: 1, Email: pointer("z@example.com"), EmailMatchKey: pointer("z@example.com")},
			{ManualEntryID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", Alias: pointer("Alpha"), Revision: 1, CreatedAtMS: 1, UpdatedAtMS: 1, Email: pointer("a@example.com"), EmailMatchKey: pointer("a@example.com")},
		},
	}, 0, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{snapshot.Accounts[0].AccountID, snapshot.Accounts[1].AccountID, snapshot.Accounts[2].AccountID}; got[0] != "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb" ||
		got[1] != "cccccccc-cccc-4ccc-8ccc-cccccccccccc" ||
		got[2] != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" {
		t.Fatalf("sort order = %v", got)
	}
}

func TestCurrentAccountRejectsStaleFence(t *testing.T) {
	t.Parallel()
	scope := strings.Repeat("e", 64)
	records := Records{
		Binding: Binding{State: BindingConfirmed, AccountScope: &scope, BindingGeneration: 9},
		Detected: []DetectedAccount{{
			AccountScope: scope, DetectedAccountID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
			AutomaticPlanState: AutomaticPlanUnavailable, Revision: 1,
		}},
	}
	account, err := CurrentAccount(records, AccountFence{AccountScope: scope, BindingGeneration: 9}, 0, "UTC")
	if err != nil || account == nil || !account.Current {
		t.Fatalf("matching fence = %#v %v", account, err)
	}
	if _, err := CurrentAccount(records, AccountFence{AccountScope: scope, BindingGeneration: 8}, 0, "UTC"); err != ErrAccountBindingChanged {
		t.Fatalf("stale generation error = %v", err)
	}
	records.Binding.State = BindingPending
	if _, err := CurrentAccount(records, AccountFence{AccountScope: scope, BindingGeneration: 9}, 0, "UTC"); err != ErrAccountBindingChanged {
		t.Fatalf("pending fence error = %v", err)
	}
}

func TestProjectDoesNotLeakPrivateScopeOnPublicFields(t *testing.T) {
	t.Parallel()
	scope := strings.Repeat("f", 64)
	snapshot, err := Project(Records{
		Binding: Binding{State: BindingUnknown},
		Detected: []DetectedAccount{{
			AccountScope: scope, DetectedAccountID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee",
			AutomaticPlanState: AutomaticPlanUnavailable, Revision: 1,
		}},
	}, 0, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	account := snapshot.Accounts[0]
	if account.AccountID == scope || (account.DetectedAccountID != nil && *account.DetectedAccountID == scope) {
		t.Fatalf("public ids used private scope: %#v", account)
	}
}
