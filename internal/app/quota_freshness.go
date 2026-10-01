package app

import (
	"context"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/diagnostics"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

func (runtime *applicationQuotaRuntime) refreshCommitted() {
	select {
	case runtime.freshnessWake <- struct{}{}:
	default:
	}
	runtime.mu.Lock()
	recovered := runtime.recoveryPending && runtime.runnerErr == nil && runtime.accepting
	if recovered {
		runtime.recoveryPending = false
	}
	runtime.mu.Unlock()
	if recovered {
		diagnostics.Emit(runtime.rootContext, diagnostics.Event{Stage: "runner", Outcome: "recovered", Trigger: "recovery"})
		notifyQueryInvalidation(runtime.invalidation, runtime.rootContext, core.InvalidationHealth)
	}
}

// freshness 与刷新进程的生命周期分离；只在已知时间边界或新记录提交时读取，永不请求上游。
func (runtime *applicationQuotaRuntime) observeFreshness(repository *store.Repository, clock func() time.Time) {
	defer close(runtime.freshnessDone)
	if clock == nil {
		clock = time.Now
	}
	if runtime.invalidation == nil {
		<-runtime.rootContext.Done()
		return
	}
	for {
		now := clock()
		readCtx, cancel := context.WithTimeout(runtime.rootContext, quotaRuntimeRecordTimeout)
		snapshot, err := repository.QuotaCurrentSnapshot(readCtx, store.QuotaAccountScopeDefault, now.UnixMilli())
		cancel()
		var timer *time.Timer
		var deadline <-chan time.Time
		if err == nil {
			if next := quotaFreshnessDeadline(snapshot, now.UnixMilli()); next != nil {
				timer = time.NewTimer(time.Duration(*next-now.UnixMilli()) * time.Millisecond)
				deadline = timer.C
			}
		}
		select {
		case <-runtime.rootContext.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-runtime.freshnessWake:
			if timer != nil {
				timer.Stop()
			}
		case <-deadline:
			notifyQueryInvalidation(runtime.invalidation, runtime.rootContext, core.InvalidationQuotaCodex)
		}
	}
}

func quotaFreshnessDeadline(snapshot store.QuotaCurrentSnapshot, nowMS int64) *int64 {
	var next *int64
	consider := func(at *int64) {
		if at != nil && *at > nowMS && (next == nil || *at < *next) {
			next = new(*at)
		}
	}
	for _, window := range snapshot.Windows {
		current := window.Current
		if current.FreshnessState == store.QuotaCurrentFresh && current.FreshUntilMS != nil {
			// fresh 状态包含截止毫秒，下一毫秒才变 stale。
			consider(new(*current.FreshUntilMS + 1))
		}
		consider(current.ResetsAtMS)
	}
	consider(snapshot.ResetCredits.NextExpiresAtMS)
	return next
}
