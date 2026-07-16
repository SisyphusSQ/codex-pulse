package scheduler

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/google/uuid"

	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const (
	defaultRefreshClaimLease        = 2 * time.Minute
	defaultRefreshCompletionTimeout = 5 * time.Second
	defaultRefreshDueLimit          = 10
)

var ErrInvalidQuotaRefreshCoordinator = errors.New("invalid quota refresh coordinator")

type RefreshPreferencesReader interface {
	LoadPreferences(context.Context) (preferences.Snapshot, error)
}

type SourceRefreshFetcher interface {
	Fetch(context.Context, string) error
}

type SourceRefreshFunc func(context.Context, string) error

func (function SourceRefreshFunc) Fetch(ctx context.Context, requestID string) error {
	if function == nil {
		return ErrInvalidQuotaRefreshCoordinator
	}
	return function(ctx, requestID)
}

type QuotaFetchService interface {
	Fetch(context.Context, string) (quotaonline.Result, error)
}

type ResetCreditsFetchService interface {
	Fetch(context.Context, string) (quotaonline.ResetCreditsResult, error)
}

// AdaptQuotaFetchService and AdaptResetCreditsFetchService bridge the typed
// clients/services into the scheduler without exposing HTTP result payloads to
// scheduling policy. Each service records its own attempt before returning.
func AdaptQuotaFetchService(service QuotaFetchService) SourceRefreshFetcher {
	if service == nil {
		return nil
	}
	return SourceRefreshFunc(func(ctx context.Context, requestID string) error {
		_, err := service.Fetch(ctx, requestID)
		return err
	})
}

func AdaptResetCreditsFetchService(service ResetCreditsFetchService) SourceRefreshFetcher {
	if service == nil {
		return nil
	}
	return SourceRefreshFunc(func(ctx context.Context, requestID string) error {
		_, err := service.Fetch(ctx, requestID)
		return err
	})
}

type RefreshRequestIDFactory func(quotaonline.RefreshSource) (string, error)

type QuotaRefreshCoordinatorConfig struct {
	Repository          *store.Repository
	Preferences         RefreshPreferencesReader
	QuotaFetcher        SourceRefreshFetcher
	ResetCreditsFetcher SourceRefreshFetcher
	Policy              *quotaonline.RefreshPolicy
	Clock               func() time.Time
	NewRequestID        RefreshRequestIDFactory
	ClaimLease          time.Duration
	CompletionTimeout   time.Duration
	DueLimit            int
}

type QuotaRefreshCoordinator struct {
	repository          *store.Repository
	preferences         RefreshPreferencesReader
	quotaFetcher        SourceRefreshFetcher
	resetCreditsFetcher SourceRefreshFetcher
	policy              quotaonline.RefreshPolicy
	clock               func() time.Time
	newRequestID        RefreshRequestIDFactory
	claimLeaseMS        int64
	completionTimeout   time.Duration
	dueLimit            int

	cycleMu sync.Mutex
}

type quotaRefreshCycleErrors struct {
	fatal       error
	recoverable error
}

func (cycleErrors *quotaRefreshCycleErrors) Error() string {
	return errors.Join(cycleErrors.fatal, cycleErrors.recoverable).Error()
}

func (cycleErrors *quotaRefreshCycleErrors) Unwrap() []error {
	result := make([]error, 0, 2)
	if cycleErrors.fatal != nil {
		result = append(result, cycleErrors.fatal)
	}
	if cycleErrors.recoverable != nil {
		result = append(result, cycleErrors.recoverable)
	}
	return result
}

func (cycleErrors *quotaRefreshCycleErrors) add(err error, fatal bool) {
	if err == nil {
		return
	}
	if fatal {
		cycleErrors.fatal = errors.Join(cycleErrors.fatal, err)
		return
	}
	cycleErrors.recoverable = errors.Join(cycleErrors.recoverable, err)
}

func (cycleErrors *quotaRefreshCycleErrors) result() error {
	if cycleErrors.fatal == nil && cycleErrors.recoverable == nil {
		return nil
	}
	return cycleErrors
}

type refreshSourceDescriptor struct {
	source           quotaonline.RefreshSource
	sourceInstanceID string
	sourceType       string
	scopeKey         string
	enabled          bool
	intervalSeconds  int64
	fetcher          SourceRefreshFetcher
}

func NewQuotaRefreshCoordinator(config QuotaRefreshCoordinatorConfig) (*QuotaRefreshCoordinator, error) {
	if config.Repository == nil || config.Preferences == nil || config.QuotaFetcher == nil ||
		config.ResetCreditsFetcher == nil {
		return nil, ErrInvalidQuotaRefreshCoordinator
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.NewRequestID == nil {
		config.NewRequestID = randomRefreshRequestID
	}
	if config.ClaimLease == 0 {
		config.ClaimLease = defaultRefreshClaimLease
	}
	if config.CompletionTimeout == 0 {
		config.CompletionTimeout = defaultRefreshCompletionTimeout
	}
	if config.DueLimit == 0 {
		config.DueLimit = defaultRefreshDueLimit
	}
	if config.ClaimLease <= 0 || config.ClaimLease > 24*time.Hour ||
		config.CompletionTimeout <= 0 || config.CompletionTimeout > time.Minute ||
		config.DueLimit < 1 || config.DueLimit > 100 {
		return nil, ErrInvalidQuotaRefreshCoordinator
	}
	policy := config.Policy
	if policy == nil {
		created, err := quotaonline.NewRefreshPolicy(rand.Float64)
		if err != nil {
			return nil, ErrInvalidQuotaRefreshCoordinator
		}
		policy = &created
	}
	return &QuotaRefreshCoordinator{
		repository: config.Repository, preferences: config.Preferences,
		quotaFetcher: config.QuotaFetcher, resetCreditsFetcher: config.ResetCreditsFetcher,
		policy: *policy, clock: config.Clock, newRequestID: config.NewRequestID,
		claimLeaseMS: config.ClaimLease.Milliseconds(), completionTimeout: config.CompletionTimeout,
		dueLimit: config.DueLimit,
	}, nil
}

// Initialize recovers expired durable claims, revalidates both source plans
// against the current wall clock and preferences, then performs one immediate
// due cycle before cron starts.
func (coordinator *QuotaRefreshCoordinator) Initialize(ctx context.Context) error {
	if coordinator == nil || ctx == nil {
		return ErrInvalidQuotaRefreshCoordinator
	}
	coordinator.cycleMu.Lock()
	defer coordinator.cycleMu.Unlock()
	nowMS := coordinator.clock().UnixMilli()
	snapshot, err := coordinator.preferences.LoadPreferences(ctx)
	if err != nil {
		return err
	}
	cycleErrors := &quotaRefreshCycleErrors{}
	recoveredIDs, err := coordinator.recoverExpiredClaimsLocked(ctx, snapshot, nowMS)
	if err != nil {
		cycleErrors.add(err, isPermanentQuotaRefreshCycleError(err))
		return cycleErrors.result()
	}
	for _, descriptor := range coordinator.descriptors(snapshot) {
		trigger := store.RefreshTriggerStartup
		if _, found := recoveredIDs[descriptor.sourceInstanceID]; found {
			trigger = store.RefreshTriggerRecovery
		}
		if _, err := coordinator.replanSource(ctx, descriptor, trigger, nowMS); err != nil {
			cycleErrors.add(err, isPermanentQuotaRefreshCycleError(err))
		}
	}
	if cycleErrors.result() != nil {
		return cycleErrors.result()
	}
	return coordinator.runDueCycleLocked(ctx, snapshot, nowMS)
}

func (coordinator *QuotaRefreshCoordinator) RunDueCycle(ctx context.Context) error {
	if coordinator == nil || ctx == nil {
		return ErrInvalidQuotaRefreshCoordinator
	}
	coordinator.cycleMu.Lock()
	defer coordinator.cycleMu.Unlock()
	snapshot, err := coordinator.preferences.LoadPreferences(ctx)
	if err != nil {
		return err
	}
	return coordinator.runDueCycleLocked(ctx, snapshot, coordinator.clock().UnixMilli())
}

// ReconcilePreferences is the settings-commit hook. It persists disabled
// sources immediately and starts newly enabled, never-loaded sources without
// waiting for their previous due time.
func (coordinator *QuotaRefreshCoordinator) ReconcilePreferences(ctx context.Context) error {
	if coordinator == nil || ctx == nil {
		return ErrInvalidQuotaRefreshCoordinator
	}
	coordinator.cycleMu.Lock()
	defer coordinator.cycleMu.Unlock()
	snapshot, err := coordinator.preferences.LoadPreferences(ctx)
	if err != nil {
		return err
	}
	nowMS := coordinator.clock().UnixMilli()
	for _, descriptor := range coordinator.descriptors(snapshot) {
		if _, err := coordinator.replanSource(ctx, descriptor, store.RefreshTriggerStartup, nowMS); err != nil {
			return err
		}
	}
	return coordinator.runDueCycleLocked(ctx, snapshot, nowMS)
}

func (coordinator *QuotaRefreshCoordinator) RequestRefresh(
	ctx context.Context,
	source quotaonline.RefreshSource,
	trigger store.SourceRefreshTrigger,
) (store.SourceRefreshSchedule, error) {
	if coordinator == nil || ctx == nil ||
		(trigger != store.RefreshTriggerManual && trigger != store.RefreshTriggerForeground &&
			trigger != store.RefreshTriggerWake) {
		return store.SourceRefreshSchedule{}, ErrInvalidQuotaRefreshCoordinator
	}
	coordinator.cycleMu.Lock()
	defer coordinator.cycleMu.Unlock()
	snapshot, err := coordinator.preferences.LoadPreferences(ctx)
	if err != nil {
		return store.SourceRefreshSchedule{}, err
	}
	descriptor, found := coordinator.descriptor(snapshot, source)
	if !found {
		return store.SourceRefreshSchedule{}, ErrInvalidQuotaRefreshCoordinator
	}
	nowMS := coordinator.clock().UnixMilli()
	schedule, decision, err := coordinator.planSource(ctx, descriptor, trigger, nowMS)
	if err != nil {
		return store.SourceRefreshSchedule{}, err
	}
	if !decision.ShouldFetch {
		if schedule == nil {
			return coordinator.persistDecision(ctx, descriptor, nil, decision, nowMS)
		}
		return *schedule, nil
	}
	persisted, err := coordinator.persistDecision(ctx, descriptor, schedule, decision, nowMS)
	if err != nil {
		return store.SourceRefreshSchedule{}, err
	}
	requestID, err := coordinator.newRequestID(source)
	if err != nil || requestID == "" {
		if err == nil {
			err = ErrInvalidQuotaRefreshCoordinator
		}
		return persisted, err
	}
	claimed, ok, err := coordinator.repository.ClaimSourceRefresh(
		ctx, descriptor.sourceInstanceID, persisted.Revision, requestID, trigger, nowMS, coordinator.claimLeaseMS,
	)
	if err != nil || !ok {
		return claimed, err
	}
	return coordinator.executeClaim(ctx, snapshot, descriptor, claimed, requestID)
}

func (coordinator *QuotaRefreshCoordinator) runDueCycleLocked(
	ctx context.Context,
	snapshot preferences.Snapshot,
	nowMS int64,
) error {
	cycleErrors := &quotaRefreshCycleErrors{}
	recoveredIDs, err := coordinator.recoverExpiredClaimsLocked(ctx, snapshot, nowMS)
	if err != nil {
		cycleErrors.add(err, isPermanentQuotaRefreshCycleError(err))
		return cycleErrors.result()
	}
	for sourceInstanceID := range recoveredIDs {
		descriptor, found := coordinator.descriptorByInstance(snapshot, sourceInstanceID)
		if !found {
			cycleErrors.add(ErrInvalidQuotaRefreshCoordinator, true)
			continue
		}
		if _, err := coordinator.replanSource(ctx, descriptor, store.RefreshTriggerRecovery, nowMS); err != nil {
			cycleErrors.add(err, isPermanentQuotaRefreshCycleError(err))
		}
	}
	if cycleErrors.result() != nil {
		return cycleErrors.result()
	}
	due, err := coordinator.repository.ListDueSourceRefreshSchedules(ctx, nowMS, coordinator.dueLimit)
	if err != nil {
		cycleErrors.add(err, isPermanentQuotaRefreshCycleError(err))
		return cycleErrors.result()
	}
	for _, schedule := range due {
		descriptor, found := coordinator.descriptorByInstance(snapshot, schedule.SourceInstanceID)
		if !found {
			cycleErrors.add(ErrInvalidQuotaRefreshCoordinator, true)
			continue
		}
		if !descriptor.enabled {
			_, err := coordinator.persistDecision(ctx, descriptor, &schedule, quotaonline.RefreshDecision{
				Reason: store.RefreshReasonDisabled,
			}, nowMS)
			cycleErrors.add(err, isPermanentQuotaRefreshCycleError(err))
			continue
		}
		requestID, err := coordinator.newRequestID(descriptor.source)
		if err != nil || requestID == "" {
			if err == nil {
				err = ErrInvalidQuotaRefreshCoordinator
			}
			cycleErrors.add(err, true)
			continue
		}
		trigger := triggerForRefreshReason(schedule.Reason)
		claimed, ok, err := coordinator.repository.ClaimSourceRefresh(
			ctx, descriptor.sourceInstanceID, schedule.Revision, requestID, trigger, nowMS, coordinator.claimLeaseMS,
		)
		if err != nil {
			cycleErrors.add(err, isPermanentQuotaRefreshCycleError(err))
			continue
		}
		if !ok {
			continue
		}
		if _, err := coordinator.executeClaim(ctx, snapshot, descriptor, claimed, requestID); err != nil {
			cycleErrors.add(err, isPermanentQuotaRefreshCycleError(err))
		}
	}
	return cycleErrors.result()
}

func (coordinator *QuotaRefreshCoordinator) recoverExpiredClaimsLocked(
	ctx context.Context,
	snapshot preferences.Snapshot,
	nowMS int64,
) (map[string]struct{}, error) {
	expired, err := coordinator.repository.ListExpiredSourceRefreshClaims(ctx, nowMS, 100)
	if err != nil {
		return nil, err
	}
	unrecorded := make(map[string]struct{}, len(expired))
	for _, claimed := range expired {
		descriptor, found := coordinator.descriptorByInstance(snapshot, claimed.SourceInstanceID)
		if !found || claimed.ActiveClaimID == nil {
			return nil, ErrInvalidQuotaRefreshCoordinator
		}
		claimID := *claimed.ActiveClaimID
		attempt, attemptErr := coordinator.repository.SourceAttempt(ctx, claimID)
		switch {
		case attemptErr == nil:
			if attempt.SourceInstanceID != claimed.SourceInstanceID {
				return nil, ErrInvalidQuotaRefreshCoordinator
			}
			if _, err := coordinator.completeRecordedClaim(ctx, snapshot, descriptor, claimed, claimID); err != nil {
				return nil, err
			}
		case errors.Is(attemptErr, store.ErrNotFound):
			recovered, released, err := coordinator.repository.ReleaseExpiredSourceRefreshClaim(ctx, store.SourceRefreshClaimRecovery{
				SourceInstanceID: claimed.SourceInstanceID, ClaimID: claimID,
				ExpectedRevision: claimed.Revision, AtMS: nowMS,
			})
			if err != nil {
				return nil, err
			}
			if !released {
				attempt, err := coordinator.repository.SourceAttempt(ctx, claimID)
				if err != nil || attempt.SourceInstanceID != claimed.SourceInstanceID {
					if err != nil {
						return nil, err
					}
					return nil, ErrInvalidQuotaRefreshCoordinator
				}
				if _, err := coordinator.completeRecordedClaim(ctx, snapshot, descriptor, recovered, claimID); err != nil {
					return nil, err
				}
				continue
			}
			unrecorded[claimed.SourceInstanceID] = struct{}{}
		default:
			return nil, attemptErr
		}
	}
	return unrecorded, nil
}

func (coordinator *QuotaRefreshCoordinator) executeClaim(
	ctx context.Context,
	snapshot preferences.Snapshot,
	descriptor refreshSourceDescriptor,
	claimed store.SourceRefreshSchedule,
	requestID string,
) (store.SourceRefreshSchedule, error) {
	if err := descriptor.fetcher.Fetch(ctx, requestID); err != nil {
		// A recorder/storage error makes durability unknown. Keep the claim until
		// its lease expires so recovery, rather than an immediate duplicate call,
		// decides the next request.
		return claimed, err
	}
	return coordinator.completeRecordedClaim(ctx, snapshot, descriptor, claimed, requestID)
}

func (coordinator *QuotaRefreshCoordinator) completeRecordedClaim(
	ctx context.Context,
	snapshot preferences.Snapshot,
	descriptor refreshSourceDescriptor,
	claimed store.SourceRefreshSchedule,
	requestID string,
) (store.SourceRefreshSchedule, error) {
	completionCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), coordinator.completionTimeout)
	defer cancel()
	nowMS := coordinator.clock().UnixMilli()
	state, err := coordinator.loadSourceState(completionCtx, descriptor.sourceInstanceID)
	if err != nil {
		return claimed, err
	}
	windows, err := coordinator.loadQuotaWindows(completionCtx, descriptor.source, nowMS)
	if err != nil {
		return claimed, err
	}
	decision, err := coordinator.policy.Plan(quotaonline.RefreshPlanInput{
		Source: descriptor.source, Trigger: store.RefreshTriggerScheduled,
		Enabled: descriptor.enabled, NowMS: nowMS, IntervalSeconds: descriptor.intervalSeconds,
		Schedule: &claimed, SourceState: state, Windows: windows,
	})
	if err != nil {
		return claimed, err
	}
	return coordinator.repository.CompleteSourceRefresh(completionCtx, store.SourceRefreshCompletion{
		SourceInstanceID: descriptor.sourceInstanceID, ClaimID: requestID,
		ExpectedRevision: claimed.Revision, NextDueAtMS: decision.NextDueAtMS,
		Reason: decision.Reason, AtMS: nowMS,
	})
}

func isPermanentQuotaRefreshCycleError(err error) bool {
	return errors.Is(err, ErrInvalidQuotaRefreshCoordinator) ||
		errors.Is(err, quotaonline.ErrInvalidRefreshPolicy) ||
		errors.Is(err, store.ErrInvalidRepository) || errors.Is(err, store.ErrInvalidRecord) ||
		errors.Is(err, storesqlite.ErrInvalidConfig) || errors.Is(err, storesqlite.ErrInvalidPath) ||
		errors.Is(err, storesqlite.ErrClosing) || errors.Is(err, storesqlite.ErrClosed) ||
		errors.Is(err, storesqlite.ErrDiskFull) || errors.Is(err, storesqlite.ErrReadOnly) ||
		errors.Is(err, storesqlite.ErrPermission) || errors.Is(err, storesqlite.ErrCorrupt)
}

func shouldStopQuotaRefreshRunner(err error) bool {
	if err == nil {
		return false
	}
	var cycleErrors *quotaRefreshCycleErrors
	return !errors.As(err, &cycleErrors) || cycleErrors.fatal != nil
}

func (coordinator *QuotaRefreshCoordinator) replanSource(
	ctx context.Context,
	descriptor refreshSourceDescriptor,
	trigger store.SourceRefreshTrigger,
	nowMS int64,
) (store.SourceRefreshSchedule, error) {
	schedule, decision, err := coordinator.planSource(ctx, descriptor, trigger, nowMS)
	if err != nil {
		return store.SourceRefreshSchedule{}, err
	}
	if schedule != nil && schedule.ActiveClaimID != nil {
		return *schedule, nil
	}
	return coordinator.persistDecision(ctx, descriptor, schedule, decision, nowMS)
}

func (coordinator *QuotaRefreshCoordinator) planSource(
	ctx context.Context,
	descriptor refreshSourceDescriptor,
	trigger store.SourceRefreshTrigger,
	nowMS int64,
) (*store.SourceRefreshSchedule, quotaonline.RefreshDecision, error) {
	schedule, err := coordinator.loadSchedule(ctx, descriptor.sourceInstanceID)
	if err != nil {
		return nil, quotaonline.RefreshDecision{}, err
	}
	state, err := coordinator.loadSourceState(ctx, descriptor.sourceInstanceID)
	if err != nil {
		return nil, quotaonline.RefreshDecision{}, err
	}
	windows, err := coordinator.loadQuotaWindows(ctx, descriptor.source, nowMS)
	if err != nil {
		return nil, quotaonline.RefreshDecision{}, err
	}
	decision, err := coordinator.policy.Plan(quotaonline.RefreshPlanInput{
		Source: descriptor.source, Trigger: trigger, Enabled: descriptor.enabled,
		NowMS: nowMS, IntervalSeconds: descriptor.intervalSeconds,
		Schedule: schedule, SourceState: state, Windows: windows,
	})
	return schedule, decision, err
}

func (coordinator *QuotaRefreshCoordinator) persistDecision(
	ctx context.Context,
	descriptor refreshSourceDescriptor,
	schedule *store.SourceRefreshSchedule,
	decision quotaonline.RefreshDecision,
	atMS int64,
) (store.SourceRefreshSchedule, error) {
	if schedule != nil && equalRefreshDecision(*schedule, decision) {
		return *schedule, nil
	}
	expectedRevision := int64(0)
	if schedule != nil {
		expectedRevision = schedule.Revision
	}
	return coordinator.repository.UpsertSourceRefreshSchedule(ctx, store.SourceRefreshScheduleUpdate{
		SourceInstanceID: descriptor.sourceInstanceID, SourceType: descriptor.sourceType,
		ScopeKey: descriptor.scopeKey, ExpectedRevision: expectedRevision,
		NextDueAtMS: decision.NextDueAtMS, Reason: decision.Reason, AtMS: atMS,
	})
}

func (coordinator *QuotaRefreshCoordinator) loadSchedule(
	ctx context.Context,
	sourceInstanceID string,
) (*store.SourceRefreshSchedule, error) {
	schedule, err := coordinator.repository.SourceRefreshSchedule(ctx, sourceInstanceID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &schedule, nil
}

func (coordinator *QuotaRefreshCoordinator) loadSourceState(
	ctx context.Context,
	sourceInstanceID string,
) (*store.SourceState, error) {
	state, err := coordinator.repository.SourceState(ctx, sourceInstanceID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &state, nil
}

func (coordinator *QuotaRefreshCoordinator) loadQuotaWindows(
	ctx context.Context,
	source quotaonline.RefreshSource,
	nowMS int64,
) ([]store.QuotaCurrent, error) {
	if source != quotaonline.RefreshSourceQuota {
		return nil, nil
	}
	return coordinator.repository.ListQuotaCurrent(ctx, store.QuotaAccountScopeDefault, nowMS)
}

func (coordinator *QuotaRefreshCoordinator) descriptors(snapshot preferences.Snapshot) []refreshSourceDescriptor {
	return []refreshSourceDescriptor{
		{
			source: quotaonline.RefreshSourceQuota, sourceInstanceID: store.QuotaSourceInstanceWhamDefault,
			sourceType: store.QuotaSourceTypeWham, scopeKey: store.QuotaAccountScopeDefault,
			enabled: snapshot.Online.QuotaEnabled, intervalSeconds: snapshot.Refresh.QuotaIntervalSeconds,
			fetcher: coordinator.quotaFetcher,
		},
		{
			source: quotaonline.RefreshSourceResetCredits, sourceInstanceID: store.ResetCreditsSourceInstanceWhamDefault,
			sourceType: store.ResetCreditsSourceTypeWham, scopeKey: store.QuotaAccountScopeDefault,
			enabled:         snapshot.Online.ResetCreditsEnabled,
			intervalSeconds: snapshot.Refresh.ResetCreditsIntervalSeconds,
			fetcher:         coordinator.resetCreditsFetcher,
		},
	}
}

func (coordinator *QuotaRefreshCoordinator) descriptor(
	snapshot preferences.Snapshot,
	source quotaonline.RefreshSource,
) (refreshSourceDescriptor, bool) {
	for _, descriptor := range coordinator.descriptors(snapshot) {
		if descriptor.source == source {
			return descriptor, true
		}
	}
	return refreshSourceDescriptor{}, false
}

func (coordinator *QuotaRefreshCoordinator) descriptorByInstance(
	snapshot preferences.Snapshot,
	sourceInstanceID string,
) (refreshSourceDescriptor, bool) {
	for _, descriptor := range coordinator.descriptors(snapshot) {
		if descriptor.sourceInstanceID == sourceInstanceID {
			return descriptor, true
		}
	}
	return refreshSourceDescriptor{}, false
}

func equalRefreshDecision(schedule store.SourceRefreshSchedule, decision quotaonline.RefreshDecision) bool {
	if schedule.Reason != decision.Reason || (schedule.NextDueAtMS == nil) != (decision.NextDueAtMS == nil) {
		return false
	}
	return schedule.NextDueAtMS == nil || *schedule.NextDueAtMS == *decision.NextDueAtMS
}

func triggerForRefreshReason(reason store.SourceRefreshReason) store.SourceRefreshTrigger {
	switch reason {
	case store.RefreshReasonManual:
		return store.RefreshTriggerManual
	case store.RefreshReasonForeground:
		return store.RefreshTriggerForeground
	case store.RefreshReasonWakeStale:
		return store.RefreshTriggerWake
	case store.RefreshReasonStartup:
		return store.RefreshTriggerStartup
	case store.RefreshReasonRecovery:
		return store.RefreshTriggerRecovery
	default:
		return store.RefreshTriggerScheduled
	}
}

func randomRefreshRequestID(source quotaonline.RefreshSource) (string, error) {
	if source != quotaonline.RefreshSourceQuota && source != quotaonline.RefreshSourceResetCredits {
		return "", ErrInvalidQuotaRefreshCoordinator
	}
	return fmt.Sprintf("refresh-%s-%s", source, uuid.NewString()), nil
}
