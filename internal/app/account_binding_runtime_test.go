package app

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/accountbinding"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptionaccounts"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func TestAccountBindingSealsAndDrainsABeforePublishingB(t *testing.T) {
	t.Parallel()

	repository := openAccountBindingTestRepository(t)
	key, scopeA, scopeB := accountBindingTestScopes(t, repository)
	if _, _, err := repository.ConfirmCodexAccountBinding(
		context.Background(), scopeA, quotaRuntimeNowMS, store.CodexAccountBindingReasonStartup,
	); err != nil {
		t.Fatalf("Confirm(A) error = %v", err)
	}

	aStarted := make(chan struct{})
	aCanceled := make(chan struct{})
	releaseDrain := make(chan struct{})
	published := make(chan quotaonline.AccountBindingFence, 1)
	aContext, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	controller := &accountBindingTestQuota{
		onSeal: func(context.Context) error {
			cancelA()
			<-releaseDrain
			return nil
		},
		onPublish: func(_ context.Context, fence quotaonline.AccountBindingFence) error {
			published <- fence
			return nil
		},
	}
	reader := &accountBindingScriptedReader{accountIDs: []string{"acct-test-b", "acct-test-b"}}
	runtime, err := newAccountBindingRuntime(
		repository, reader, key,
		func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		controller, nil, nil,
	)
	if err != nil {
		t.Fatalf("newAccountBindingRuntime() error = %v", err)
	}
	runtime.setActive(scopeA, 1)

	go func() {
		close(aStarted)
		<-aContext.Done()
		close(aCanceled)
	}()
	<-aStarted

	done := make(chan error, 1)
	go func() {
		done <- runtime.HandleObservedScopeChange(context.Background())
	}()
	select {
	case <-aCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("A context was not canceled before B publish")
	}
	select {
	case <-published:
		t.Fatal("published B before A drained")
	default:
	}
	close(releaseDrain)
	if err := <-done; err != nil {
		t.Fatalf("HandleObservedScopeChange() error = %v", err)
	}
	fence := <-published
	if fence.AccountScope != scopeB || fence.BindingGeneration == 1 {
		t.Fatalf("published fence = %#v", fence)
	}
	active := runtime.Active()
	if active == nil || active.Scope != scopeB || active.Generation != fence.BindingGeneration {
		t.Fatalf("active = %#v", active)
	}
	if runtime.Active().Generation <= 1 {
		t.Fatal("B reused A's generation")
	}
}

func TestAccountBindingLateAPublisherDoesNotReplaceB(t *testing.T) {
	t.Parallel()

	repository := openAccountBindingTestRepository(t)
	key, scopeA, scopeB := accountBindingTestScopes(t, repository)
	runtime := mustAccountBindingRuntime(t, repository, key, &accountBindingScriptedReader{
		accountIDs: []string{"acct-test-a", "acct-test-b", "acct-test-b"},
	}, &accountBindingTestQuota{})
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start(A) error = %v", err)
	}
	if err := runtime.HandleObservedScopeChange(context.Background()); err != nil {
		t.Fatalf("Handle(B) error = %v", err)
	}
	activeB := runtime.Active()
	if activeB == nil || activeB.Scope != scopeB {
		t.Fatalf("active after B = %#v", activeB)
	}
	if err := runtime.quota.PublishBinding(context.Background(), quotaonline.AccountBindingFence{
		AccountScope: scopeA, BindingGeneration: 1,
	}); err != nil {
		t.Fatalf("late PublishBinding(A) error = %v", err)
	}
	active := runtime.Active()
	if active == nil || active.Scope != scopeB || active.Generation != activeB.Generation {
		t.Fatalf("late A replaced B: %#v", active)
	}
	current, err := repository.CodexAccountBinding(context.Background())
	if err != nil || current.AccountScope == nil || *current.AccountScope != scopeB {
		t.Fatalf("stored binding = %#v, %v", current, err)
	}
}

func TestAccountBindingFailedSecondConfirmStaysPending(t *testing.T) {
	t.Parallel()

	repository := openAccountBindingTestRepository(t)
	key, scopeA, _ := accountBindingTestScopes(t, repository)
	if _, _, err := repository.ConfirmCodexAccountBinding(
		context.Background(), scopeA, quotaRuntimeNowMS, store.CodexAccountBindingReasonStartup,
	); err != nil {
		t.Fatalf("Confirm(A) error = %v", err)
	}
	reader := &accountBindingScriptedReader{
		accountIDs: []string{"acct-test-b"},
		err:        errors.New("synthetic confirmation failed"),
		failOn:     2,
	}
	runtime := mustAccountBindingRuntime(t, repository, key, reader, &accountBindingTestQuota{})
	if err := runtime.HandleObservedScopeChange(context.Background()); err == nil {
		t.Fatal("HandleObservedScopeChange() error = nil, want confirmation failure")
	}
	if runtime.Active() != nil {
		t.Fatalf("active after failed confirm = %#v", runtime.Active())
	}
	current, err := repository.CodexAccountBinding(context.Background())
	if err != nil || current.State != store.CodexAccountBindingPending || current.AccountScope != nil {
		t.Fatalf("binding after failed confirm = %#v, %v", current, err)
	}
}

func TestAccountBindingIdentityUnavailableSealsConfirmedGeneration(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repository := openAccountBindingTestRepository(t)
	key, _, _ := accountBindingTestScopes(t, repository)
	var sealCalls atomic.Int64
	runtime := mustAccountBindingRuntime(t, repository, key, &accountBindingScriptedReader{
		accountIDs: []string{"acct-test-a", ""},
	}, &accountBindingTestQuota{
		onSeal: func(context.Context) error {
			sealCalls.Add(1)
			return nil
		},
	})
	if err := runtime.Start(ctx); err != nil {
		t.Fatalf("Start(A) error = %v", err)
	}
	if err := runtime.Discover(ctx, store.CodexAccountBindingReasonStable); err != nil {
		t.Fatalf("Discover(identity unavailable) error = %v", err)
	}
	if sealCalls.Load() != 1 {
		t.Fatalf("SealAndDrain() calls = %d, want 1", sealCalls.Load())
	}
	if runtime.Active() != nil {
		t.Fatalf("active after identity unavailable = %#v", runtime.Active())
	}
	binding, err := repository.CodexAccountBinding(ctx)
	if err != nil || binding.State != store.CodexAccountBindingIdentityUnavailable ||
		binding.AccountScope != nil {
		t.Fatalf("binding after identity unavailable = %#v, %v", binding, err)
	}
}

func TestAccountBindingSuccessfulBStartsOnlyBGeneration(t *testing.T) {
	t.Parallel()

	repository := openAccountBindingTestRepository(t)
	key, scopeA, scopeB := accountBindingTestScopes(t, repository)
	published := make([]quotaonline.AccountBindingFence, 0, 2)
	runtime := mustAccountBindingRuntime(t, repository, key, &accountBindingScriptedReader{
		accountIDs: []string{"acct-test-a", "acct-test-b", "acct-test-b"},
	}, &accountBindingTestQuota{
		onPublish: func(_ context.Context, fence quotaonline.AccountBindingFence) error {
			published = append(published, fence)
			return nil
		},
	})
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start(A) error = %v", err)
	}
	if err := runtime.HandleObservedScopeChange(context.Background()); err != nil {
		t.Fatalf("Handle(B) error = %v", err)
	}
	if len(published) != 2 || published[0].AccountScope != scopeA || published[1].AccountScope != scopeB ||
		published[1].BindingGeneration <= published[0].BindingGeneration {
		t.Fatalf("published = %#v", published)
	}
	if runtime.Active() == nil || runtime.Active().Scope != scopeB {
		t.Fatalf("active = %#v", runtime.Active())
	}
}

func TestAccountBindingABARestoresScopeWithNewGeneration(t *testing.T) {
	t.Parallel()

	repository := openAccountBindingTestRepository(t)
	key, scopeA, scopeB := accountBindingTestScopes(t, repository)
	runtime := mustAccountBindingRuntime(t, repository, key, &accountBindingScriptedReader{
		accountIDs: []string{
			"acct-test-a",
			"acct-test-b", "acct-test-b",
			"acct-test-a", "acct-test-a",
		},
	}, &accountBindingTestQuota{})
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start(A) error = %v", err)
	}
	first := runtime.Active()
	if first == nil || first.Scope != scopeA {
		t.Fatalf("first A = %#v", first)
	}
	if err := runtime.HandleObservedScopeChange(context.Background()); err != nil {
		t.Fatalf("Handle(B) error = %v", err)
	}
	if runtime.Active() == nil || runtime.Active().Scope != scopeB {
		t.Fatalf("B = %#v", runtime.Active())
	}
	if err := runtime.HandleObservedScopeChange(context.Background()); err != nil {
		t.Fatalf("Handle(A restore) error = %v", err)
	}
	restored := runtime.Active()
	if restored == nil || restored.Scope != scopeA || restored.Generation <= first.Generation {
		t.Fatalf("restored A = %#v first=%#v", restored, first)
	}
}

func TestAccountBindingNotifiesAccountThenQuotaOnSwitch(t *testing.T) {
	t.Parallel()

	repository := openAccountBindingTestRepository(t)
	key, _, scopeB := accountBindingTestScopes(t, repository)
	invalidation := &recordingQueryInvalidationNotifier{}
	runtime, err := newAccountBindingRuntime(
		repository,
		&accountBindingScriptedReader{accountIDs: []string{"acct-test-a", "acct-test-b", "acct-test-b"}},
		key,
		func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		&accountBindingTestQuota{},
		invalidation,
		nil,
	)
	if err != nil {
		t.Fatalf("newAccountBindingRuntime() error = %v", err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start(A) error = %v", err)
	}
	invalidation.reset()
	if err := runtime.HandleObservedScopeChange(context.Background()); err != nil {
		t.Fatalf("Handle(B) error = %v", err)
	}
	got := invalidation.snapshot()
	if len(got) < 2 || len(got)%2 != 0 {
		t.Fatalf("invalidations = %#v, want account then quota pairs", got)
	}
	for index := 0; index < len(got); index += 2 {
		if got[index] != core.InvalidationAccount || got[index+1] != core.InvalidationQuota {
			t.Fatalf("invalidation order = %#v, want account then quota", got)
		}
	}
	if runtime.Active() == nil || runtime.Active().Scope != scopeB {
		t.Fatalf("active after B = %#v", runtime.Active())
	}
}

func TestAccountBindingSameScopeProbeDoesNotInvalidateAccount(t *testing.T) {
	t.Parallel()

	repository := openAccountBindingTestRepository(t)
	key, scopeA, _ := accountBindingTestScopes(t, repository)
	invalidation := &recordingQueryInvalidationNotifier{}
	runtime, err := newAccountBindingRuntime(
		repository,
		&accountBindingScriptedReader{accountIDs: []string{"acct-test-a"}},
		key,
		func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		&accountBindingTestQuota{},
		invalidation,
		nil,
	)
	if err != nil {
		t.Fatalf("newAccountBindingRuntime() error = %v", err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start(A) error = %v", err)
	}
	if runtime.Active() == nil || runtime.Active().Scope != scopeA {
		t.Fatalf("active after Start = %#v", runtime.Active())
	}
	invalidation.reset()
	if err := runtime.Discover(context.Background(), store.CodexAccountBindingReasonStable); err != nil {
		t.Fatalf("Discover(same scope) error = %v", err)
	}
	if got := invalidation.snapshot(); len(got) != 0 {
		t.Fatalf("same-scope probe invalidations = %#v, want none", got)
	}
}

func TestAccountBindingHomeFenceRunsBeforeAccountWithoutDeadlock(t *testing.T) {
	t.Parallel()

	var home sync.Mutex
	home.Lock()
	repository := openAccountBindingTestRepository(t)
	key, _, _ := accountBindingTestScopes(t, repository)
	runtime := mustAccountBindingRuntime(t, repository, key, &accountBindingScriptedReader{
		accountIDs: []string{"acct-test-a"},
	}, &accountBindingTestQuota{})
	done := make(chan error, 1)
	go func() {
		done <- runtime.Start(context.Background())
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start() while Home lock held error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("account transition deadlocked behind Home lock")
	}
	home.Unlock()
}

func TestAccountBindingProbeReadErrorKeepsConfirmed(t *testing.T) {
	t.Parallel()

	repository := openAccountBindingTestRepository(t)
	key, scopeA, _ := accountBindingTestScopes(t, repository)
	reader := &accountBindingScriptedReader{
		accountIDs: []string{"acct-test-a"},
		err:        errors.New("synthetic identity probe failed"),
		failOn:     2,
	}
	runtime := mustAccountBindingRuntime(t, repository, key, reader, &accountBindingTestQuota{})
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start(A) error = %v", err)
	}
	if err := runtime.Discover(context.Background(), store.CodexAccountBindingReasonStable); err == nil {
		t.Fatal("Discover() error = nil, want probe failure")
	}
	active := runtime.Active()
	if active == nil || active.Scope != scopeA {
		t.Fatalf("active after probe failure = %#v", active)
	}
	current, err := repository.CodexAccountBinding(context.Background())
	if err != nil || current.State != store.CodexAccountBindingConfirmed ||
		current.AccountScope == nil || *current.AccountScope != scopeA {
		t.Fatalf("binding after probe failure = %#v, %v", current, err)
	}
}

func TestAccountBindingCallerCancellationDoesNotPersistConfirmationFailure(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		failOn int
		err    error
	}{
		{name: "first read canceled", failOn: 1, err: context.Canceled},
		{name: "confirmation read canceled", failOn: 2, err: context.Canceled},
		{name: "first read deadline", failOn: 1, err: context.DeadlineExceeded},
		{name: "confirmation read deadline", failOn: 2, err: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			repository := openAccountBindingTestRepository(t)
			key, _, _ := accountBindingTestScopes(t, repository)
			if _, err := repository.MarkCodexAccountBindingPending(
				t.Context(),
				quotaRuntimeNowMS-1,
				store.CodexAccountBindingReasonAccountChanged,
			); err != nil {
				t.Fatalf("MarkCodexAccountBindingPending() error = %v", err)
			}
			runtime := mustAccountBindingRuntime(
				t,
				repository,
				key,
				&accountBindingScriptedReader{
					accountIDs: []string{"acct-test-a"},
					err:        test.err,
					failOn:     test.failOn,
				},
				&accountBindingTestQuota{},
			)

			err := runtime.Discover(t.Context(), store.CodexAccountBindingReasonStable)
			if !errors.Is(err, test.err) {
				t.Fatalf("Discover() error = %v, want %v", err, test.err)
			}
			binding, err := repository.CodexAccountBinding(t.Context())
			if err != nil {
				t.Fatalf("CodexAccountBinding() error = %v", err)
			}
			if binding.State != store.CodexAccountBindingPending ||
				binding.Reason != store.CodexAccountBindingReasonAccountChanged {
				t.Fatalf(
					"binding after caller cancellation = %#v, want pending/account_changed",
					binding,
				)
			}
		})
	}
}

func TestAccountBindingLoadDisplayRequiresMatchingSandwich(t *testing.T) {
	t.Parallel()

	repository := openAccountBindingTestRepository(t)
	key, scopeA, _ := accountBindingTestScopes(t, repository)
	email := "person@example.com"
	plan := "prolite"
	runtime, err := newAccountBindingRuntime(
		repository,
		&accountBindingScriptedReader{accountIDs: []string{"acct-test-a"}},
		key,
		func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		&accountBindingTestQuota{},
		nil,
		func(context.Context) (appserver.AccountSandwich, error) {
			return appserver.AccountSandwich{
				BeforeID:                 appserver.SensitiveAccountID("acct-test-a"),
				AfterID:                  appserver.SensitiveAccountID("acct-test-a"),
				BeforeRateLimitPlanTypes: []string{"prolite"},
				AfterRateLimitPlanTypes:  []string{"prolite"},
				Account:                  &appserver.AccountSnapshot{Type: "chatgpt", Email: &email, PlanType: &plan},
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("newAccountBindingRuntime() error = %v", err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start(A) error = %v", err)
	}
	display, err := runtime.LoadDisplay(context.Background())
	if err != nil || display == nil || display.Email == nil || *display.Email != email ||
		display.PlanType == nil || *display.PlanType != plan ||
		len(display.BeforeRateLimitPlanTypes) != 1 || display.BeforeRateLimitPlanTypes[0] != "prolite" ||
		len(display.AfterRateLimitPlanTypes) != 1 || display.AfterRateLimitPlanTypes[0] != "prolite" {
		t.Fatalf("LoadDisplay() = %#v, %v", display, err)
	}
	cached, err := runtime.LoadDisplay(context.Background())
	if err != nil || cached == nil || cached.Email == nil || *cached.Email != email {
		t.Fatalf("cached LoadDisplay() = %#v, %v", cached, err)
	}

	mismatch, err := newAccountBindingRuntime(
		repository,
		&accountBindingScriptedReader{accountIDs: []string{"acct-test-a"}},
		key,
		func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		&accountBindingTestQuota{},
		nil,
		func(context.Context) (appserver.AccountSandwich, error) {
			return appserver.AccountSandwich{
				BeforeID: appserver.SensitiveAccountID("acct-test-a"),
				AfterID:  appserver.SensitiveAccountID("acct-test-b"),
				Account:  &appserver.AccountSnapshot{Type: "chatgpt", Email: &email, PlanType: &plan},
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("mismatch runtime error = %v", err)
	}
	mismatch.setActive(scopeA, runtime.Active().Generation)
	empty, err := mismatch.LoadDisplay(context.Background())
	if err != nil || empty != nil {
		t.Fatalf("mismatched sandwich LoadDisplay() = %#v, %v", empty, err)
	}
}

func TestAccountBindingSameProfileRefreshDoesNotReinvalidateAccount(t *testing.T) {
	t.Parallel()
	repository := openAccountBindingTestRepository(t)
	key, _, _ := accountBindingTestScopes(t, repository)
	email := "person@example.com"
	plan := "plus"
	nowMS := int64(quotaRuntimeNowMS)
	invalidation := &recordingQueryInvalidationNotifier{}
	runtime, err := newAccountBindingRuntime(
		repository,
		&accountBindingScriptedReader{accountIDs: []string{"acct-test-a", "acct-test-a"}},
		key,
		func() time.Time { return time.UnixMilli(nowMS).UTC() },
		&accountBindingTestQuota{},
		invalidation,
		func(context.Context) (appserver.AccountSandwich, error) {
			return appserver.AccountSandwich{
				BeforeID:                 appserver.SensitiveAccountID("acct-test-a"),
				AfterID:                  appserver.SensitiveAccountID("acct-test-a"),
				BeforeRateLimitPlanTypes: []string{plan},
				AfterRateLimitPlanTypes:  []string{plan},
				Account: &appserver.AccountSnapshot{
					Type: "chatgpt", Email: &email, PlanType: &plan,
				},
			}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	invalidation.reset()
	if _, err := runtime.LoadDisplay(t.Context()); err != nil {
		t.Fatal(err)
	}
	if invalidation.count(core.InvalidationAccount) != 1 {
		t.Fatalf("first profile invalidations = %#v", invalidation.snapshot())
	}
	invalidation.reset()
	nowMS++
	if err := runtime.Discover(t.Context(), store.CodexAccountBindingReasonStable); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.LoadDisplay(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := invalidation.snapshot(); len(got) != 0 {
		t.Fatalf("same profile re-invalidated account: %#v", got)
	}
}

func TestAccountBindingLateDisplayDoesNotWriteAfterSwitch(t *testing.T) {
	t.Parallel()
	repository := openAccountBindingTestRepository(t)
	key, scopeA, scopeB := accountBindingTestScopes(t, repository)
	emailA := "a@example.com"
	emailB := "b@example.com"
	plan := "plus"
	currentID := "acct-test-a"
	runtime, err := newAccountBindingRuntime(
		repository,
		&accountBindingScriptedReader{accountIDs: []string{"acct-test-a", "acct-test-b", "acct-test-b"}},
		key,
		func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		&accountBindingTestQuota{},
		nil,
		func(context.Context) (appserver.AccountSandwich, error) {
			return appserver.AccountSandwich{
				BeforeID:                 appserver.SensitiveAccountID(currentID),
				AfterID:                  appserver.SensitiveAccountID(currentID),
				BeforeRateLimitPlanTypes: []string{plan},
				AfterRateLimitPlanTypes:  []string{plan},
				Account:                  &appserver.AccountSnapshot{Type: "chatgpt", Email: &emailA, PlanType: &plan},
			}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.LoadDisplay(context.Background()); err != nil {
		t.Fatal(err)
	}
	generationA := runtime.Active().Generation
	currentID = "acct-test-b"
	runtime.sandwich = func(context.Context) (appserver.AccountSandwich, error) {
		return appserver.AccountSandwich{
			BeforeID:                 appserver.SensitiveAccountID("acct-test-b"),
			AfterID:                  appserver.SensitiveAccountID("acct-test-b"),
			BeforeRateLimitPlanTypes: []string{plan},
			AfterRateLimitPlanTypes:  []string{plan},
			Account:                  &appserver.AccountSnapshot{Type: "chatgpt", Email: &emailB, PlanType: &plan},
		}, nil
	}
	if err := runtime.HandleObservedScopeChange(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.LoadDisplay(context.Background()); err != nil {
		t.Fatal(err)
	}
	runtime.setActive(scopeA, generationA)
	runtime.sandwich = func(context.Context) (appserver.AccountSandwich, error) {
		return appserver.AccountSandwich{
			BeforeID: appserver.SensitiveAccountID("acct-test-a"),
			AfterID:  appserver.SensitiveAccountID("acct-test-a"),
			Account:  &appserver.AccountSnapshot{Type: "chatgpt", Email: &emailA, PlanType: &plan},
		}, nil
	}
	late, err := runtime.LoadDisplay(context.Background())
	if err != nil || late != nil {
		t.Fatalf("late A display = %#v %v", late, err)
	}
	records, err := repository.ListCodexSubscriptionRecords(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var sawA, sawB bool
	for _, detected := range records.Detected {
		switch detected.AccountScope {
		case scopeA:
			sawA = true
			if detected.DetectedEmail == nil || *detected.DetectedEmail != emailA {
				t.Fatalf("A profile lost: %#v", detected)
			}
		case scopeB:
			sawB = true
			if detected.DetectedEmail == nil || *detected.DetectedEmail != emailB {
				t.Fatalf("B profile polluted: %#v", detected)
			}
		}
	}
	if !sawA || !sawB {
		t.Fatalf("missing detected rows: %#v", records.Detected)
	}
}

func TestAccountBindingABARestoresDetectedPublicIDWithoutCopyingB(t *testing.T) {
	t.Parallel()
	repository := openAccountBindingTestRepository(t)
	key, _, _ := accountBindingTestScopes(t, repository)
	emailA := "a@example.com"
	emailB := "b@example.com"
	planA := "plus"
	planB := "pro"
	currentID := "acct-test-a"
	runtime, err := newAccountBindingRuntime(
		repository,
		&accountBindingScriptedReader{accountIDs: []string{
			"acct-test-a",
			"acct-test-b", "acct-test-b",
			"acct-test-a", "acct-test-a",
		}},
		key,
		func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		&accountBindingTestQuota{},
		nil,
		func(context.Context) (appserver.AccountSandwich, error) {
			email, plan := emailA, planA
			if currentID == "acct-test-b" {
				email, plan = emailB, planB
			}
			return appserver.AccountSandwich{
				BeforeID:                 appserver.SensitiveAccountID(currentID),
				AfterID:                  appserver.SensitiveAccountID(currentID),
				BeforeRateLimitPlanTypes: []string{plan},
				AfterRateLimitPlanTypes:  []string{plan},
				Account:                  &appserver.AccountSnapshot{Type: "chatgpt", Email: &email, PlanType: &plan},
			}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.LoadDisplay(context.Background()); err != nil {
		t.Fatal(err)
	}
	idA := detectedPublicID(t, repository, runtime.Active().Scope)
	currentID = "acct-test-b"
	if err := runtime.HandleObservedScopeChange(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.LoadDisplay(context.Background()); err != nil {
		t.Fatal(err)
	}
	idB := detectedPublicID(t, repository, runtime.Active().Scope)
	if idA == idB {
		t.Fatal("A and B reused the same public id")
	}
	currentID = "acct-test-a"
	if err := runtime.HandleObservedScopeChange(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.LoadDisplay(context.Background()); err != nil {
		t.Fatal(err)
	}
	restored := detectedPublicID(t, repository, runtime.Active().Scope)
	if restored != idA {
		t.Fatalf("restored public id = %q, want %q", restored, idA)
	}
	records, err := repository.ListCodexSubscriptionRecords(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := subscriptionaccounts.Project(records, quotaRuntimeNowMS, "UTC")
	if err != nil || len(snapshot.Accounts) != 2 {
		t.Fatalf("list = %#v %v", snapshot, err)
	}
	for _, account := range snapshot.Accounts {
		if account.AccountID == idB && account.DetectedEmail != nil && *account.DetectedEmail == emailA {
			t.Fatalf("B inherited A email: %#v", account)
		}
		if account.AccountID == idA && (account.DetectedEmail == nil || *account.DetectedEmail != emailA) {
			t.Fatalf("A profile not restored: %#v", account)
		}
	}
}

func detectedPublicID(t *testing.T, repository *store.Repository, scope string) string {
	t.Helper()
	records, err := repository.ListCodexSubscriptionRecords(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, detected := range records.Detected {
		if detected.AccountScope == scope {
			return detected.DetectedAccountID
		}
	}
	t.Fatalf("no detected row for current scope")
	return ""
}

type accountBindingTestQuota struct {
	onSeal    func(context.Context) error
	onPublish func(context.Context, quotaonline.AccountBindingFence) error
}

func (controller *accountBindingTestQuota) SealAndDrain(ctx context.Context) error {
	if controller != nil && controller.onSeal != nil {
		return controller.onSeal(ctx)
	}
	return nil
}

func (controller *accountBindingTestQuota) PublishBinding(
	ctx context.Context,
	fence quotaonline.AccountBindingFence,
) error {
	if controller != nil && controller.onPublish != nil {
		return controller.onPublish(ctx, fence)
	}
	return nil
}

type accountBindingScriptedReader struct {
	mu         sync.Mutex
	accountIDs []string
	next       int
	err        error
	failOn     int
	calls      atomic.Int64
}

func (reader *accountBindingScriptedReader) Read(
	ctx context.Context,
	excludeResetCreditDetails bool,
) (appserver.AccountRateLimitsSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return appserver.AccountRateLimitsSnapshot{}, err
	}
	call := int(reader.calls.Add(1))
	if reader.failOn != 0 && call == reader.failOn && reader.err != nil {
		return appserver.AccountRateLimitsSnapshot{}, reader.err
	}
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if len(reader.accountIDs) == 0 {
		return appserver.AccountRateLimitsSnapshot{}, errors.New("no synthetic account")
	}
	index := reader.next
	if index >= len(reader.accountIDs) {
		index = len(reader.accountIDs) - 1
	} else {
		reader.next++
	}
	return quotaRuntimeAccountSnapshot(reader.accountIDs[index]), nil
}

func openAccountBindingTestRepository(t testing.TB) *store.Repository {
	t.Helper()
	_, repository := openQuotaRuntimeStore(t)
	return repository
}

func accountBindingTestScopes(
	t testing.TB,
	repository *store.Repository,
) (key [32]byte, scopeA, scopeB string) {
	t.Helper()
	var candidate [32]byte
	copy(candidate[:], bytes.Repeat([]byte{0x42}, 32))
	stored, err := repository.EnsureCodexAccountScopeKey(context.Background(), candidate, quotaRuntimeNowMS)
	if err != nil {
		t.Fatalf("EnsureCodexAccountScopeKey() error = %v", err)
	}
	scopeA, err = accountbinding.DeriveScope(stored, []byte("acct-test-a"))
	if err != nil {
		t.Fatalf("DeriveScope(A) error = %v", err)
	}
	scopeB, err = accountbinding.DeriveScope(stored, []byte("acct-test-b"))
	if err != nil {
		t.Fatalf("DeriveScope(B) error = %v", err)
	}
	return stored, scopeA, scopeB
}

func mustAccountBindingRuntime(
	t testing.TB,
	repository *store.Repository,
	key [32]byte,
	reader quotaonline.AccountRateLimitsReader,
	quota quotaAccountPublisher,
) *accountBindingRuntime {
	t.Helper()
	runtime, err := newAccountBindingRuntime(
		repository, reader, key,
		func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		quota, nil, nil,
	)
	if err != nil {
		t.Fatalf("newAccountBindingRuntime() error = %v", err)
	}
	return runtime
}
