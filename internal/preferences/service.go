package preferences

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"math"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
)

var (
	ErrInvalidService     = errors.New("invalid preferences service")
	ErrHomeAlreadyActive  = errors.New("Codex Home is already active")
	ErrHomeUnavailable    = errors.New("Codex Home is unavailable")
	ErrSwitchPlanNotFound = errors.New("Home switch plan not found")
	ErrSwitchPlanStale    = errors.New("Home switch plan is stale")
	ErrSwitchRecovery     = errors.New("Home switch requires recovery")
)

type BootstrapStatus string

const (
	BootstrapStatusNotStarted         BootstrapStatus = "not_started"
	BootstrapStatusQueued             BootstrapStatus = "queued"
	BootstrapStatusRunning            BootstrapStatus = "running"
	BootstrapStatusSucceeded          BootstrapStatus = "succeeded"
	BootstrapStatusFailedSafeRollback BootstrapStatus = "failed_safe_to_rollback"
	BootstrapStatusFailedNeedsResume  BootstrapStatus = "failed_requires_resume"
)

type PreferencesStore interface {
	LoadPreferences(ctx context.Context) (Snapshot, error)
	CompareAndSwap(ctx context.Context, expectedRevision uint64, next Snapshot) error
	AcquireSwitchLease(ctx context.Context) (SwitchExecutionLease, error)
}

type SwitchExecutionLease interface {
	Release()
}

type HomeProbe interface {
	Probe(ctx context.Context, path string) (logs.HomeMetadata, error)
}

// HomeRuntime 由 TOO-259/260 实现。Drain 只有在旧 generation 不再存在已接纳或执行中 writer
// 后才能返回；StartBootstrap 必须对精确的 (switch ID, generation) 持久幂等；Resume 也必须幂等。
type HomeRuntime interface {
	Drain(ctx context.Context, generation uint64) error
	StartBootstrap(ctx context.Context, request BootstrapRequest) error
	BootstrapStatus(ctx context.Context, switchID string, generation uint64) (BootstrapStatus, error)
	Resume(ctx context.Context, generation uint64) error
}

type ServiceConfig struct {
	Store        PreferencesStore
	Probe        HomeProbe
	Runtime      HomeRuntime
	Clock        func() time.Time
	NewAttemptID func() (string, error)
}

type Service struct {
	store        PreferencesStore
	probe        HomeProbe
	runtime      HomeRuntime
	clock        func() time.Time
	newAttemptID func() (string, error)

	mu       sync.Mutex
	lastPlan *SwitchPlan
}

type SettingsUpdate struct {
	ExpectedRevision uint64
	Online           OnlinePreferences
	Refresh          RefreshPreferences
	Updates          UpdatePreferences
	UI               UIPreferences
}

type SwitchImpact struct {
	PreservesOldFacts  bool
	ClearsDerivedFacts bool
	SummaryZH          string
}

type SwitchPlan struct {
	ID             string
	SourceRevision uint64
	From           CodexHomePreferences
	Target         CodexHomePreferences
	Strategy       HomeSwitchStrategy
	Impact         SwitchImpact
}

type BootstrapRequest struct {
	SwitchID     string
	Generation   uint64
	Source       ConfirmedSource
	DataStoreKey string
	Strategy     HomeSwitchStrategy
}

func NewService(config ServiceConfig) (*Service, error) {
	if config.Store == nil || config.Probe == nil || config.Runtime == nil {
		return nil, ErrInvalidService
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.NewAttemptID == nil {
		config.NewAttemptID = randomAttemptID
	}
	return &Service{
		store: config.Store, probe: config.Probe, runtime: config.Runtime,
		clock: config.Clock, newAttemptID: config.NewAttemptID,
	}, nil
}

func (service *Service) UpdateSettings(ctx context.Context, request SettingsUpdate) (Snapshot, error) {
	if service == nil {
		return Snapshot{}, ErrInvalidService
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	current, err := service.store.LoadPreferences(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if current.PendingSwitch != nil || current.PendingResume != nil {
		return cloneSnapshot(current), ErrSwitchRecovery
	}
	if settingsEqual(current, request) {
		return cloneSnapshot(current), nil
	}
	if current.Revision != request.ExpectedRevision {
		return cloneSnapshot(current), ErrPreferencesConflict
	}
	revision, err := nextRevision(current.Revision)
	if err != nil {
		return Snapshot{}, err
	}
	next := cloneSnapshot(current)
	next.Revision = revision
	next.Online = request.Online
	next.Refresh = request.Refresh
	next.Updates = cloneUpdatePreferences(request.Updates)
	next.UI = request.UI
	if err := validatePreferences(next); err != nil {
		return cloneSnapshot(current), err
	}
	if err := service.store.CompareAndSwap(ctx, current.Revision, next); err != nil {
		return service.readbackSettings(ctx, current, next, err)
	}
	return cloneSnapshot(next), nil
}

func (service *Service) readbackSettings(
	ctx context.Context,
	current Snapshot,
	next Snapshot,
	original error,
) (Snapshot, error) {
	recoveryCtx, cancel := service.recoveryContext(ctx)
	defer cancel()
	readback, loadErr := service.store.LoadPreferences(recoveryCtx)
	if loadErr != nil {
		return cloneSnapshot(current), errors.Join(ErrDurabilityUnknown, original, loadErr)
	}
	if reflect.DeepEqual(readback, next) {
		return cloneSnapshot(readback), nil
	}
	if reflect.DeepEqual(readback, current) {
		return cloneSnapshot(readback), original
	}
	return cloneSnapshot(readback), errors.Join(ErrPreferencesConflict, original)
}

func (service *Service) PlanSwitch(
	ctx context.Context,
	targetPath string,
	strategy HomeSwitchStrategy,
) (SwitchPlan, error) {
	if service == nil || !validHomeSwitchStrategy(strategy) {
		return SwitchPlan{}, ErrInvalidService
	}
	if err := ctx.Err(); err != nil {
		return SwitchPlan{}, err
	}
	if !filepath.IsAbs(targetPath) || filepath.Clean(targetPath) != targetPath {
		return SwitchPlan{}, ErrHomeUnavailable
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	current, err := service.store.LoadPreferences(ctx)
	if err != nil {
		return SwitchPlan{}, err
	}
	if current.PendingSwitch != nil || current.PendingResume != nil {
		return SwitchPlan{}, ErrSwitchRecovery
	}
	if current.CodexHome.Generation == math.MaxUint64 {
		return SwitchPlan{}, ErrInvalidPreferences
	}
	metadata, err := service.probe.Probe(ctx, targetPath)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return SwitchPlan{}, err
		}
		return SwitchPlan{}, ErrHomeUnavailable
	}
	if !validHomeMetadata(metadata) {
		return SwitchPlan{}, ErrHomeUnavailable
	}
	targetSource := ConfirmedSource{
		Path: metadata.Path, DeviceID: metadata.DeviceID, Inode: metadata.Inode,
		ConfirmedAtMS: service.clock().UnixMilli(),
	}
	if validateConfirmedSource(targetSource) != nil {
		return SwitchPlan{}, ErrHomeUnavailable
	}
	if sameSourceIdentity(current.CodexHome.Source, targetSource) {
		return SwitchPlan{}, ErrHomeAlreadyActive
	}
	id := switchPlanID(current, targetSource, strategy)
	target := CodexHomePreferences{
		Source: targetSource, Generation: current.CodexHome.Generation + 1,
		DataStoreKey: current.CodexHome.DataStoreKey,
	}
	if strategy == HomeSwitchIndependentDatabase {
		target.DataStoreKey = detachedDataStoreKey(current.DetachedHomes, targetSource)
		if target.DataStoreKey == "" {
			target.DataStoreKey = "home-" + id[len("home-switch:"):]
		}
	}
	plan := SwitchPlan{
		ID: id, SourceRevision: current.Revision, From: current.CodexHome,
		Target: target, Strategy: strategy, Impact: switchImpact(strategy),
	}
	service.lastPlan = &plan
	return plan, nil
}

func (service *Service) ConfirmSwitch(ctx context.Context, planID string) (Snapshot, error) {
	if service == nil {
		return Snapshot{}, ErrInvalidService
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.lastPlan == nil || service.lastPlan.ID != planID {
		return Snapshot{}, ErrSwitchPlanNotFound
	}
	lease, err := service.store.AcquireSwitchLease(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if lease == nil {
		return Snapshot{}, ErrInvalidService
	}
	defer lease.Release()
	plan := *service.lastPlan
	current, err := service.store.LoadPreferences(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if current.PendingSwitch != nil || current.PendingResume != nil {
		return cloneSnapshot(current), ErrSwitchRecovery
	}
	if current.Revision != plan.SourceRevision || current.CodexHome != plan.From {
		if resolvedSwitchMatches(current, plan.Target, plan.ID, HomeSwitchCompleted) {
			return cloneSnapshot(current), nil
		}
		return cloneSnapshot(current), ErrSwitchPlanStale
	}
	metadata, err := service.probe.Probe(ctx, plan.Target.Source.Path)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Snapshot{}, err
		}
		return cloneSnapshot(current), ErrHomeUnavailable
	}
	if !metadataMatchesSource(metadata, plan.Target.Source) {
		return cloneSnapshot(current), ErrSwitchPlanStale
	}
	guard, err := service.resumeGuardSnapshot(current, plan)
	if err != nil {
		return cloneSnapshot(current), err
	}
	if err := service.store.CompareAndSwap(ctx, current.Revision, guard); err != nil {
		visible, committed, readable := service.readbackResumeGuard(ctx, guard)
		if !readable {
			return cloneSnapshot(current), errors.Join(ErrSwitchRecovery, err)
		}
		if !committed {
			if reflect.DeepEqual(visible, current) {
				return cloneSnapshot(visible), err
			}
			if resolvedSwitchMatches(visible, plan.Target, plan.ID, HomeSwitchCompleted) {
				return cloneSnapshot(visible), nil
			}
			return cloneSnapshot(visible), errors.Join(ErrSwitchRecovery, err)
		}
		guard = visible
	}

	if err := service.runtime.Drain(ctx, current.CodexHome.Generation); err != nil {
		drainErr := fmt.Errorf("drain Home generation %d: %w", current.CodexHome.Generation, err)
		resumed, resumeErr := service.completePendingResume(ctx, guard)
		return resumed, errors.Join(drainErr, resumeErr)
	}

	pending, err := service.pendingSnapshot(guard, plan)
	if err != nil {
		resumed, resumeErr := service.completePendingResume(ctx, guard)
		return resumed, errors.Join(err, resumeErr)
	}
	if err := service.store.CompareAndSwap(ctx, guard.Revision, pending); err != nil {
		visible, committed, readable := service.readbackPending(ctx, pending)
		if !readable {
			return cloneSnapshot(guard), errors.Join(ErrSwitchRecovery, err)
		}
		if !committed {
			if resumeGuardMatches(visible, guard.PendingResume) {
				resumed, resumeErr := service.completePendingResume(ctx, visible)
				return resumed, errors.Join(err, resumeErr)
			}
			if resumeCompletedMatches(visible, plan.ID) {
				return cloneSnapshot(visible), err
			}
			if resolvedSwitchMatches(visible, pending.CodexHome, plan.ID, HomeSwitchCompleted) {
				return cloneSnapshot(visible), nil
			}
			return cloneSnapshot(visible), errors.Join(ErrSwitchRecovery, err)
		}
		pending = visible
	}

	request := BootstrapRequest{
		SwitchID: plan.ID, Generation: pending.CodexHome.Generation,
		Source: pending.CodexHome.Source, DataStoreKey: pending.CodexHome.DataStoreKey, Strategy: plan.Strategy,
	}
	if err := service.runtime.StartBootstrap(ctx, request); err != nil {
		startErr := fmt.Errorf("start Home bootstrap generation %d: %w", request.Generation, err)
		return service.resolvePending(ctx, pending, startErr)
	}
	return service.finalizePending(ctx, pending, HomeSwitchCompleted)
}

func (service *Service) RecoverSwitch(ctx context.Context) (Snapshot, error) {
	if service == nil {
		return Snapshot{}, ErrInvalidService
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	lease, err := service.store.AcquireSwitchLease(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if lease == nil {
		return Snapshot{}, ErrInvalidService
	}
	defer lease.Release()
	current, err := service.store.LoadPreferences(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if current.PendingResume != nil {
		return service.completePendingResume(ctx, current)
	}
	if current.PendingSwitch == nil {
		return cloneSnapshot(current), nil
	}
	return service.resolvePending(ctx, current, nil)
}

func (service *Service) resumeGuardSnapshot(current Snapshot, plan SwitchPlan) (Snapshot, error) {
	attemptID, err := service.newAttemptID()
	if err != nil {
		return Snapshot{}, fmt.Errorf("create Home switch attempt ID: %w", err)
	}
	revision, err := nextRevision(current.Revision)
	if err != nil {
		return Snapshot{}, err
	}
	next := cloneSnapshot(current)
	next.Revision = revision
	next.PendingResume = &HomeResumeJournal{
		SwitchID: plan.ID, AttemptID: attemptID, Generation: current.CodexHome.Generation,
		TargetGeneration: plan.Target.Generation, Strategy: plan.Strategy,
		StartedAtMS: service.clock().UnixMilli(),
	}
	if err := validatePreferences(next); err != nil {
		return Snapshot{}, err
	}
	return next, nil
}

func (service *Service) pendingSnapshot(current Snapshot, plan SwitchPlan) (Snapshot, error) {
	if current.PendingResume == nil || current.PendingResume.SwitchID != plan.ID ||
		current.PendingResume.Generation != current.CodexHome.Generation ||
		current.PendingResume.TargetGeneration != plan.Target.Generation ||
		current.PendingResume.Strategy != plan.Strategy {
		return Snapshot{}, ErrInvalidPreferences
	}
	revision, err := nextRevision(current.Revision)
	if err != nil {
		return Snapshot{}, err
	}
	next := cloneSnapshot(current)
	next.Revision = revision
	next.CodexHome = plan.Target
	next.PendingResume = nil
	next.PendingSwitch = &HomeSwitchJournal{
		SwitchID: plan.ID, AttemptID: current.PendingResume.AttemptID,
		Previous: current.CodexHome, Target: plan.Target,
		Strategy: plan.Strategy, StartedAtMS: service.clock().UnixMilli(),
	}
	if err := validatePreferences(next); err != nil {
		return Snapshot{}, err
	}
	return next, nil
}

func (service *Service) resolvePending(
	ctx context.Context,
	current Snapshot,
	original error,
) (Snapshot, error) {
	journal := current.PendingSwitch
	if journal == nil {
		return cloneSnapshot(current), original
	}
	recoveryCtx, cancel := service.recoveryContext(ctx)
	defer cancel()
	status, err := service.runtime.BootstrapStatus(recoveryCtx, journal.SwitchID, journal.Target.Generation)
	if err != nil {
		statusErr := fmt.Errorf("read Home bootstrap status generation %d: %w", journal.Target.Generation, err)
		return cloneSnapshot(current), errors.Join(ErrSwitchRecovery, original, statusErr)
	}
	switch status {
	case BootstrapStatusNotStarted, BootstrapStatusFailedSafeRollback:
		rolledBack, rollbackErr := service.rollbackPending(recoveryCtx, current)
		if rollbackErr != nil {
			return rolledBack, errors.Join(ErrSwitchRecovery, original, rollbackErr)
		}
		resumed, resumeErr := service.completePendingResume(recoveryCtx, rolledBack)
		if resumeErr != nil {
			return resumed, errors.Join(ErrSwitchRecovery, original, resumeErr)
		}
		return resumed, original
	case BootstrapStatusQueued, BootstrapStatusRunning, BootstrapStatusSucceeded, BootstrapStatusFailedNeedsResume:
		return service.finalizePending(recoveryCtx, current, HomeSwitchCompleted)
	default:
		return cloneSnapshot(current), errors.Join(ErrSwitchRecovery, original)
	}
}

func (service *Service) rollbackPending(ctx context.Context, current Snapshot) (Snapshot, error) {
	journal := current.PendingSwitch
	if journal == nil {
		return cloneSnapshot(current), nil
	}
	revision, err := nextRevision(current.Revision)
	if err != nil {
		return cloneSnapshot(current), err
	}
	next := cloneSnapshot(current)
	next.Revision = revision
	next.CodexHome = journal.Previous
	next.PendingSwitch = nil
	next.PendingResume = &HomeResumeJournal{
		SwitchID: journal.SwitchID, AttemptID: journal.AttemptID, Generation: journal.Previous.Generation,
		TargetGeneration: journal.Target.Generation, Strategy: journal.Strategy,
		StartedAtMS: journal.StartedAtMS,
	}
	return service.persistResumeMarker(ctx, current, next)
}

func (service *Service) completePendingResume(ctx context.Context, current Snapshot) (Snapshot, error) {
	journal := current.PendingResume
	if journal == nil {
		return cloneSnapshot(current), nil
	}
	recoveryCtx, cancel := service.recoveryContext(ctx)
	defer cancel()
	if err := service.runtime.Resume(recoveryCtx, journal.Generation); err != nil {
		wrapped := fmt.Errorf("resume Home generation %d: %w", journal.Generation, err)
		return cloneSnapshot(current), errors.Join(ErrSwitchRecovery, wrapped)
	}
	revision, err := nextRevision(current.Revision)
	if err != nil {
		return cloneSnapshot(current), errors.Join(ErrSwitchRecovery, err)
	}
	next := cloneSnapshot(current)
	next.Revision = revision
	next.PendingResume = nil
	next.LastSwitch = &HomeSwitchAudit{
		SwitchID: journal.SwitchID, FromGeneration: journal.Generation,
		ToGeneration: journal.TargetGeneration, Strategy: journal.Strategy,
		Outcome: HomeSwitchRolledBack, FinishedAtMS: service.clock().UnixMilli(),
	}
	return service.persistResolution(recoveryCtx, current, next)
}

func (service *Service) persistResumeMarker(ctx context.Context, current, next Snapshot) (Snapshot, error) {
	if next.PendingResume == nil || next.PendingSwitch != nil {
		return cloneSnapshot(current), ErrInvalidPreferences
	}
	if err := validatePreferences(next); err != nil {
		return cloneSnapshot(current), err
	}
	if err := service.store.CompareAndSwap(ctx, current.Revision, next); err == nil {
		return cloneSnapshot(next), nil
	} else {
		recoveryCtx, cancel := service.recoveryContext(ctx)
		defer cancel()
		readback, loadErr := service.store.LoadPreferences(recoveryCtx)
		if loadErr == nil && (reflect.DeepEqual(readback, next) ||
			resumeCompletedMatches(readback, next.PendingResume.SwitchID)) {
			return cloneSnapshot(readback), nil
		}
		return cloneSnapshot(readback), errors.Join(ErrSwitchRecovery, err, loadErr)
	}
}

func (service *Service) finalizePending(
	ctx context.Context,
	current Snapshot,
	outcome HomeSwitchOutcome,
) (Snapshot, error) {
	journal := current.PendingSwitch
	if journal == nil {
		return cloneSnapshot(current), nil
	}
	revision, err := nextRevision(current.Revision)
	if err != nil {
		return cloneSnapshot(current), err
	}
	next := cloneSnapshot(current)
	next.Revision = revision
	next.CodexHome = journal.Target
	next.PendingSwitch = nil
	if journal.Strategy == HomeSwitchIndependentDatabase {
		next.DetachedHomes = detachedWithoutDataStore(next.DetachedHomes, journal.Target.DataStoreKey)
		next.DetachedHomes = append(next.DetachedHomes, journal.Previous)
	}
	next.LastSwitch = &HomeSwitchAudit{
		SwitchID: journal.SwitchID, FromGeneration: journal.Previous.Generation,
		ToGeneration: journal.Target.Generation, Strategy: journal.Strategy,
		Outcome: outcome, FinishedAtMS: service.clock().UnixMilli(),
	}
	return service.persistResolution(ctx, current, next)
}

func (service *Service) persistResolution(ctx context.Context, current, next Snapshot) (Snapshot, error) {
	if next.LastSwitch == nil {
		return cloneSnapshot(current), ErrInvalidPreferences
	}
	if err := validatePreferences(next); err != nil {
		return cloneSnapshot(current), err
	}
	if err := service.store.CompareAndSwap(ctx, current.Revision, next); err == nil {
		return cloneSnapshot(next), nil
	} else {
		recoveryCtx, cancel := service.recoveryContext(ctx)
		defer cancel()
		readback, loadErr := service.store.LoadPreferences(recoveryCtx)
		if loadErr == nil && reflect.DeepEqual(readback.DetachedHomes, next.DetachedHomes) &&
			resolvedSwitchMatches(readback, next.CodexHome, next.LastSwitch.SwitchID, next.LastSwitch.Outcome) {
			return cloneSnapshot(readback), nil
		}
		return cloneSnapshot(readback), errors.Join(ErrSwitchRecovery, err, loadErr)
	}
}

func (service *Service) readbackResumeGuard(ctx context.Context, want Snapshot) (Snapshot, bool, bool) {
	recoveryCtx, cancel := service.recoveryContext(ctx)
	defer cancel()
	value, err := service.store.LoadPreferences(recoveryCtx)
	if err != nil {
		return Snapshot{}, false, false
	}
	return value, resumeGuardMatches(value, want.PendingResume), true
}

func (service *Service) readbackPending(ctx context.Context, want Snapshot) (Snapshot, bool, bool) {
	recoveryCtx, cancel := service.recoveryContext(ctx)
	defer cancel()
	value, err := service.store.LoadPreferences(recoveryCtx)
	if err != nil {
		return Snapshot{}, false, false
	}
	return value, value.PendingSwitch != nil && want.PendingSwitch != nil &&
		*value.PendingSwitch == *want.PendingSwitch && value.CodexHome == want.CodexHome, true
}

func (service *Service) recoveryContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
}

func settingsEqual(current Snapshot, request SettingsUpdate) bool {
	return current.Online == request.Online && current.Refresh == request.Refresh &&
		reflect.DeepEqual(current.Updates, request.Updates) && current.UI == request.UI
}

func switchImpact(strategy HomeSwitchStrategy) SwitchImpact {
	if strategy == HomeSwitchIndependentDatabase {
		return SwitchImpact{
			PreservesOldFacts: true,
			SummaryZH:         "新 Codex Home 使用独立数据库；旧数据库和可审计事实保留，但不会同时激活两个 Home。",
		}
	}
	return SwitchImpact{
		ClearsDerivedFacts: true,
		SummaryZH:          "当前派生索引仅在新 generation bootstrap 明确执行清理后重建；Codex 原始文件不会被删除。",
	}
}

func detachedDataStoreKey(values []CodexHomePreferences, source ConfirmedSource) string {
	for _, value := range values {
		if sameSourceIdentity(value.Source, source) {
			return value.DataStoreKey
		}
	}
	return ""
}

func detachedWithoutDataStore(values []CodexHomePreferences, dataStoreKey string) []CodexHomePreferences {
	result := make([]CodexHomePreferences, 0, len(values))
	for _, value := range values {
		if value.DataStoreKey != dataStoreKey {
			result = append(result, value)
		}
	}
	return result
}

func resolvedSwitchMatches(
	value Snapshot,
	home CodexHomePreferences,
	switchID string,
	outcome HomeSwitchOutcome,
) bool {
	return value.PendingSwitch == nil && value.PendingResume == nil && value.CodexHome == home && value.LastSwitch != nil &&
		value.LastSwitch.SwitchID == switchID && value.LastSwitch.Outcome == outcome
}

func resumeGuardMatches(value Snapshot, want *HomeResumeJournal) bool {
	return want != nil && value.PendingSwitch == nil && value.PendingResume != nil &&
		*value.PendingResume == *want && value.CodexHome.Generation == want.Generation
}

func resumeCompletedMatches(value Snapshot, switchID string) bool {
	return value.PendingSwitch == nil && value.PendingResume == nil && value.LastSwitch != nil &&
		value.LastSwitch.SwitchID == switchID && value.LastSwitch.Outcome == HomeSwitchRolledBack
}

func randomAttemptID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func switchPlanID(current Snapshot, target ConfirmedSource, strategy HomeSwitchStrategy) string {
	hasher := sha256.New()
	writeSwitchUint64(hasher, current.Revision)
	writeSwitchUint64(hasher, current.CodexHome.Generation)
	writeSwitchString(hasher, current.CodexHome.DataStoreKey)
	writeSwitchString(hasher, target.Path)
	writeSwitchString(hasher, target.DeviceID)
	writeSwitchUint64(hasher, uint64(target.Inode))
	writeSwitchString(hasher, string(strategy))
	return "home-switch:" + hex.EncodeToString(hasher.Sum(nil))
}

func writeSwitchString(hasher hash.Hash, value string) {
	writeSwitchUint64(hasher, uint64(len(value)))
	_, _ = hasher.Write([]byte(value))
}

func writeSwitchUint64(hasher hash.Hash, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = hasher.Write(encoded[:])
}

func validHomeMetadata(value logs.HomeMetadata) bool {
	return filepath.IsAbs(value.Path) && filepath.Clean(value.Path) == value.Path &&
		value.DeviceID != "" && value.Inode > 0
}

func metadataMatchesSource(metadata logs.HomeMetadata, source ConfirmedSource) bool {
	return validHomeMetadata(metadata) && metadata.Path == source.Path &&
		metadata.DeviceID == source.DeviceID && metadata.Inode == source.Inode
}

func cloneSnapshot(value Snapshot) Snapshot {
	value.Updates = cloneUpdatePreferences(value.Updates)
	value.DetachedHomes = append([]CodexHomePreferences(nil), value.DetachedHomes...)
	if value.PendingSwitch != nil {
		journal := *value.PendingSwitch
		value.PendingSwitch = &journal
	}
	if value.PendingResume != nil {
		journal := *value.PendingResume
		value.PendingResume = &journal
	}
	if value.LastSwitch != nil {
		audit := *value.LastSwitch
		value.LastSwitch = &audit
	}
	return value
}

func cloneUpdatePreferences(value UpdatePreferences) UpdatePreferences {
	value.SkippedVersion = cloneString(value.SkippedVersion)
	value.SnoozeUntilMS = cloneInt64(value.SnoozeUntilMS)
	value.LastCheckAtMS = cloneInt64(value.LastCheckAtMS)
	return value
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
