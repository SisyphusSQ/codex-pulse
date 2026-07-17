package health

import (
	"errors"
	"sort"
	"strings"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type Level string

const (
	LevelHealthy  Level = "healthy"
	LevelBusy     Level = "busy"
	LevelPaused   Level = "paused"
	LevelDegraded Level = "degraded"
	LevelBlocked  Level = "blocked"
)

type Component string

const (
	ComponentLocalIndex      Component = "local_index"
	ComponentLiveQueue       Component = "live_queue"
	ComponentHistoryBackfill Component = "history_backfill"
	ComponentOnlineQuota     Component = "online_quota"
	ComponentStorage         Component = "storage"
	ComponentRuntime         Component = "runtime"
	ComponentUpdater         Component = "updater"
)

var componentOrder = []Component{
	ComponentLocalIndex, ComponentLiveQueue, ComponentHistoryBackfill,
	ComponentOnlineQuota, ComponentStorage, ComponentRuntime, ComponentUpdater,
}

type Evidence string

const (
	EvidenceKnown         Evidence = "known"
	EvidenceUnknown       Evidence = "unknown"
	EvidenceNotConfigured Evidence = "not_configured"
)

type Reason string

const (
	ReasonHealthy             Reason = "healthy"
	ReasonNotConfigured       Reason = "not_configured"
	ReasonIndexPaused         Reason = "index_paused"
	ReasonIndexDraining       Reason = "index_draining"
	ReasonIndexReconciling    Reason = "index_reconciling"
	ReasonSystemSleeping      Reason = "system_sleeping"
	ReasonBackfillPaused      Reason = "backfill_paused"
	ReasonLiveQueueStalled    Reason = "live_queue_stalled"
	ReasonBackfillStalled     Reason = "backfill_stalled"
	ReasonAuthRequired        Reason = "auth_required"
	ReasonDiskLow             Reason = "disk_low"
	ReasonCPUPressure         Reason = "cpu_pressure"
	ReasonMemoryPressure      Reason = "memory_pressure"
	ReasonUpdaterUnavailable  Reason = "updater_unavailable"
	ReasonUpdaterUnknown      Reason = "updater_unknown"
	ReasonMetricsStale        Reason = "metrics_stale"
	ReasonSourceTimeout       Reason = "source_timeout"
	ReasonSourceUnavailable   Reason = "source_unavailable"
	ReasonSourcePermission    Reason = "source_permission"
	ReasonSourceCorrupt       Reason = "source_corrupt"
	ReasonSourceStale         Reason = "source_stale"
	ReasonJobInterrupted      Reason = "job_interrupted"
	ReasonJobFailed           Reason = "job_failed"
	ReasonJobCancelled        Reason = "job_cancelled"
	ReasonStoreBusy           Reason = "store_busy"
	ReasonStoreDiskFull       Reason = "store_disk_full"
	ReasonStoreReadOnly       Reason = "store_read_only"
	ReasonStorePermission     Reason = "store_permission"
	ReasonStoreIO             Reason = "store_io"
	ReasonStoreCorrupt        Reason = "store_corrupt"
	ReasonStoreUnavailable    Reason = "store_unavailable"
	ReasonStoreUnknown        Reason = "store_unknown"
	ReasonWALPressure         Reason = "wal_pressure"
	ReasonPricingUnavailable  Reason = "pricing_unavailable"
	ReasonPricingInvalid      Reason = "pricing_invalid"
	ReasonRuntimeUnknown      Reason = "runtime_unknown"
	ReasonLifecycleUnknown    Reason = "lifecycle_unknown"
	ReasonSourceUnknown       Reason = "source_unknown"
	ReasonSourceFailureStreak Reason = "source_failure_streak"
)

type Impact string

const (
	ImpactNone                    Impact = "none"
	ImpactIndexingStopped         Impact = "indexing_stopped"
	ImpactIndexingPaused          Impact = "indexing_paused"
	ImpactLiveDataDelayed         Impact = "live_data_delayed"
	ImpactHistoryIncomplete       Impact = "history_incomplete"
	ImpactOnlineQuotaUnavailable  Impact = "online_quota_unavailable"
	ImpactStorageAtRisk           Impact = "storage_at_risk"
	ImpactRuntimeAtRisk           Impact = "runtime_at_risk"
	ImpactUpdateChecksUnavailable Impact = "update_checks_unavailable"
)

type Protection string

const (
	ProtectionNone              Protection = "none"
	ProtectionWritesStopped     Protection = "writes_stopped"
	ProtectionAutoRetryStopped  Protection = "auto_retry_stopped"
	ProtectionUserPauseRetained Protection = "user_pause_retained"
	ProtectionRetryBackoff      Protection = "retry_backoff"
	ProtectionObservationOnly   Protection = "observation_only"
)

type RecoveryAction string

const (
	RecoveryNone            RecoveryAction = "none"
	RecoveryRetry           RecoveryAction = "retry"
	RecoveryCheckSource     RecoveryAction = "check_source"
	RecoveryGrantPermission RecoveryAction = "grant_permission"
	RecoveryFreeSpace       RecoveryAction = "free_space"
	RecoveryRepairStore     RecoveryAction = "repair_store"
)

type UpdaterState string

const (
	UpdaterCurrent       UpdaterState = "current"
	UpdaterChecking      UpdaterState = "checking"
	UpdaterUnavailable   UpdaterState = "unavailable"
	UpdaterUnknown       UpdaterState = "unknown"
	UpdaterNotConfigured UpdaterState = "not_configured"
)

type ComponentStatus struct {
	Component      Component
	Level          Level
	Evidence       Evidence
	Reason         Reason
	Impact         Impact
	Protection     Protection
	RecoveryAction RecoveryAction
	EventCode      store.HealthCode
}

type Result struct {
	Level      Level
	Primary    *ComponentStatus
	Components []ComponentStatus
	EventBatch store.HealthEvaluationBatch
}

type Input struct {
	EvaluatedAtMS int64
	Updater       UpdaterState
	Snapshot      store.HealthEvaluationSnapshot
}

type Thresholds struct {
	LiveWaitMS             int64
	BackfillWaitMS         int64
	DiskFreeBytes          int64
	CPUPercent             float64
	RSSBytes               int64
	SustainedMS            int64
	MaxSampleGapMS         int64
	SourceWarningFailures  int64
	SourceDegradedFailures int64
}

func DefaultThresholds() Thresholds {
	return Thresholds{
		LiveWaitMS: 30_000, BackfillWaitMS: 300_000, DiskFreeBytes: 1 << 30,
		CPUPercent: 20, RSSBytes: 512 << 20, SustainedMS: 120_000, MaxSampleGapMS: 65_000,
		SourceWarningFailures: 3, SourceDegradedFailures: 10,
	}
}

type Evaluator struct{ thresholds Thresholds }

func NewEvaluator(thresholds Thresholds) (*Evaluator, error) {
	if thresholds.LiveWaitMS <= 0 || thresholds.BackfillWaitMS <= 0 || thresholds.DiskFreeBytes <= 0 ||
		thresholds.CPUPercent <= 0 || thresholds.RSSBytes <= 0 || thresholds.SustainedMS <= 0 ||
		thresholds.MaxSampleGapMS <= 0 || thresholds.SourceWarningFailures <= 0 ||
		thresholds.SourceDegradedFailures <= thresholds.SourceWarningFailures {
		return nil, errors.New("health thresholds must be positive")
	}
	return &Evaluator{thresholds: thresholds}, nil
}

type rule struct {
	component  Component
	reason     Reason
	level      Level
	impact     Impact
	protection Protection
	action     RecoveryAction
	domain     store.HealthDomain
	code       store.HealthCode
}

var managedRules = []rule{
	{ComponentLocalIndex, ReasonSourceUnavailable, LevelDegraded, ImpactIndexingStopped, ProtectionWritesStopped, RecoveryCheckSource, store.HealthDomainSource, store.HealthCodeSourceUnavailable},
	{ComponentLiveQueue, ReasonLiveQueueStalled, LevelDegraded, ImpactLiveDataDelayed, ProtectionRetryBackoff, RecoveryRetry, store.HealthDomainJob, store.HealthCodeJobLiveQueueStalled},
	{ComponentHistoryBackfill, ReasonBackfillStalled, LevelDegraded, ImpactHistoryIncomplete, ProtectionRetryBackoff, RecoveryRetry, store.HealthDomainJob, store.HealthCodeJobBackfillStalled},
	{ComponentOnlineQuota, ReasonAuthRequired, LevelDegraded, ImpactOnlineQuotaUnavailable, ProtectionAutoRetryStopped, RecoveryGrantPermission, store.HealthDomainSource, store.HealthCodeSourceAuthRequired},
	{ComponentStorage, ReasonDiskLow, LevelBusy, ImpactStorageAtRisk, ProtectionObservationOnly, RecoveryFreeSpace, store.HealthDomainStore, store.HealthCodeStoreDiskLow},
	{ComponentRuntime, ReasonCPUPressure, LevelDegraded, ImpactRuntimeAtRisk, ProtectionObservationOnly, RecoveryRetry, store.HealthDomainRuntime, store.HealthCodeRuntimeCPUPressure},
	{ComponentRuntime, ReasonMemoryPressure, LevelDegraded, ImpactRuntimeAtRisk, ProtectionObservationOnly, RecoveryRetry, store.HealthDomainRuntime, store.HealthCodeRuntimeMemoryPressure},
	{ComponentUpdater, ReasonUpdaterUnavailable, LevelDegraded, ImpactUpdateChecksUnavailable, ProtectionObservationOnly, RecoveryRetry, store.HealthDomainRuntime, store.HealthCodeRuntimeUpdaterUnavailable},
	{ComponentUpdater, ReasonUpdaterUnknown, LevelDegraded, ImpactUpdateChecksUnavailable, ProtectionObservationOnly, RecoveryRetry, store.HealthDomainRuntime, store.HealthCodeRuntimeUpdaterUnknown},
	{ComponentRuntime, ReasonMetricsStale, LevelDegraded, ImpactRuntimeAtRisk, ProtectionObservationOnly, RecoveryRetry, store.HealthDomainRuntime, store.HealthCodeRuntimeMetricsStale},
	{ComponentOnlineQuota, ReasonSourceFailureStreak, LevelBusy, ImpactOnlineQuotaUnavailable, ProtectionRetryBackoff, RecoveryRetry, store.HealthDomainSource, store.HealthCodeSourceFailureStreak},
}

func (evaluator *Evaluator) Evaluate(input Input) (Result, error) {
	if evaluator == nil || input.EvaluatedAtMS < 0 {
		return Result{}, errors.New("health evaluation input is invalid")
	}
	statuses := make([]ComponentStatus, 0, len(componentOrder))
	for _, component := range componentOrder {
		statuses = append(statuses, ComponentStatus{Component: component, Level: LevelHealthy,
			Evidence: EvidenceKnown, Reason: ReasonHealthy, Impact: ImpactNone,
			Protection: ProtectionNone, RecoveryAction: RecoveryNone})
	}
	observations := make([]store.HealthObservation, 0, len(managedRules))
	applyStatus := func(candidate ComponentStatus) {
		index := componentIndex(candidate.Component)
		if levelRank(candidate.Level) > levelRank(statuses[index].Level) {
			statuses[index] = candidate
		}
	}
	applyAtLevel := func(value rule, level Level) {
		candidate := ComponentStatus{Component: value.component, Level: level, Evidence: EvidenceKnown,
			Reason: value.reason, Impact: value.impact, Protection: value.protection,
			RecoveryAction: value.action, EventCode: value.code}
		applyStatus(candidate)
		identity := eventIdentity(value)
		observations = append(observations, store.HealthObservation{
			EventID: identity, Fingerprint: store.SHA256DigestOf([]byte(identity)), Domain: value.domain,
			Severity: severityForLevel(level), Code: value.code, ObservedAtMS: input.EvaluatedAtMS,
		})
	}
	apply := func(value rule) { applyAtLevel(value, value.level) }

	activeHealth := append([]store.HealthEventMetric(nil), input.Snapshot.ActiveHealth...)
	sort.Slice(activeHealth, func(left, right int) bool {
		if activeHealth[left].Domain != activeHealth[right].Domain {
			return activeHealth[left].Domain < activeHealth[right].Domain
		}
		if activeHealth[left].Severity != activeHealth[right].Severity {
			return activeHealth[left].Severity < activeHealth[right].Severity
		}
		return activeHealth[left].Code < activeHealth[right].Code
	})
	for _, metric := range activeHealth {
		descriptor, ok := DescribeEvent(metric.Domain, metric.Code)
		level, levelOK := levelForSeverity(metric.Severity)
		if !ok || !levelOK || metric.Count < 1 {
			return Result{}, errors.New("active health metric is invalid")
		}
		applyStatus(ComponentStatus{
			Component: descriptor.Component, Level: level, Evidence: EvidenceKnown,
			Reason: descriptor.Rule, Impact: descriptor.Impact, Protection: descriptor.Protection,
			RecoveryAction: descriptor.RecoveryAction, EventCode: metric.Code,
		})
	}

	liveQueueEligible := true
	backfillEligible := true
	lifecycle := input.Snapshot.Lifecycle
	if lifecycle == nil {
		applyStatus(ComponentStatus{
			Component: ComponentLocalIndex, Level: LevelDegraded, Evidence: EvidenceUnknown,
			Reason: ReasonLifecycleUnknown, Impact: ImpactIndexingStopped,
			Protection: ProtectionObservationOnly, RecoveryAction: RecoveryCheckSource,
		})
	} else if !validLifecycle(*lifecycle, input.EvaluatedAtMS) {
		return Result{}, errors.New("scheduler lifecycle is invalid")
	} else {
		switch lifecycle.Transition {
		case store.LifecycleTransitionBlocked:
			applyAtLevel(managedRules[0], LevelBlocked)
		case store.LifecycleTransitionDraining:
			applyStatus(busyStatus(ComponentLocalIndex, ReasonIndexDraining))
		case store.LifecycleTransitionReconciling:
			applyStatus(busyStatus(ComponentLocalIndex, ReasonIndexReconciling))
		}
		switch lifecycle.SourceState {
		case store.LifecycleSourceUnavailable:
			if lifecycle.Transition != store.LifecycleTransitionBlocked {
				apply(managedRules[0])
			}
		case store.LifecycleSourceUnknown:
			applyStatus(ComponentStatus{
				Component: ComponentLocalIndex, Level: LevelDegraded, Evidence: EvidenceUnknown,
				Reason: ReasonSourceUnknown, Impact: ImpactIndexingStopped,
				Protection: ProtectionObservationOnly, RecoveryAction: RecoveryCheckSource,
			})
		}
		if lifecycle.SystemState == store.LifecycleSystemSleeping {
			liveQueueEligible = false
			backfillEligible = false
			for _, component := range []Component{ComponentLocalIndex, ComponentLiveQueue, ComponentHistoryBackfill} {
				applyStatus(pausedStatus(component, ReasonSystemSleeping))
			}
		}
		switch lifecycle.UserPauseScope {
		case store.LifecyclePauseAll:
			liveQueueEligible = false
			backfillEligible = false
			applyStatus(pausedStatus(ComponentLocalIndex, ReasonIndexPaused))
			applyStatus(pausedStatus(ComponentLiveQueue, ReasonIndexPaused))
			applyStatus(pausedStatus(ComponentHistoryBackfill, ReasonBackfillPaused))
		case store.LifecyclePauseBackfill:
			backfillEligible = false
			applyStatus(pausedStatus(ComponentHistoryBackfill, ReasonBackfillPaused))
		}
	}

	latest, samples := normalizedSamples(input.Snapshot.Metrics.RuntimeSamples)
	for _, sample := range samples {
		if sample.CapturedAtMS < 0 || sample.CapturedAtMS > input.EvaluatedAtMS {
			return Result{}, errors.New("runtime health sample time is invalid")
		}
	}
	if latest != nil && input.EvaluatedAtMS-latest.CapturedAtMS <= evaluator.thresholds.MaxSampleGapMS {
		if liveQueueEligible && latest.OldestLiveWaitMS > evaluator.thresholds.LiveWaitMS {
			apply(managedRules[1])
		}
		progress := input.Snapshot.Metrics.Scheduler.LastBackfillProgressAtMS
		if backfillEligible && latest.BackfillQueueDepth > 0 && latest.OldestBackfillWaitMS > evaluator.thresholds.BackfillWaitMS &&
			(progress == nil || input.EvaluatedAtMS-*progress > evaluator.thresholds.BackfillWaitMS) {
			apply(managedRules[2])
		}
		if latest.DiskFreeBytes < evaluator.thresholds.DiskFreeBytes {
			apply(managedRules[4])
		}
	} else {
		apply(managedRules[9])
		statuses[componentIndex(ComponentRuntime)].Evidence = EvidenceUnknown
	}
	hasAuthFailure := hasFailureCode(input.Snapshot.Metrics.Sources.CurrentFailureCodes, store.SourceFailureAuthRequired)
	if hasAuthFailure {
		apply(managedRules[3])
	} else if input.Snapshot.Metrics.Sources.MaxConsecutiveFailures >= evaluator.thresholds.SourceDegradedFailures {
		applyAtLevel(managedRules[10], LevelDegraded)
	} else if input.Snapshot.Metrics.Sources.MaxConsecutiveFailures >= evaluator.thresholds.SourceWarningFailures {
		applyAtLevel(managedRules[10], LevelBusy)
	}
	if input.Snapshot.Metrics.Jobs.Running > 0 {
		if sustained(samples, input.EvaluatedAtMS, evaluator.thresholds, func(sample store.AppRuntimeSample) bool {
			return sample.CPUPercent > evaluator.thresholds.CPUPercent
		}) {
			apply(managedRules[5])
		}
		if sustained(samples, input.EvaluatedAtMS, evaluator.thresholds, func(sample store.AppRuntimeSample) bool {
			return sample.RSSBytes > evaluator.thresholds.RSSBytes
		}) {
			apply(managedRules[6])
		}
	}
	switch input.Updater {
	case UpdaterNotConfigured:
		index := componentIndex(ComponentUpdater)
		if statuses[index].Level == LevelHealthy && statuses[index].Reason == ReasonHealthy {
			statuses[index] = ComponentStatus{Component: ComponentUpdater, Level: LevelHealthy,
				Evidence: EvidenceNotConfigured, Reason: ReasonNotConfigured, Impact: ImpactNone,
				Protection: ProtectionNone, RecoveryAction: RecoveryNone}
		}
	case UpdaterUnavailable:
		apply(managedRules[7])
	case UpdaterUnknown:
		apply(managedRules[8])
	}

	result := Result{Level: LevelHealthy, Components: statuses, EventBatch: store.HealthEvaluationBatch{
		Observations: observations, ManagedEvents: managedEvents(), EvaluatedAtMS: input.EvaluatedAtMS,
	}}
	for index := range result.Components {
		status := result.Components[index]
		if levelRank(status.Level) > levelRank(result.Level) {
			result.Level = status.Level
			copyOfStatus := status
			result.Primary = &copyOfStatus
		}
	}
	return result, nil
}

func normalizedSamples(values []store.AppRuntimeSample) (*store.AppRuntimeSample, []store.AppRuntimeSample) {
	if len(values) == 0 {
		return nil, nil
	}
	samples := append([]store.AppRuntimeSample(nil), values...)
	sort.Slice(samples, func(left, right int) bool { return samples[left].CapturedAtMS < samples[right].CapturedAtMS })
	return &samples[len(samples)-1], samples
}

func sustained(samples []store.AppRuntimeSample, evaluatedAtMS int64, thresholds Thresholds,
	matches func(store.AppRuntimeSample) bool,
) bool {
	if len(samples) == 0 || evaluatedAtMS-samples[len(samples)-1].CapturedAtMS > thresholds.MaxSampleGapMS {
		return false
	}
	windowStart := evaluatedAtMS - thresholds.SustainedMS
	previous := evaluatedAtMS
	for index := len(samples) - 1; index >= 0; index-- {
		sample := samples[index]
		if previous-sample.CapturedAtMS > thresholds.MaxSampleGapMS || !matches(sample) {
			return false
		}
		previous = sample.CapturedAtMS
		if sample.CapturedAtMS <= windowStart {
			return true
		}
	}
	return false
}

func levelForSeverity(value store.HealthSeverity) (Level, bool) {
	switch value {
	case store.HealthInfo:
		return LevelHealthy, true
	case store.HealthWarning:
		return LevelBusy, true
	case store.HealthError:
		return LevelDegraded, true
	case store.HealthCritical:
		return LevelBlocked, true
	default:
		return "", false
	}
}

func hasFailureCode(values []store.SourceFailureCodeMetric, target store.SourceFailureCode) bool {
	for _, value := range values {
		if value.FailureCode == target && value.Count > 0 {
			return true
		}
	}
	return false
}

func pausedStatus(component Component, reason Reason) ComponentStatus {
	return ComponentStatus{Component: component, Level: LevelPaused, Evidence: EvidenceKnown,
		Reason: reason, Impact: ImpactIndexingPaused, Protection: ProtectionUserPauseRetained,
		RecoveryAction: RecoveryNone}
}

func busyStatus(component Component, reason Reason) ComponentStatus {
	return ComponentStatus{Component: component, Level: LevelBusy, Evidence: EvidenceKnown,
		Reason: reason, Impact: ImpactNone, Protection: ProtectionObservationOnly,
		RecoveryAction: RecoveryNone}
}

func validLifecycle(value store.SchedulerLifecycle, evaluatedAtMS int64) bool {
	if value.UpdatedAtMS < 0 || value.UpdatedAtMS > evaluatedAtMS {
		return false
	}
	if value.UserPauseScope != store.LifecyclePauseNone &&
		value.UserPauseScope != store.LifecyclePauseBackfill &&
		value.UserPauseScope != store.LifecyclePauseAll {
		return false
	}
	if value.SystemState != store.LifecycleSystemAwake && value.SystemState != store.LifecycleSystemSleeping {
		return false
	}
	switch value.Transition {
	case store.LifecycleTransitionSteady, store.LifecycleTransitionDraining,
		store.LifecycleTransitionReconciling, store.LifecycleTransitionBlocked:
	default:
		return false
	}
	switch value.SourceState {
	case store.LifecycleSourceUnknown, store.LifecycleSourceAvailable, store.LifecycleSourceUnavailable:
		return true
	default:
		return false
	}
}

func managedEvents() []store.HealthManagedEvent {
	descriptors := make([]store.HealthManagedEvent, 0, len(managedRules))
	for _, value := range managedRules {
		identity := eventIdentity(value)
		descriptors = append(descriptors, store.HealthManagedEvent{
			EventID: identity, Fingerprint: store.SHA256DigestOf([]byte(identity)),
			Domain: value.domain, Code: value.code,
		})
	}
	return descriptors
}

func eventIdentity(value rule) string {
	return store.HealthEvaluatorEventIDPrefix + strings.ReplaceAll(string(value.component)+"-"+string(value.reason)+"-"+string(value.code), ".", "-")
}

// ManagedEvents 返回 evaluator 唯一允许排除和解析的完整事件描述集合。
func (evaluator *Evaluator) ManagedEvents() []store.HealthManagedEvent {
	if evaluator == nil {
		return nil
	}
	return managedEvents()
}

func componentIndex(target Component) int {
	for index, component := range componentOrder {
		if component == target {
			return index
		}
	}
	panic("unknown health component")
}

func levelRank(value Level) int {
	switch value {
	case LevelBlocked:
		return 5
	case LevelPaused:
		return 4
	case LevelDegraded:
		return 3
	case LevelBusy:
		return 2
	default:
		return 1
	}
}

func severityForLevel(value Level) store.HealthSeverity {
	switch value {
	case LevelBlocked:
		return store.HealthCritical
	case LevelDegraded:
		return store.HealthError
	default:
		return store.HealthWarning
	}
}
