package store

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/accountbinding"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptionaccounts"
)

func TestCodexSubscriptionDuplicateEmailDoesNotMergeOrUniqueConflict(t *testing.T) {
	t.Parallel()
	repository := openAccountBindingRepository(t)
	key := ensureTestScopeKey(t, repository)
	scopeA, err := accountbinding.DeriveScope(key, []byte("acct-test-a"))
	if err != nil {
		t.Fatal(err)
	}
	scopeB, err := accountbinding.DeriveScope(key, []byte("acct-test-b"))
	if err != nil {
		t.Fatal(err)
	}
	email := "shared@example.com"
	if _, _, err := repository.ConfirmCodexAccountBinding(t.Context(), scopeA, 1, CodexAccountBindingReasonStartup); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.RecordDetectedCodexSubscriptionProfile(t.Context(), fenceFor(t, repository, scopeA), detectedEmailProfile(email, subscriptionaccounts.PlanPlus)); err != nil {
		t.Fatalf("profile A: %v", err)
	}
	if _, err := repository.MarkCodexAccountBindingPending(t.Context(), 2, CodexAccountBindingReasonAccountChanged); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.ConfirmCodexAccountBinding(t.Context(), scopeB, 3, CodexAccountBindingReasonAccountChanged); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.RecordDetectedCodexSubscriptionProfile(t.Context(), fenceFor(t, repository, scopeB), detectedEmailProfile(email, subscriptionaccounts.PlanPlus)); err != nil {
		t.Fatalf("profile B: %v", err)
	}
	manualA := uuid.NewString()
	manualB := uuid.NewString()
	if mutation, err := repository.CreateCodexSubscriptionManualEntry(t.Context(), standaloneCreate(manualA, email)); err != nil || mutation.Result != string(subscriptionaccounts.MutationApplied) {
		t.Fatalf("create A = %#v %v", mutation, err)
	}
	if mutation, err := repository.CreateCodexSubscriptionManualEntry(t.Context(), standaloneCreate(manualB, email)); err != nil || mutation.Result != string(subscriptionaccounts.MutationApplied) {
		t.Fatalf("create B = %#v %v", mutation, err)
	}
	records, err := repository.ListCodexSubscriptionRecords(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(records.Detected) != 2 || len(records.Manual) != 2 || len(records.Links) != 0 {
		t.Fatalf("records = %#v", records)
	}
}

func TestCodexSubscriptionCreateExactReplayAndIDReuse(t *testing.T) {
	t.Parallel()
	repository := openAccountBindingRepository(t)
	id := uuid.NewString()
	first, err := repository.CreateCodexSubscriptionManualEntry(t.Context(), standaloneCreate(id, "user@example.com"))
	if err != nil || first.Result != string(subscriptionaccounts.MutationApplied) {
		t.Fatalf("create = %#v %v", first, err)
	}
	replay, err := repository.CreateCodexSubscriptionManualEntry(t.Context(), standaloneCreate(id, "user@example.com"))
	if err != nil || replay.Result != string(subscriptionaccounts.MutationNoop) {
		t.Fatalf("replay = %#v %v", replay, err)
	}
	conflict, err := repository.CreateCodexSubscriptionManualEntry(t.Context(), standaloneCreate(id, "other@example.com"))
	if err != nil || conflict.Result != string(subscriptionaccounts.MutationConflict) ||
		conflict.Reason == nil || *conflict.Reason != subscriptionaccounts.ReasonRequestIDReused {
		t.Fatalf("reuse = %#v %v", conflict, err)
	}
	if count := scalarCount(t, repository.database, `SELECT COUNT(*) FROM codex_subscription_manual_entries`); count != 1 {
		t.Fatalf("manual rows = %d", count)
	}
}

func TestCodexSubscriptionStaleRevisionDoesNotPartialWrite(t *testing.T) {
	t.Parallel()
	repository := openAccountBindingRepository(t)
	id := uuid.NewString()
	if _, err := repository.CreateCodexSubscriptionManualEntry(t.Context(), standaloneCreate(id, "user@example.com")); err != nil {
		t.Fatal(err)
	}
	stale := int64(99)
	mutation, err := repository.UpdateCodexSubscriptionManualEntry(t.Context(), CodexSubscriptionManualUpdate{
		AccountID:              id,
		Fields:                 subscriptionaccounts.ManualFields{Email: stringPtr("new@example.com")},
		ExpectedManualRevision: &stale,
		NowMS:                  2,
	})
	if err != nil || mutation.Result != string(subscriptionaccounts.MutationConflict) ||
		mutation.Reason == nil || *mutation.Reason != subscriptionaccounts.ReasonRevisionChanged {
		t.Fatalf("stale update = %#v %v", mutation, err)
	}
	email := scalarText(t, repository.database, `SELECT email FROM codex_subscription_manual_entries WHERE manual_entry_id = '`+id+`'`)
	if email != "user@example.com" {
		t.Fatalf("email mutated on conflict: %q", email)
	}
}

func TestCodexSubscriptionLateProfileWriteIsFenced(t *testing.T) {
	t.Parallel()
	repository := openAccountBindingRepository(t)
	scopeA, scopeB := confirmTwoScopes(t, repository)
	binding, err := repository.CodexAccountBinding(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	staleFence := CodexAccountFence{AccountScope: scopeA, BindingGeneration: 1}
	if binding.BindingGeneration == 1 {
		t.Fatal("expected B to advance generation")
	}
	_, _, err = repository.RecordDetectedCodexSubscriptionProfile(
		t.Context(), staleFence, detectedEmailProfile("late-a@example.com", subscriptionaccounts.PlanPlus),
	)
	if !errors.Is(err, ErrCodexAccountBindingChanged) {
		t.Fatalf("late A write error = %v", err)
	}
	if strings.Contains(err.Error(), "late-a@example.com") || strings.Contains(err.Error(), scopeA) {
		t.Fatalf("fence error leaked identity: %v", err)
	}
	records, err := repository.ListCodexSubscriptionRecords(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, detected := range records.Detected {
		if detected.AccountScope == scopeB && detected.DetectedEmail != nil {
			t.Fatalf("B was polluted: %#v", detected)
		}
		if detected.AccountScope == scopeA && detected.DetectedEmail != nil {
			t.Fatalf("rejected A write still landed: %#v", detected)
		}
	}
}

func TestConfirmCodexAccountBindingReusesDetectedPublicID(t *testing.T) {
	t.Parallel()
	repository := openAccountBindingRepository(t)
	key := ensureTestScopeKey(t, repository)
	scopeA, err := accountbinding.DeriveScope(key, []byte("acct-test-a"))
	if err != nil {
		t.Fatal(err)
	}
	scopeB, err := accountbinding.DeriveScope(key, []byte("acct-test-b"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.ConfirmCodexAccountBinding(t.Context(), scopeA, 1, CodexAccountBindingReasonStartup); err != nil {
		t.Fatal(err)
	}
	idA := scalarText(t, repository.database, `SELECT detected_account_id FROM codex_subscription_detected_accounts WHERE account_scope = '`+scopeA+`'`)
	if _, err := repository.MarkCodexAccountBindingPending(t.Context(), 2, CodexAccountBindingReasonAccountChanged); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.ConfirmCodexAccountBinding(t.Context(), scopeB, 3, CodexAccountBindingReasonAccountChanged); err != nil {
		t.Fatal(err)
	}
	idB := scalarText(t, repository.database, `SELECT detected_account_id FROM codex_subscription_detected_accounts WHERE account_scope = '`+scopeB+`'`)
	if _, err := repository.MarkCodexAccountBindingPending(t.Context(), 4, CodexAccountBindingReasonAccountChanged); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.ConfirmCodexAccountBinding(t.Context(), scopeA, 5, CodexAccountBindingReasonStartup); err != nil {
		t.Fatal(err)
	}
	restored := scalarText(t, repository.database, `SELECT detected_account_id FROM codex_subscription_detected_accounts WHERE account_scope = '`+scopeA+`'`)
	if restored != idA || restored == idB || idA == idB {
		t.Fatalf("public ids A=%q B=%q restored=%q", idA, idB, restored)
	}
	if count := scalarCount(t, repository.database, `SELECT COUNT(*) FROM codex_subscription_detected_accounts`); count != 2 {
		t.Fatalf("detected rows = %d", count)
	}
}

func TestCodexSubscriptionDetectedFirstEditCreatesSupplementAtomically(t *testing.T) {
	t.Parallel()
	repository := openAccountBindingRepository(t)
	key := ensureTestScopeKey(t, repository)
	scopeA, err := accountbinding.DeriveScope(key, []byte("acct-test-a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.ConfirmCodexAccountBinding(t.Context(), scopeA, 1, CodexAccountBindingReasonStartup); err != nil {
		t.Fatal(err)
	}
	records, err := repository.ListCodexSubscriptionRecords(t.Context())
	if err != nil || len(records.Detected) != 1 {
		t.Fatalf("list = %#v %v", records, err)
	}
	detectedID := records.Detected[0].DetectedAccountID
	manualID := uuid.NewString()
	alias := "Home"
	plus := subscriptionaccounts.PlanPlus
	applied, err := repository.UpdateCodexSubscriptionManualEntry(t.Context(), CodexSubscriptionManualUpdate{
		AccountID:        detectedID,
		NewManualEntryID: &manualID,
		Fields: subscriptionaccounts.ManualFields{
			Email: stringPtr("manual@example.com"),
			Alias: &alias,
			Plan:  &plus,
		},
		NowMS: 20,
	})
	if err != nil || applied.Result != string(subscriptionaccounts.MutationApplied) {
		t.Fatalf("first edit = %#v %v", applied, err)
	}
	replay, err := repository.UpdateCodexSubscriptionManualEntry(t.Context(), CodexSubscriptionManualUpdate{
		AccountID:        detectedID,
		NewManualEntryID: &manualID,
		Fields: subscriptionaccounts.ManualFields{
			Email: stringPtr("manual@example.com"),
			Alias: &alias,
			Plan:  &plus,
		},
		NowMS: 21,
	})
	if err != nil || replay.Result != string(subscriptionaccounts.MutationNoop) {
		t.Fatalf("first edit replay = %#v %v", replay, err)
	}
	reuse, err := repository.UpdateCodexSubscriptionManualEntry(t.Context(), CodexSubscriptionManualUpdate{
		AccountID:        detectedID,
		NewManualEntryID: &manualID,
		Fields:           subscriptionaccounts.ManualFields{Email: stringPtr("other@example.com")},
		NowMS:            22,
	})
	if err != nil || reuse.Result != string(subscriptionaccounts.MutationConflict) ||
		reuse.Reason == nil || *reuse.Reason != subscriptionaccounts.ReasonRequestIDReused {
		t.Fatalf("first edit reuse = %#v %v", reuse, err)
	}
	records, err = repository.ListCodexSubscriptionRecords(t.Context())
	if err != nil || len(records.Manual) != 1 || len(records.Links) != 1 {
		t.Fatalf("after first edit = %#v %v", records, err)
	}
	if records.Links[0].ManualEntryID != manualID || records.Manual[0].Email == nil || *records.Manual[0].Email != "manual@example.com" {
		t.Fatalf("supplement = %#v", records)
	}
}

func TestCodexSubscriptionLinkUnlinkCAS(t *testing.T) {
	t.Parallel()
	repository := openAccountBindingRepository(t)
	scopeA, _ := confirmTwoScopes(t, repository)
	records, err := repository.ListCodexSubscriptionRecords(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var detectedID string
	for _, detected := range records.Detected {
		if detected.AccountScope == scopeA {
			detectedID = detected.DetectedAccountID
		}
	}
	manualID := uuid.NewString()
	if _, err := repository.CreateCodexSubscriptionManualEntry(t.Context(), standaloneCreate(manualID, "user@example.com")); err != nil {
		t.Fatal(err)
	}
	applied, err := repository.LinkCodexSubscriptionAccount(t.Context(), CodexSubscriptionLinkRequest{
		DetectedAccountID: detectedID, ManualEntryID: manualID, ExpectedManualRevision: 1, NowMS: 10,
	})
	if err != nil || applied.Result != string(subscriptionaccounts.MutationApplied) {
		t.Fatalf("link = %#v %v", applied, err)
	}
	replay, err := repository.LinkCodexSubscriptionAccount(t.Context(), CodexSubscriptionLinkRequest{
		DetectedAccountID: detectedID, ManualEntryID: manualID, ExpectedManualRevision: 1, NowMS: 11,
	})
	if err != nil || replay.Result != string(subscriptionaccounts.MutationNoop) {
		t.Fatalf("link replay = %#v %v", replay, err)
	}
	other := uuid.NewString()
	if _, err := repository.CreateCodexSubscriptionManualEntry(t.Context(), standaloneCreate(other, "other@example.com")); err != nil {
		t.Fatal(err)
	}
	conflict, err := repository.LinkCodexSubscriptionAccount(t.Context(), CodexSubscriptionLinkRequest{
		DetectedAccountID: detectedID, ManualEntryID: other, ExpectedManualRevision: 1, NowMS: 12,
	})
	if err != nil || conflict.Result != string(subscriptionaccounts.MutationConflict) ||
		conflict.Reason == nil || *conflict.Reason != subscriptionaccounts.ReasonAlreadyLinked {
		t.Fatalf("second link = %#v %v", conflict, err)
	}
	unlink, err := repository.UnlinkCodexSubscriptionAccount(t.Context(), CodexSubscriptionUnlinkRequest{
		DetectedAccountID: detectedID, ManualEntryID: manualID,
		ExpectedManualRevision: 1, ExpectedLinkRevision: 1, NowMS: 13,
	})
	if err != nil || unlink.Result != string(subscriptionaccounts.MutationApplied) {
		t.Fatalf("unlink = %#v %v", unlink, err)
	}
	unlinkReplay, err := repository.UnlinkCodexSubscriptionAccount(t.Context(), CodexSubscriptionUnlinkRequest{
		DetectedAccountID: detectedID, ManualEntryID: manualID,
		ExpectedManualRevision: 1, ExpectedLinkRevision: 1, NowMS: 14,
	})
	if err != nil || unlinkReplay.Result != string(subscriptionaccounts.MutationNoop) {
		t.Fatalf("unlink replay = %#v %v", unlinkReplay, err)
	}
}

func TestCodexSubscriptionDeleteLinkedNonCurrentAccountRemovesVisibleComposite(t *testing.T) {
	t.Parallel()
	repository := openAccountBindingRepository(t)
	scopeA, _ := confirmTwoScopes(t, repository)
	records, err := repository.ListCodexSubscriptionRecords(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var detectedID string
	var detectedRevision int64
	for _, detected := range records.Detected {
		if detected.AccountScope == scopeA {
			detectedID = detected.DetectedAccountID
			detectedRevision = detected.Revision
		}
	}
	manualID := uuid.NewString()
	if _, err := repository.CreateCodexSubscriptionManualEntry(
		t.Context(), standaloneCreate(manualID, "old@example.com"),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.LinkCodexSubscriptionAccount(t.Context(), CodexSubscriptionLinkRequest{
		DetectedAccountID: detectedID, ManualEntryID: manualID,
		ExpectedManualRevision: 1, NowMS: 10,
	}); err != nil {
		t.Fatal(err)
	}
	manualRevision, linkRevision := int64(1), int64(1)
	request := CodexSubscriptionDeleteRequest{
		AccountID:                detectedID,
		ExpectedDetectedRevision: &detectedRevision,
		ExpectedManualRevision:   &manualRevision,
		ExpectedLinkRevision:     &linkRevision,
	}
	mutation, err := repository.DeleteCodexSubscriptionAccount(t.Context(), request)
	if err != nil || mutation.Result != string(subscriptionaccounts.MutationApplied) {
		t.Fatalf("delete linked account = %#v %v", mutation, err)
	}
	after, err := repository.ListCodexSubscriptionRecords(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Detected) != 1 || len(after.Manual) != 0 || len(after.Links) != 0 {
		t.Fatalf("delete left composite rows = %#v", after)
	}
	if count := scalarCount(t, repository.database, `SELECT COUNT(*) FROM codex_account_scopes`); count != 2 {
		t.Fatalf("delete removed account identity scopes = %d", count)
	}
	replay, err := repository.DeleteCodexSubscriptionAccount(t.Context(), request)
	if err != nil || replay.Result != string(subscriptionaccounts.MutationNoop) {
		t.Fatalf("delete replay = %#v %v", replay, err)
	}
}

func TestCodexSubscriptionDeleteRejectsCurrentAccount(t *testing.T) {
	t.Parallel()
	repository := openAccountBindingRepository(t)
	_, scopeB := confirmTwoScopes(t, repository)
	records, err := repository.ListCodexSubscriptionRecords(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var currentID string
	var revision int64
	for _, detected := range records.Detected {
		if detected.AccountScope == scopeB {
			currentID = detected.DetectedAccountID
			revision = detected.Revision
		}
	}
	mutation, err := repository.DeleteCodexSubscriptionAccount(t.Context(), CodexSubscriptionDeleteRequest{
		AccountID: currentID, ExpectedDetectedRevision: &revision,
	})
	if err != nil || mutation.Result != string(subscriptionaccounts.MutationConflict) ||
		mutation.Reason == nil || *mutation.Reason != subscriptionaccounts.ReasonCurrentAccount {
		t.Fatalf("delete current account = %#v %v", mutation, err)
	}
	if count := scalarCount(t, repository.database, `SELECT COUNT(*) FROM codex_subscription_detected_accounts`); count != 2 {
		t.Fatalf("current account was deleted, rows = %d", count)
	}
}

func TestCodexSubscriptionDeleteStandaloneManualAccount(t *testing.T) {
	t.Parallel()
	repository := openAccountBindingRepository(t)
	manualID := uuid.NewString()
	if _, err := repository.CreateCodexSubscriptionManualEntry(
		t.Context(), standaloneCreate(manualID, "manual@example.com"),
	); err != nil {
		t.Fatal(err)
	}
	revision := int64(1)
	mutation, err := repository.DeleteCodexSubscriptionAccount(t.Context(), CodexSubscriptionDeleteRequest{
		AccountID: manualID, ExpectedManualRevision: &revision,
	})
	if err != nil || mutation.Result != string(subscriptionaccounts.MutationApplied) {
		t.Fatalf("delete standalone manual = %#v %v", mutation, err)
	}
	if count := scalarCount(t, repository.database, `SELECT COUNT(*) FROM codex_subscription_manual_entries`); count != 0 {
		t.Fatalf("standalone manual rows = %d", count)
	}
}

func TestCodexSubscriptionDeleteStaleLinkedRevisionDoesNotPartialWrite(t *testing.T) {
	t.Parallel()
	repository := openAccountBindingRepository(t)
	scopeA, _ := confirmTwoScopes(t, repository)
	records, err := repository.ListCodexSubscriptionRecords(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var detectedID string
	var detectedRevision int64
	for _, detected := range records.Detected {
		if detected.AccountScope == scopeA {
			detectedID = detected.DetectedAccountID
			detectedRevision = detected.Revision
		}
	}
	manualID := uuid.NewString()
	if _, err := repository.CreateCodexSubscriptionManualEntry(
		t.Context(), standaloneCreate(manualID, "stale@example.com"),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.LinkCodexSubscriptionAccount(t.Context(), CodexSubscriptionLinkRequest{
		DetectedAccountID: detectedID, ManualEntryID: manualID,
		ExpectedManualRevision: 1, NowMS: 10,
	}); err != nil {
		t.Fatal(err)
	}
	manualRevision, staleLinkRevision := int64(1), int64(99)
	mutation, err := repository.DeleteCodexSubscriptionAccount(t.Context(), CodexSubscriptionDeleteRequest{
		AccountID:                detectedID,
		ExpectedDetectedRevision: &detectedRevision,
		ExpectedManualRevision:   &manualRevision,
		ExpectedLinkRevision:     &staleLinkRevision,
	})
	if err != nil || mutation.Result != string(subscriptionaccounts.MutationConflict) ||
		mutation.Reason == nil || *mutation.Reason != subscriptionaccounts.ReasonRevisionChanged {
		t.Fatalf("stale delete = %#v %v", mutation, err)
	}
	if detected := scalarCount(t, repository.database, `SELECT COUNT(*) FROM codex_subscription_detected_accounts`); detected != 2 {
		t.Fatalf("stale delete changed detected rows = %d", detected)
	}
	if manual := scalarCount(t, repository.database, `SELECT COUNT(*) FROM codex_subscription_manual_entries`); manual != 1 {
		t.Fatalf("stale delete changed manual rows = %d", manual)
	}
	if links := scalarCount(t, repository.database, `SELECT COUNT(*) FROM codex_subscription_links`); links != 1 {
		t.Fatalf("stale delete changed link rows = %d", links)
	}
}

func TestCodexSubscriptionDeleteRejectsLinkRemovedAfterList(t *testing.T) {
	t.Parallel()
	repository := openAccountBindingRepository(t)
	scopeA, _ := confirmTwoScopes(t, repository)
	records, err := repository.ListCodexSubscriptionRecords(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var detectedID string
	var detectedRevision int64
	for _, detected := range records.Detected {
		if detected.AccountScope == scopeA {
			detectedID = detected.DetectedAccountID
			detectedRevision = detected.Revision
		}
	}
	manualID := uuid.NewString()
	if _, err := repository.CreateCodexSubscriptionManualEntry(
		t.Context(), standaloneCreate(manualID, "changed@example.com"),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.LinkCodexSubscriptionAccount(t.Context(), CodexSubscriptionLinkRequest{
		DetectedAccountID: detectedID, ManualEntryID: manualID,
		ExpectedManualRevision: 1, NowMS: 10,
	}); err != nil {
		t.Fatal(err)
	}
	manualRevision, linkRevision := int64(1), int64(1)
	staleRequest := CodexSubscriptionDeleteRequest{
		AccountID:                detectedID,
		ExpectedDetectedRevision: &detectedRevision,
		ExpectedManualRevision:   &manualRevision,
		ExpectedLinkRevision:     &linkRevision,
	}
	if mutation, err := repository.UnlinkCodexSubscriptionAccount(t.Context(), CodexSubscriptionUnlinkRequest{
		DetectedAccountID: detectedID, ManualEntryID: manualID,
		ExpectedManualRevision: manualRevision, ExpectedLinkRevision: linkRevision, NowMS: 11,
	}); err != nil || mutation.Result != string(subscriptionaccounts.MutationApplied) {
		t.Fatalf("unlink before delete = %#v %v", mutation, err)
	}
	mutation, err := repository.DeleteCodexSubscriptionAccount(t.Context(), staleRequest)
	if err != nil || mutation.Result != string(subscriptionaccounts.MutationConflict) ||
		mutation.Reason == nil || *mutation.Reason != subscriptionaccounts.ReasonLinkTargetChanged {
		t.Fatalf("delete after link changed = %#v %v", mutation, err)
	}
	if detected := scalarCount(t, repository.database, `SELECT COUNT(*) FROM codex_subscription_detected_accounts`); detected != 2 {
		t.Fatalf("link-changed delete changed detected rows = %d", detected)
	}
	if manual := scalarCount(t, repository.database, `SELECT COUNT(*) FROM codex_subscription_manual_entries`); manual != 1 {
		t.Fatalf("link-changed delete changed manual rows = %d", manual)
	}
}

func TestCodexSubscriptionEmptyDetectedEmailKeepsPrevious(t *testing.T) {
	t.Parallel()
	repository := openAccountBindingRepository(t)
	key := ensureTestScopeKey(t, repository)
	scopeA, err := accountbinding.DeriveScope(key, []byte("acct-test-a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.ConfirmCodexAccountBinding(t.Context(), scopeA, 1, CodexAccountBindingReasonStartup); err != nil {
		t.Fatal(err)
	}
	first, _, err := repository.RecordDetectedCodexSubscriptionProfile(
		t.Context(), fenceFor(t, repository, scopeA), detectedEmailProfile("keep@example.com", subscriptionaccounts.PlanPlus),
	)
	if err != nil || first.DetectedEmail == nil || *first.DetectedEmail != "keep@example.com" {
		t.Fatalf("first profile = %#v %v", first, err)
	}
	second, _, err := repository.RecordDetectedCodexSubscriptionProfile(
		t.Context(), fenceFor(t, repository, scopeA), DetectedCodexSubscriptionProfile{
			Automatic:    subscriptionaccounts.AutomaticPlanFact{State: subscriptionaccounts.AutomaticPlanUnknown},
			ObservedAtMS: 50,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if second.DetectedEmail == nil || *second.DetectedEmail != "keep@example.com" {
		t.Fatalf("empty email overwrote previous: %#v", second)
	}
	if second.AutomaticPlanState != subscriptionaccounts.AutomaticPlanUnknown || second.AutomaticPlan != nil {
		t.Fatalf("automatic plan was not replaced: %#v", second)
	}
}

func TestCodexSubscriptionProfileReportsOnlyMaterialChanges(t *testing.T) {
	t.Parallel()
	repository := openAccountBindingRepository(t)
	key := ensureTestScopeKey(t, repository)
	scope, err := accountbinding.DeriveScope(key, []byte("acct-test-a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.ConfirmCodexAccountBinding(
		t.Context(), scope, 1, CodexAccountBindingReasonStartup,
	); err != nil {
		t.Fatal(err)
	}
	fence := fenceFor(t, repository, scope)
	first, changed, err := repository.RecordDetectedCodexSubscriptionProfile(
		t.Context(), fence, detectedEmailProfile("same@example.com", subscriptionaccounts.PlanPlus),
	)
	if err != nil || !changed {
		t.Fatalf("first profile = %#v changed=%v err=%v", first, changed, err)
	}
	profile := detectedEmailProfile("same@example.com", subscriptionaccounts.PlanPlus)
	profile.ObservedAtMS = 20
	second, changed, err := repository.RecordDetectedCodexSubscriptionProfile(t.Context(), fence, profile)
	if err != nil || changed {
		t.Fatalf("same facts = %#v changed=%v err=%v", second, changed, err)
	}
	if second.Revision <= first.Revision || second.AutomaticPlanObservedAtMS == nil ||
		*second.AutomaticPlanObservedAtMS != 20 {
		t.Fatalf("same facts did not advance observation metadata: first=%#v second=%#v", first, second)
	}
	profile.Automatic.Plan = planPtr(subscriptionaccounts.PlanPro20X)
	third, changed, err := repository.RecordDetectedCodexSubscriptionProfile(t.Context(), fence, profile)
	if err != nil || !changed || third.AutomaticPlan == nil ||
		*third.AutomaticPlan != subscriptionaccounts.PlanPro20X {
		t.Fatalf("changed facts = %#v changed=%v err=%v", third, changed, err)
	}
}

func confirmTwoScopes(t *testing.T, repository *Repository) (string, string) {
	t.Helper()
	key := ensureTestScopeKey(t, repository)
	scopeA, err := accountbinding.DeriveScope(key, []byte("acct-test-a"))
	if err != nil {
		t.Fatal(err)
	}
	scopeB, err := accountbinding.DeriveScope(key, []byte("acct-test-b"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.ConfirmCodexAccountBinding(t.Context(), scopeA, 1, CodexAccountBindingReasonStartup); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.MarkCodexAccountBindingPending(t.Context(), 2, CodexAccountBindingReasonAccountChanged); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.ConfirmCodexAccountBinding(t.Context(), scopeB, 3, CodexAccountBindingReasonAccountChanged); err != nil {
		t.Fatal(err)
	}
	return scopeA, scopeB
}

func ensureTestScopeKey(t *testing.T, repository *Repository) [32]byte {
	t.Helper()
	var candidate [32]byte
	copy(candidate[:], bytes.Repeat([]byte{0x21}, 32))
	key, err := repository.EnsureCodexAccountScopeKey(t.Context(), candidate, 1)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func fenceFor(t *testing.T, repository *Repository, scope string) CodexAccountFence {
	t.Helper()
	binding, err := repository.CodexAccountBinding(t.Context())
	if err != nil || binding.AccountScope == nil || *binding.AccountScope != scope {
		t.Fatalf("current binding is not %s: %#v %v", scope, binding, err)
	}
	return CodexAccountFence{AccountScope: scope, BindingGeneration: binding.BindingGeneration}
}

func detectedEmailProfile(email string, plan subscriptionaccounts.Plan) DetectedCodexSubscriptionProfile {
	return DetectedCodexSubscriptionProfile{
		Email: &email,
		Automatic: subscriptionaccounts.AutomaticPlanFact{
			State: subscriptionaccounts.AutomaticPlanKnown,
			Plan:  &plan,
		},
		ObservedAtMS: 10,
	}
}

func standaloneCreate(id, email string) CodexSubscriptionManualCreate {
	return CodexSubscriptionManualCreate{
		ManualEntryID: id,
		Fields:        subscriptionaccounts.ManualFields{Email: &email},
		NowMS:         1,
	}
}

func stringPtr(value string) *string {
	return &value
}

func planPtr(value subscriptionaccounts.Plan) *subscriptionaccounts.Plan {
	return &value
}
