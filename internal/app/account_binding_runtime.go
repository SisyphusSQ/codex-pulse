package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/accountbinding"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

var ErrAccountBindingRuntime = errors.New("account binding runtime is unavailable")

type activeCodexAccount struct {
	Scope      string
	Generation int64
}

type accountDisplayCache struct {
	Scope      string
	Generation int64
	Type       string
	Email      *string
	PlanType   *string
}

type quotaAccountPublisher interface {
	SealAndDrain(context.Context) error
	PublishBinding(context.Context, quotaonline.AccountBindingFence) error
}

type accountBindingRuntime struct {
	repository   *store.Repository
	reader       quotaonline.AccountRateLimitsReader
	scopeKey     [32]byte
	clock        func() time.Time
	quota        quotaAccountPublisher
	invalidation queryInvalidationNotifier
	sandwich     func(context.Context) (appserver.AccountSandwich, error)

	transition chan struct{}

	mu      sync.Mutex
	active  *activeCodexAccount
	display *accountDisplayCache
	closed  bool
}

func newAccountBindingRuntime(
	repository *store.Repository,
	reader quotaonline.AccountRateLimitsReader,
	scopeKey [32]byte,
	clock func() time.Time,
	quota quotaAccountPublisher,
	invalidation queryInvalidationNotifier,
	sandwich func(context.Context) (appserver.AccountSandwich, error),
) (*accountBindingRuntime, error) {
	if repository == nil || reader == nil {
		return nil, ErrAccountBindingRuntime
	}
	if clock == nil {
		clock = time.Now
	}
	runtime := &accountBindingRuntime{
		repository: repository, reader: reader, scopeKey: scopeKey, clock: clock,
		quota: quota, invalidation: invalidation, sandwich: sandwich,
		transition: make(chan struct{}, 1),
	}
	runtime.transition <- struct{}{}
	return runtime, nil
}

func (runtime *accountBindingRuntime) Active() *activeCodexAccount {
	if runtime == nil {
		return nil
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.active == nil {
		return nil
	}
	copy := *runtime.active
	return &copy
}

func (runtime *accountBindingRuntime) Display() *accountDisplayCache {
	if runtime == nil {
		return nil
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.display == nil {
		return nil
	}
	copy := *runtime.display
	if copy.Email != nil {
		email := *copy.Email
		copy.Email = &email
	}
	if copy.PlanType != nil {
		plan := *copy.PlanType
		copy.PlanType = &plan
	}
	return &copy
}

func (runtime *accountBindingRuntime) Close() {
	if runtime == nil {
		return
	}
	runtime.mu.Lock()
	runtime.closed = true
	runtime.mu.Unlock()
}

func (runtime *accountBindingRuntime) Start(ctx context.Context) error {
	return runtime.reconcileIdentity(ctx, store.CodexAccountBindingReasonStartup, identityReconcileStartup)
}

func (runtime *accountBindingRuntime) Discover(ctx context.Context, reason store.CodexAccountBindingReason) error {
	return runtime.reconcileIdentity(ctx, reason, identityReconcileProbe)
}

func (runtime *accountBindingRuntime) HandleObservedScopeChange(ctx context.Context) error {
	return runtime.reconcileIdentity(ctx, store.CodexAccountBindingReasonAccountChanged, identityReconcileSwitch)
}

type identityReconcileMode int

const (
	identityReconcileStartup identityReconcileMode = iota
	identityReconcileProbe
	identityReconcileSwitch
)

func (runtime *accountBindingRuntime) reconcileIdentity(
	ctx context.Context,
	reason store.CodexAccountBindingReason,
	mode identityReconcileMode,
) error {
	if runtime == nil || ctx == nil {
		return ErrAccountBindingRuntime
	}
	finish, err := runtime.beginTransition(ctx)
	if err != nil {
		return err
	}
	defer finish()

	nowMS := runtime.clock().UnixMilli()
	current, err := runtime.repository.CodexAccountBinding(ctx)
	if err != nil {
		return err
	}
	hideCurrent := (mode == identityReconcileStartup || mode == identityReconcileSwitch) &&
		current.State == store.CodexAccountBindingConfirmed
	if hideCurrent {
		if _, err := runtime.repository.MarkCodexAccountBindingPending(ctx, nowMS, reason); err != nil {
			return err
		}
		runtime.clearActive()
		runtime.clearDisplay()
		if err := runtime.sealQuota(ctx); err != nil {
			return err
		}
		runtime.notifyInvalidation(ctx)
		current, err = runtime.repository.CodexAccountBinding(ctx)
		if err != nil {
			return err
		}
	}

	firstScope, err := runtime.discoverScope(ctx)
	if errors.Is(err, accountbinding.ErrAccountIdentityUnavailable) ||
		errors.Is(err, appserver.ErrAccountIdentityUnavailable) {
		return runtime.markUnavailable(ctx, nowMS, store.CodexAccountBindingReasonMissingAccountID)
	}
	if err != nil {
		if mode == identityReconcileProbe && current.State == store.CodexAccountBindingConfirmed {
			return err
		}
		return runtime.keepPending(ctx, nowMS, store.CodexAccountBindingReasonConfirmationFailed, err)
	}

	if mode == identityReconcileProbe && current.State == store.CodexAccountBindingConfirmed &&
		current.AccountScope != nil && *current.AccountScope == firstScope {
		runtime.setActive(*current.AccountScope, current.BindingGeneration)
		return nil
	}

	if mode == identityReconcileProbe && current.State == store.CodexAccountBindingConfirmed &&
		(current.AccountScope == nil || *current.AccountScope != firstScope) {
		if _, err := runtime.repository.MarkCodexAccountBindingPending(ctx, nowMS, reason); err != nil {
			return err
		}
		runtime.clearActive()
		runtime.clearDisplay()
		if err := runtime.sealQuota(ctx); err != nil {
			return err
		}
		runtime.notifyInvalidation(ctx)
		current, err = runtime.repository.CodexAccountBinding(ctx)
		if err != nil {
			return err
		}
	}

	if mode == identityReconcileSwitch ||
		(mode == identityReconcileProbe && current.State != store.CodexAccountBindingConfirmed) {
		secondScope, confirmErr := runtime.discoverScope(ctx)
		if confirmErr != nil {
			return runtime.keepPending(ctx, nowMS, store.CodexAccountBindingReasonConfirmationFailed, confirmErr)
		}
		if secondScope != firstScope {
			return runtime.keepPending(ctx, nowMS, store.CodexAccountBindingReasonConfirmationFailed, nil)
		}
		firstScope = secondScope
	}

	binding, _, err := runtime.repository.ConfirmCodexAccountBinding(ctx, firstScope, nowMS, reason)
	if err != nil || binding.AccountScope == nil {
		return err
	}
	fence := quotaonline.AccountBindingFence{
		AccountScope: *binding.AccountScope, BindingGeneration: binding.BindingGeneration,
	}
	runtime.setActive(*binding.AccountScope, binding.BindingGeneration)
	if runtime.quota != nil {
		if err := runtime.quota.PublishBinding(ctx, fence); err != nil {
			return err
		}
	}
	runtime.notifyInvalidation(ctx)
	return nil
}

func (runtime *accountBindingRuntime) markUnavailable(
	ctx context.Context,
	nowMS int64,
	reason store.CodexAccountBindingReason,
) error {
	_, _, err := runtime.repository.SetCodexAccountBindingUnavailable(
		ctx, store.CodexAccountBindingIdentityUnavailable, nowMS, reason,
	)
	runtime.clearActive()
	runtime.clearDisplay()
	sealErr := runtime.sealQuota(ctx)
	runtime.notifyInvalidation(ctx)
	return errors.Join(err, sealErr)
}

func (runtime *accountBindingRuntime) keepPending(
	ctx context.Context,
	nowMS int64,
	reason store.CodexAccountBindingReason,
	cause error,
) error {
	_, err := runtime.repository.MarkCodexAccountBindingPending(ctx, nowMS, reason)
	runtime.clearActive()
	runtime.clearDisplay()
	runtime.notifyInvalidation(ctx)
	return errors.Join(cause, err)
}

func (runtime *accountBindingRuntime) LoadDisplay(ctx context.Context) (*accountDisplayCache, error) {
	if runtime == nil || ctx == nil {
		return nil, ErrAccountBindingRuntime
	}
	active := runtime.Active()
	if active == nil {
		runtime.clearDisplay()
		return nil, nil
	}
	if cached := runtime.Display(); cached != nil &&
		cached.Scope == active.Scope && cached.Generation == active.Generation {
		return cached, nil
	}
	display, err := runtime.refreshDisplay(ctx, *active)
	if err != nil {
		return nil, err
	}
	return display, nil
}

func (runtime *accountBindingRuntime) discoverScope(ctx context.Context) (string, error) {
	snapshot, err := runtime.reader.Read(ctx, true)
	if err != nil {
		return "", err
	}
	accountID := append([]byte(nil), snapshot.AccountID...)
	clearAccountID(snapshot.AccountID)
	return accountbinding.DeriveScope(runtime.scopeKey, accountID)
}

func (runtime *accountBindingRuntime) refreshDisplay(
	ctx context.Context,
	active activeCodexAccount,
) (*accountDisplayCache, error) {
	if runtime.sandwich == nil {
		return nil, nil
	}
	sandwich, err := runtime.sandwich(ctx)
	if err != nil {
		runtime.clearDisplay()
		return nil, err
	}
	beforeScope, beforeErr := accountbinding.DeriveScope(runtime.scopeKey, append([]byte(nil), sandwich.BeforeID...))
	afterScope, afterErr := accountbinding.DeriveScope(runtime.scopeKey, append([]byte(nil), sandwich.AfterID...))
	clearAccountID(sandwich.BeforeID)
	clearAccountID(sandwich.AfterID)
	current, bindErr := runtime.repository.CodexAccountBinding(ctx)
	if bindErr != nil {
		runtime.clearDisplay()
		return nil, bindErr
	}
	if beforeErr != nil || afterErr != nil || beforeScope != afterScope ||
		current.State != store.CodexAccountBindingConfirmed || current.AccountScope == nil ||
		*current.AccountScope != beforeScope || current.BindingGeneration != active.Generation ||
		beforeScope != active.Scope {
		runtime.clearDisplay()
		return nil, nil
	}
	display := &accountDisplayCache{
		Scope: active.Scope, Generation: active.Generation, Type: "chatgpt",
	}
	if sandwich.Account != nil {
		display.Type = sandwich.Account.Type
		display.Email = cloneOptionalString(sandwich.Account.Email)
		display.PlanType = cloneOptionalString(sandwich.Account.PlanType)
	}
	runtime.setDisplay(display)
	return display, nil
}

func (runtime *accountBindingRuntime) sealQuota(ctx context.Context) error {
	if runtime.quota == nil {
		return nil
	}
	return runtime.quota.SealAndDrain(ctx)
}

func (runtime *accountBindingRuntime) beginTransition(ctx context.Context) (func(), error) {
	if runtime == nil || ctx == nil || runtime.transition == nil {
		return nil, ErrAccountBindingRuntime
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-runtime.transition:
		var once sync.Once
		return func() {
			once.Do(func() { runtime.transition <- struct{}{} })
		}, nil
	}
}

func (runtime *accountBindingRuntime) setActive(scope string, generation int64) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.active = &activeCodexAccount{Scope: scope, Generation: generation}
	if runtime.display != nil &&
		(runtime.display.Scope != scope || runtime.display.Generation != generation) {
		runtime.display = nil
	}
}

func (runtime *accountBindingRuntime) clearActive() {
	runtime.mu.Lock()
	runtime.active = nil
	runtime.mu.Unlock()
}

func (runtime *accountBindingRuntime) setDisplay(display *accountDisplayCache) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.display = display
}

func (runtime *accountBindingRuntime) clearDisplay() {
	runtime.mu.Lock()
	runtime.display = nil
	runtime.mu.Unlock()
}

func (runtime *accountBindingRuntime) notifyInvalidation(ctx context.Context) {
	notifyQueryInvalidation(runtime.invalidation, ctx, core.InvalidationAccount)
	notifyQueryInvalidation(runtime.invalidation, ctx, core.InvalidationQuota)
}

func clearAccountID(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func cloneOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
