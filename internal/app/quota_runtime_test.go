package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/bootstrap"
	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/core"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const quotaRuntimeNowMS = int64(1_784_000_000_000)

func (runtime *applicationLifecycleRuntime) reconcileQuotaPreferencesForTest(ctx context.Context) error {
	if runtime == nil || runtime.quota == nil || ctx == nil {
		return ErrApplicationLifecycleRuntime
	}
	operationContext, finish, err := runtime.beginControlAdmission(ctx)
	if err != nil {
		return err
	}
	defer finish()
	return runtime.quota.ReconcilePreferences(operationContext)
}

func TestApplicationQuotaRuntimeStartsEnabledSourcesAndStops(t *testing.T) {
	t.Parallel()

	database, repository := openQuotaRuntimeStore(t)
	home := writeSyntheticAuthHome(t, "synthetic-runtime-access-token")
	initialPreferences := enabledQuotaRuntimePreferences(t, home)
	loader := &quotaRuntimePreferencesLoader{snapshot: initialPreferences}
	requests := make(chan string, 2)

	runtime, err := startApplicationQuotaRuntime(context.Background(), withBoundQuotaRuntime(t, repository, ApplicationQuotaRuntimeConfig{
		Repository:  repository,
		Preferences: loader,
		Reader:      newQuotaRuntimeSuccessReader(requests),
		Clock: func() time.Time {
			return time.UnixMilli(quotaRuntimeNowMS).UTC()
		},
	}))
	if err != nil {
		t.Fatalf("startApplicationQuotaRuntime() error = %v", err)
	}
	if runtime == nil {
		t.Fatal("startApplicationQuotaRuntime() returned nil runtime")
	}

	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case endpoint := <-requests:
			seen[endpoint] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("startup requests = %#v", seen)
		}
	}
	if !seen[quotaRuntimeReadQuota] || !seen[quotaRuntimeReadReset] {
		t.Fatalf("startup requests = %#v", seen)
	}

	waitForQuotaRuntimeState(t, repository, store.QuotaSourceInstanceWhamDefault, func(state store.SourceState) bool {
		return state.LastSuccessAtMS != nil && state.LastFailureCode == nil
	})
	waitForQuotaRuntimeState(t, repository, store.ResetCreditsSourceInstanceWhamDefault, func(state store.SourceState) bool {
		return state.LastSuccessAtMS != nil && state.LastFailureCode == nil
	})
	for _, sourceInstanceID := range quotaRuntimeInstances(t, repository) {
		waitForQuotaRuntimeSchedule(t, repository, sourceInstanceID, func(schedule store.SourceRefreshSchedule) bool {
			return schedule.NextDueAtMS != nil && schedule.ActiveClaimID == nil
		})
	}

	closeContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runtime.Close(closeContext); err != nil {
		t.Fatalf("Close(first) error = %v", err)
	}
	if err := runtime.Close(closeContext); err != nil {
		t.Fatalf("Close(second) error = %v", err)
	}
	restarted, err := startApplicationQuotaRuntime(context.Background(), withBoundQuotaRuntime(t, repository, ApplicationQuotaRuntimeConfig{
		Repository: repository, Preferences: loader,
		Reader: newQuotaRuntimeSuccessReader(nil), Clock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	}))
	if err != nil || restarted == nil {
		t.Fatalf("startApplicationQuotaRuntime(restart) = %#v, %v", restarted, err)
	}
	if err := restarted.ReconcilePreferences(context.Background()); err != nil {
		t.Fatalf("ReconcilePreferences(restart) error = %v", err)
	}
	restartWindow := time.NewTimer(100 * time.Millisecond)
	select {
	case endpoint := <-requests:
		restartWindow.Stop()
		t.Fatalf("restart duplicated durable request to %q", endpoint)
	case <-restartWindow.C:
	}
	if err := restarted.Close(closeContext); err != nil {
		t.Fatalf("Close(restart) error = %v", err)
	}
	if err := database.Close(closeContext); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func TestApplicationQuotaRuntimeMarksMissingIdentityUnavailableAndManualRecovery(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	database, repository := openQuotaRuntimeStore(t)
	home := t.TempDir()
	loader := &quotaRuntimePreferencesLoader{
		snapshot: enabledQuotaRuntimePreferences(t, home),
	}
	calls := make(chan string, 8)
	reader := newQuotaRuntimeSuccessReader(calls)
	reader.missingID.Store(true)
	var nowMS atomic.Int64
	nowMS.Store(quotaRuntimeNowMS)
	config := withBoundQuotaRuntime(t, repository, ApplicationQuotaRuntimeConfig{
		Repository:  repository,
		Preferences: loader,
		Reader:      reader,
		Clock:       func() time.Time { return time.UnixMilli(nowMS.Load()).UTC() },
	})
	before, err := repository.CodexAccountBinding(ctx)
	if err != nil || before.AccountScope == nil {
		t.Fatalf("CodexAccountBinding(before missing identity) = %#v, %v", before, err)
	}
	oldScope := *before.AccountScope
	runtime, err := startApplicationQuotaRuntime(ctx, config)
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationQuotaRuntime() = %#v, %v", runtime, err)
	}

	unavailable := waitForQuotaRuntimeBinding(t, repository, func(binding store.CodexAccountBinding) bool {
		return binding.State == store.CodexAccountBindingIdentityUnavailable
	})
	if unavailable.AccountScope != nil || unavailable.BindingGeneration <= before.BindingGeneration {
		t.Fatalf("CodexAccountBinding(missing identity) = %#v", unavailable)
	}
	for _, sourceInstanceID := range []string{
		store.QuotaSourceInstanceAppServer(oldScope),
		store.ResetCreditsSourceInstanceAppServer(oldScope),
	} {
		if _, err := repository.SourceState(ctx, sourceInstanceID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("SourceState(%q) error = %v, want ErrNotFound", sourceInstanceID, err)
		}
	}
	reader.missingID.Store(false)
	nowMS.Store(quotaRuntimeNowMS + 61_000)
	for _, source := range []quotaonline.RefreshSource{
		quotaonline.RefreshSourceQuota,
		quotaonline.RefreshSourceResetCredits,
	} {
		if _, err := runtime.RequestRefresh(ctx, source, store.RefreshTriggerManual); err != nil {
			t.Fatalf("RequestRefresh(%s) error = %v", source, err)
		}
	}
	waitForQuotaRuntimeState(t, repository, store.QuotaSourceInstanceWhamDefault, func(state store.SourceState) bool {
		return state.LastSuccessAtMS != nil && state.LastFailureCode == nil
	})
	waitForQuotaRuntimeState(t, repository, store.ResetCreditsSourceInstanceWhamDefault, func(state store.SourceState) bool {
		return state.LastSuccessAtMS != nil && state.LastFailureCode == nil
	})
	recovered := waitForQuotaRuntimeBinding(t, repository, func(binding store.CodexAccountBinding) bool {
		return binding.State == store.CodexAccountBindingConfirmed
	})
	if recovered.AccountScope == nil || *recovered.AccountScope != oldScope ||
		recovered.BindingGeneration <= before.BindingGeneration {
		t.Fatalf("CodexAccountBinding(manual recovery) = %#v", recovered)
	}

	closeContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runtime.Close(closeContext); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := database.Close(closeContext); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func TestApplicationQuotaRuntimeCloseCancelsInflightRequestAndClearsAuthorization(t *testing.T) {
	t.Parallel()

	database, repository := openQuotaRuntimeStore(t)
	home := writeSyntheticAuthHome(t, "synthetic-cancel-access-token")
	snapshot := enabledQuotaRuntimePreferences(t, home)
	snapshot.Online.ResetCreditsEnabled = false
	loader := &quotaRuntimePreferencesLoader{snapshot: snapshot}
	started := make(chan struct{}, 1)
	reader := newQuotaRuntimeSuccessReader(nil)
	reader.started = started
	reader.block = make(chan struct{})
	runtime, err := startApplicationQuotaRuntime(context.Background(), withBoundQuotaRuntime(t, repository, ApplicationQuotaRuntimeConfig{
		Repository: repository, Preferences: loader,
		Reader: reader,
		Clock:  func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	}))
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationQuotaRuntime() = %#v, %v", runtime, err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("startup request did not reach App Server reader")
	}

	closeContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runtime.Close(closeContext); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	rejectedContext, rejectCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer rejectCancel()
	if _, err := runtime.RequestRefresh(
		rejectedContext,
		quotaonline.RefreshSourceQuota,
		store.RefreshTriggerManual,
	); !errors.Is(err, ErrApplicationQuotaRuntime) {
		t.Fatalf("RequestRefresh(after Close) error = %v, want ErrApplicationQuotaRuntime", err)
	}
	if err := runtime.ReconcilePreferences(rejectedContext); !errors.Is(err, ErrApplicationQuotaRuntime) {
		t.Fatalf("ReconcilePreferences(after Close) error = %v, want ErrApplicationQuotaRuntime", err)
	}
	state, err := repository.SourceState(context.Background(), quotaRuntimeInstance(t, repository, store.QuotaSourceInstanceWhamDefault))
	if err != nil || state.LastFailureCode == nil || *state.LastFailureCode != store.SourceFailureCancelled {
		t.Fatalf("SourceState(quota) = %#v, %v", state, err)
	}
	if err := database.Close(closeContext); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func TestApplicationQuotaRuntimeReconcilesEnableDisableAndPreservesHistory(t *testing.T) {
	t.Parallel()

	database, repository := openQuotaRuntimeStore(t)
	home := writeSyntheticAuthHome(t, "synthetic-toggle-access-token")
	snapshot := enabledQuotaRuntimePreferences(t, home)
	snapshot.Online = preferences.OnlinePreferences{}
	loader := &quotaRuntimePreferencesLoader{snapshot: snapshot}
	requests := make(chan string, 1)
	runtime, err := startApplicationQuotaRuntime(context.Background(), withBoundQuotaRuntime(t, repository, ApplicationQuotaRuntimeConfig{
		Repository: repository, Preferences: loader,
		Reader: newQuotaRuntimeSuccessReader(requests),
		Clock:  func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	}))
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationQuotaRuntime() = %#v, %v", runtime, err)
	}
	for _, sourceInstanceID := range quotaRuntimeInstances(t, repository) {
		waitForQuotaRuntimeSchedule(t, repository, sourceInstanceID, func(schedule store.SourceRefreshSchedule) bool {
			return schedule.Reason == store.RefreshReasonDisabled && schedule.NextDueAtMS == nil
		})
	}
	for _, sourceInstanceID := range quotaRuntimeInstances(t, repository) {
		schedule, scheduleErr := repository.SourceRefreshSchedule(context.Background(), sourceInstanceID)
		if scheduleErr != nil || schedule.Reason != store.RefreshReasonDisabled || schedule.NextDueAtMS != nil {
			t.Fatalf("SourceRefreshSchedule(%q) = %#v, %v", sourceInstanceID, schedule, scheduleErr)
		}
	}
	select {
	case endpoint := <-requests:
		t.Fatalf("disabled startup called %q", endpoint)
	default:
	}

	enabled := snapshot
	enabled.Revision++
	enabled.Online.QuotaEnabled = true
	loader.setSnapshot(enabled)
	if err := runtime.ReconcilePreferences(context.Background()); err != nil {
		t.Fatalf("ReconcilePreferences(enable quota) error = %v", err)
	}
	select {
	case endpoint := <-requests:
		if endpoint != quotaRuntimeReadQuota {
			t.Fatalf("enabled source endpoint = %q", endpoint)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("newly enabled Quota source did not enter startup due")
	}
	stateBeforeDisable, err := repository.SourceState(context.Background(), quotaRuntimeInstance(t, repository, store.QuotaSourceInstanceWhamDefault))
	if err != nil || stateBeforeDisable.LastSuccessAtMS == nil {
		t.Fatalf("SourceState(before disable) = %#v, %v", stateBeforeDisable, err)
	}
	attemptsBeforeDisable, err := repository.ListSourceAttempts(
		context.Background(), quotaRuntimeInstance(t, repository, store.QuotaSourceInstanceWhamDefault), 10,
	)
	if err != nil || len(attemptsBeforeDisable) != 1 {
		t.Fatalf("ListSourceAttempts(before disable) = %#v, %v", attemptsBeforeDisable, err)
	}

	disabled := enabled
	disabled.Revision++
	disabled.Online.QuotaEnabled = false
	loader.setSnapshot(disabled)
	if err := runtime.ReconcilePreferences(context.Background()); err != nil {
		t.Fatalf("ReconcilePreferences(disable quota) error = %v", err)
	}
	schedule, err := repository.SourceRefreshSchedule(context.Background(), quotaRuntimeInstance(t, repository, store.QuotaSourceInstanceWhamDefault))
	if err != nil || schedule.Reason != store.RefreshReasonDisabled || schedule.NextDueAtMS != nil {
		t.Fatalf("SourceRefreshSchedule(disabled quota) = %#v, %v", schedule, err)
	}
	stateAfterDisable, err := repository.SourceState(context.Background(), quotaRuntimeInstance(t, repository, store.QuotaSourceInstanceWhamDefault))
	if err != nil || stateAfterDisable.LastSuccessAtMS == nil ||
		*stateAfterDisable.LastSuccessAtMS != *stateBeforeDisable.LastSuccessAtMS {
		t.Fatalf("SourceState(after disable) = %#v, %v", stateAfterDisable, err)
	}
	attemptsAfterDisable, err := repository.ListSourceAttempts(
		context.Background(), quotaRuntimeInstance(t, repository, store.QuotaSourceInstanceWhamDefault), 10,
	)
	if err != nil || len(attemptsAfterDisable) != len(attemptsBeforeDisable) {
		t.Fatalf("ListSourceAttempts(after disable) = %#v, %v", attemptsAfterDisable, err)
	}

	closeContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runtime.Close(closeContext); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	if err := database.Close(closeContext); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func TestApplicationQuotaRuntimeCloseSealsAndDrainsAdmittedOperation(t *testing.T) {
	t.Parallel()

	database, repository := openQuotaRuntimeStore(t)
	home := writeSyntheticAuthHome(t, "synthetic-admission-access-token")
	snapshot := enabledQuotaRuntimePreferences(t, home)
	snapshot.Online = preferences.OnlinePreferences{}
	loader := &quotaRuntimePreferencesLoader{snapshot: snapshot}
	admitted := make(chan struct{})
	releaseAdmission := make(chan struct{})
	admissionSealed := make(chan struct{})
	var admissionOnce sync.Once
	runtime, err := startApplicationQuotaRuntime(context.Background(), withBoundQuotaRuntime(t, repository, ApplicationQuotaRuntimeConfig{
		Repository: repository, Preferences: loader,
		Reader: newQuotaRuntimeSuccessReader(nil),
		Clock:  func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		hooks: quotaRuntimeHooks{
			afterAdmission: func() {
				admissionOnce.Do(func() {
					close(admitted)
					<-releaseAdmission
				})
			},
			afterAdmissionSealed: func() { close(admissionSealed) },
		},
	}))
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationQuotaRuntime() = %#v, %v", runtime, err)
	}
	for _, sourceInstanceID := range quotaRuntimeInstances(t, repository) {
		waitForQuotaRuntimeSchedule(t, repository, sourceInstanceID, func(schedule store.SourceRefreshSchedule) bool {
			return schedule.Reason == store.RefreshReasonDisabled && schedule.NextDueAtMS == nil
		})
	}
	enabled := snapshot
	enabled.Revision++
	enabled.Online.QuotaEnabled = true
	loader.setSnapshot(enabled)
	requestDone := make(chan error, 1)
	go func() {
		_, requestErr := runtime.RequestRefresh(
			context.Background(), quotaonline.RefreshSourceQuota, store.RefreshTriggerManual,
		)
		requestDone <- requestErr
	}()
	select {
	case <-admitted:
	case <-time.After(2 * time.Second):
		t.Fatal("manual request did not reach the admission barrier")
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- runtime.Close(context.Background()) }()
	select {
	case <-admissionSealed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not seal quota admission")
	}
	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before admitted operation drained: %v", err)
	default:
	}
	if _, err := runtime.RequestRefresh(
		context.Background(), quotaonline.RefreshSourceQuota, store.RefreshTriggerManual,
	); !errors.Is(err, ErrApplicationQuotaRuntime) {
		t.Fatalf("RequestRefresh(after seal) error = %v, want ErrApplicationQuotaRuntime", err)
	}
	close(releaseAdmission)
	select {
	case err := <-requestDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("admitted request error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("admitted request did not exit after Close cancellation")
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after admitted operation drained")
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func TestApplicationQuotaRuntimeConcurrentResumeIsSerializedAndIdempotent(t *testing.T) {
	t.Parallel()

	database, repository := openQuotaRuntimeStore(t)
	home := writeSyntheticAuthHome(t, "synthetic-resume-access-token")
	snapshot := enabledQuotaRuntimePreferences(t, home)
	snapshot.Online = preferences.OnlinePreferences{}
	loader := &quotaRuntimePreferencesLoader{snapshot: snapshot}
	firstResumeEntered := make(chan struct{})
	secondResumeEntered := make(chan struct{})
	releaseResume := make(chan struct{})
	var resumeCount atomic.Int32
	runtime, err := startApplicationQuotaRuntime(context.Background(), withBoundQuotaRuntime(t, repository, ApplicationQuotaRuntimeConfig{
		Repository: repository, Preferences: loader,
		Reader: newQuotaRuntimeSuccessReader(nil),
		Clock:  func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		hooks: quotaRuntimeHooks{
			beforeResumeReadback: func() {
				switch resumeCount.Add(1) {
				case 1:
					close(firstResumeEntered)
				case 2:
					close(secondResumeEntered)
				}
				<-releaseResume
			},
		},
	}))
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationQuotaRuntime() = %#v, %v", runtime, err)
	}
	if err := runtime.DrainGeneration(context.Background(), snapshot.CodexHome.Generation); err != nil {
		t.Fatalf("DrainGeneration() error = %v", err)
	}
	resumeErrors := make(chan error, 2)
	go func() { resumeErrors <- runtime.ResumeGeneration(context.Background(), snapshot.CodexHome.Generation) }()
	select {
	case <-firstResumeEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("first ResumeGeneration did not reach readback barrier")
	}
	go func() { resumeErrors <- runtime.ResumeGeneration(context.Background(), snapshot.CodexHome.Generation) }()
	select {
	case <-secondResumeEntered:
		t.Fatal("second ResumeGeneration entered while the first transition was active")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseResume)
	for index := 0; index < 2; index++ {
		select {
		case resumeErr := <-resumeErrors:
			if resumeErr != nil {
				t.Fatalf("ResumeGeneration(%d) error = %v", index, resumeErr)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent ResumeGeneration did not finish")
		}
	}
	if runtime.generation != snapshot.CodexHome.Generation || !runtime.accepting {
		t.Fatalf("resumed runtime generation=%d accepting=%t", runtime.generation, runtime.accepting)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func TestApplicationQuotaRuntimeFatalRunnerCancelsAdmittedOperation(t *testing.T) {
	t.Parallel()

	database, repository := openQuotaRuntimeStore(t)
	home := writeSyntheticAuthHome(t, "synthetic-fatal-access-token")
	snapshot := enabledQuotaRuntimePreferences(t, home)
	snapshot.Online = preferences.OnlinePreferences{}
	loader := &quotaRuntimePreferencesLoader{snapshot: snapshot}
	runnerStarted := make(chan struct{})
	allowFatal := make(chan struct{})
	operationAdmitted := make(chan struct{})
	runnerFailure := errors.New("synthetic quota runner fatal")
	var admissionOnce sync.Once
	runtime, err := startApplicationQuotaRuntime(context.Background(), withBoundQuotaRuntime(t, repository, ApplicationQuotaRuntimeConfig{
		Repository: repository, Preferences: loader,
		Reader: newQuotaRuntimeSuccessReader(nil),
		Clock:  func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		hooks: quotaRuntimeHooks{
			runRunner: func(context.Context) error {
				close(runnerStarted)
				<-allowFatal
				return runnerFailure
			},
			afterAdmissionContext: func(ctx context.Context) {
				admissionOnce.Do(func() { close(operationAdmitted) })
				<-ctx.Done()
			},
		},
	}))
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationQuotaRuntime() = %#v, %v", runtime, err)
	}
	select {
	case <-runnerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("quota runner did not start")
	}
	requestDone := make(chan error, 1)
	go func() {
		_, requestErr := runtime.RequestRefresh(
			context.Background(), quotaonline.RefreshSourceQuota, store.RefreshTriggerManual,
		)
		requestDone <- requestErr
	}()
	select {
	case <-operationAdmitted:
	case <-time.After(2 * time.Second):
		t.Fatal("manual request was not admitted")
	}
	close(allowFatal)
	select {
	case requestErr := <-requestDone:
		if !errors.Is(requestErr, context.Canceled) {
			t.Fatalf("RequestRefresh() error = %v, want context.Canceled", requestErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner fatal did not cancel the admitted request")
	}
	if err := runtime.DrainGeneration(
		context.Background(), snapshot.CodexHome.Generation,
	); !errors.Is(err, runnerFailure) {
		t.Fatalf("DrainGeneration() error = %v, want runner failure", err)
	}
	if err := runtime.Close(context.Background()); !errors.Is(err, runnerFailure) {
		t.Fatalf("runtime.Close() error = %v, want runner failure", err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func TestApplicationLifecycleRuntimeComposesQuotaControlHooksAndForeground(t *testing.T) {
	t.Parallel()

	database, repository := openQuotaRuntimeStore(t)
	home := writeSyntheticAuthHome(t, "synthetic-lifecycle-access-token")
	for _, directory := range []string{"sessions", "archived_sessions"} {
		if err := os.Mkdir(filepath.Join(home, directory), 0o700); err != nil {
			t.Fatalf("os.Mkdir(%s) error = %v", directory, err)
		}
	}
	initialPreferences := enabledQuotaRuntimePreferences(t, home)
	loader := &quotaRuntimePreferencesLoader{snapshot: initialPreferences}
	requests := make(chan string, 8)
	var nowMS atomic.Int64
	nowMS.Store(quotaRuntimeNowMS)
	runtime, err := startApplicationLifecycleRuntime(context.Background(), withBoundLifecycleQuota(t, repository, ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: loader,
		QuotaReader:  newQuotaRuntimeSuccessReader(requests),
		EventTimeout: time.Second, QuotaClock: func() time.Time { return time.UnixMilli(nowMS.Load()).UTC() },
	}))
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationLifecycleRuntime() = %#v, %v", runtime, err)
	}
	waitForQuotaRuntimeReads(t, requests, 2)
	waitForQuotaRuntimeState(t, repository, store.QuotaSourceInstanceWhamDefault, func(state store.SourceState) bool {
		return state.LastSuccessAtMS != nil && state.LastFailureCode == nil
	})
	waitForQuotaRuntimeState(t, repository, store.ResetCreditsSourceInstanceWhamDefault, func(state store.SourceState) bool {
		return state.LastSuccessAtMS != nil && state.LastFailureCode == nil
	})
	waitForQuotaRuntimeSchedule(t, repository, store.QuotaSourceInstanceWhamDefault, func(schedule store.SourceRefreshSchedule) bool {
		return schedule.NextDueAtMS != nil && schedule.ActiveClaimID == nil
	})
	waitForQuotaRuntimeSchedule(t, repository, store.ResetCreditsSourceInstanceWhamDefault, func(schedule store.SourceRefreshSchedule) bool {
		return schedule.NextDueAtMS != nil && schedule.ActiveClaimID == nil
	})

	nowMS.Store(quotaRuntimeNowMS + 2*time.Minute.Milliseconds())
	if err := runtime.adapter.NotifyLifecycle(t.Context(), "application_did_become_active"); err != nil {
		t.Fatalf("NotifyLifecycle() error = %v", err)
	}
	waitForQuotaRuntimeReads(t, requests, 2)
	for _, sourceInstanceID := range quotaRuntimeInstances(t, repository) {
		waitForQuotaRuntimeState(t, repository, sourceInstanceID, func(state store.SourceState) bool {
			return state.LastSuccessAtMS != nil && *state.LastSuccessAtMS == nowMS.Load() && state.LastFailureCode == nil
		})
	}

	disabled := initialPreferences
	disabled.Revision++
	disabled.Online = preferences.OnlinePreferences{}
	loader.setSnapshot(disabled)
	if err := runtime.reconcileQuotaPreferencesForTest(context.Background()); err != nil {
		t.Fatalf("reconcileQuotaPreferencesForTest(disabled) error = %v", err)
	}
	for _, sourceInstanceID := range quotaRuntimeInstances(t, repository) {
		schedule, scheduleErr := repository.SourceRefreshSchedule(context.Background(), sourceInstanceID)
		if scheduleErr != nil || schedule.Reason != store.RefreshReasonDisabled || schedule.NextDueAtMS != nil {
			t.Fatalf("SourceRefreshSchedule(%q) = %#v, %v", sourceInstanceID, schedule, scheduleErr)
		}
	}

	enabled := disabled
	enabled.Revision++
	enabled.Online = preferences.OnlinePreferences{QuotaEnabled: true, ResetCreditsEnabled: true}
	loader.setSnapshot(enabled)
	if err := runtime.reconcileQuotaPreferencesForTest(context.Background()); err != nil {
		t.Fatalf("reconcileQuotaPreferencesForTest(enabled) error = %v", err)
	}
	if _, err := runtime.RequestQuotaRefresh(context.Background(), quotaonline.RefreshSourceQuota); err != nil {
		t.Fatalf("RequestQuotaRefresh() error = %v", err)
	}
	select {
	case endpoint := <-requests:
		if endpoint != quotaRuntimeReadQuota {
			t.Fatalf("manual refresh endpoint = %q", endpoint)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("manual refresh did not call Quota endpoint")
	}

	closeContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runtime.Close(closeContext); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	if err := database.Close(closeContext); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func TestApplicationQuotaLifecycleCoordinatorSuspendsRequestsAcrossSleepAndResumesOnWake(t *testing.T) {
	t.Parallel()

	database, repository := openQuotaRuntimeStore(t)
	home := writeSyntheticAuthHome(t, "synthetic-sleep-access-token")
	snapshot := enabledQuotaRuntimePreferences(t, home)
	snapshot.Online.ResetCreditsEnabled = false
	loader := &quotaRuntimePreferencesLoader{snapshot: snapshot}
	requests := make(chan string, 4)
	reader := newQuotaRuntimeSuccessReader(requests)
	var nowMS atomic.Int64
	nowMS.Store(quotaRuntimeNowMS)
	quotaRuntime, err := startApplicationQuotaRuntime(context.Background(), withBoundQuotaRuntime(t, repository, ApplicationQuotaRuntimeConfig{
		Repository: repository, Preferences: loader,
		Reader: reader, Clock: func() time.Time { return time.UnixMilli(nowMS.Load()).UTC() },
	}))
	if err != nil || quotaRuntime == nil {
		t.Fatalf("startApplicationQuotaRuntime() = %#v, %v", quotaRuntime, err)
	}
	t.Cleanup(func() {
		closeContext, cancelClose := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelClose()
		_ = quotaRuntime.Close(closeContext)
		_ = database.Close(closeContext)
	})
	waitForQuotaRuntimeRequest(t, requests, quotaRuntimeReadQuota)
	waitForQuotaRuntimeState(t, repository, store.QuotaSourceInstanceWhamDefault, func(state store.SourceState) bool {
		return state.LastSuccessAtMS != nil && state.LastFailureCode == nil
	})
	waitForQuotaRuntimeSchedule(t, repository, store.QuotaSourceInstanceWhamDefault, func(schedule store.SourceRefreshSchedule) bool {
		return schedule.NextDueAtMS != nil && schedule.ActiveClaimID == nil
	})

	nowMS.Store(quotaRuntimeNowMS + 61_000)
	started := make(chan struct{}, 1)
	block := make(chan struct{})
	reader.started = started
	reader.block = block
	refreshDone := make(chan error, 1)
	go func() {
		_, refreshErr := quotaRuntime.RequestRefresh(
			context.Background(), quotaonline.RefreshSourceQuota, store.RefreshTriggerManual,
		)
		refreshDone <- refreshErr
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("manual quota request did not reach the blocking App Server reader")
	}
	waitForQuotaRuntimeRequest(t, requests, quotaRuntimeReadQuota)

	generation := int64(snapshot.CodexHome.Generation)
	local := &quotaLifecycleCoordinatorStub{
		state:        store.SchedulerLifecycle{HomeGeneration: generation},
		sleepEntered: make(chan struct{}, 1),
		releaseSleep: make(chan struct{}, 1),
	}
	t.Cleanup(func() {
		select {
		case local.releaseSleep <- struct{}{}:
		default:
		}
	})
	lifecycle := applicationQuotaLifecycleCoordinator{local: local, quota: quotaRuntime}
	sleepContext, cancelSleep := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelSleep()
	sleepDone := make(chan error, 1)
	go func() {
		_, sleepErr := lifecycle.SystemWillSleep(sleepContext, "sleep:quota-runtime")
		sleepDone <- sleepErr
	}()
	select {
	case <-local.sleepEntered:
	case <-time.After(time.Second):
		t.Fatal("SystemWillSleep did not reach the local lifecycle coordinator")
	}
	quotaRuntime.mu.Lock()
	acceptingWhileSleeping := quotaRuntime.accepting
	quotaRuntime.mu.Unlock()
	if acceptingWhileSleeping {
		t.Fatal("quota runtime still accepted work after local sleep handling began")
	}
	select {
	case refreshErr := <-refreshDone:
		if refreshErr != nil {
			t.Fatalf("cancelled manual refresh error = %v", refreshErr)
		}
	case <-time.After(time.Second):
		t.Fatal("SystemWillSleep did not cancel the in-flight quota request")
	}
	local.releaseSleep <- struct{}{}
	select {
	case sleepErr := <-sleepDone:
		if sleepErr != nil {
			t.Fatalf("SystemWillSleep() error = %v", sleepErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SystemWillSleep did not finish after the local drain completed")
	}
	waitForQuotaRuntimeState(t, repository, store.QuotaSourceInstanceWhamDefault, func(state store.SourceState) bool {
		return state.ConsecutiveFailures == 0 && state.LastFailureCode != nil &&
			*state.LastFailureCode == store.SourceFailureCancelled
	})

	reader.block = nil
	nowMS.Store(quotaRuntimeNowMS + 10*time.Minute.Milliseconds())
	if _, err := lifecycle.SystemDidWake(context.Background(), "wake:quota-runtime"); err != nil {
		t.Fatalf("SystemDidWake() error = %v", err)
	}
	waitForQuotaRuntimeRequest(t, requests, quotaRuntimeReadQuota)
	waitForQuotaRuntimeState(t, repository, store.QuotaSourceInstanceWhamDefault, func(state store.SourceState) bool {
		return state.LastSuccessAtMS != nil && *state.LastSuccessAtMS == nowMS.Load() &&
			state.ConsecutiveFailures == 0 && state.LastFailureCode == nil
	})
}

type quotaLifecycleCoordinatorStub struct {
	state        store.SchedulerLifecycle
	sleepEntered chan struct{}
	releaseSleep chan struct{}
}

func (coordinator *quotaLifecycleCoordinatorStub) SystemWillSleep(
	context.Context,
	string,
) (store.SchedulerLifecycle, error) {
	if coordinator.sleepEntered != nil {
		coordinator.sleepEntered <- struct{}{}
	}
	if coordinator.releaseSleep != nil {
		<-coordinator.releaseSleep
	}
	coordinator.state.SystemState = store.LifecycleSystemSleeping
	return coordinator.state, nil
}

func (coordinator *quotaLifecycleCoordinatorStub) SystemDidWake(
	context.Context,
	string,
) (store.SchedulerLifecycle, error) {
	coordinator.state.SystemState = store.LifecycleSystemAwake
	return coordinator.state, nil
}

func (coordinator *quotaLifecycleCoordinatorStub) SourceChanged(
	context.Context,
	string,
	bool,
) (store.SchedulerLifecycle, error) {
	return coordinator.state, nil
}

func TestApplicationLifecycleRuntimeCommitsSettingsBeforeQuotaReconcile(t *testing.T) {
	t.Parallel()

	database, repository := openQuotaRuntimeStore(t)
	home := writeSyntheticAuthHome(t, "synthetic-settings-access-token")
	for _, directory := range []string{"sessions", "archived_sessions"} {
		if err := os.Mkdir(filepath.Join(home, directory), 0o700); err != nil {
			t.Fatalf("os.Mkdir(%s) error = %v", directory, err)
		}
	}
	preferenceStore := confirmedQuotaRuntimeFileStore(t, home, true, true)
	requests := make(chan string, 2)
	invalidation := &recordingQueryInvalidationNotifier{}
	runtime, err := startApplicationLifecycleRuntime(context.Background(), withBoundLifecycleQuota(t, repository, ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		QuotaReader:  newQuotaRuntimeSuccessReader(requests),
		Invalidation: invalidation,
		EventTimeout: time.Second,
		QuotaClock:   func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	}))
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationLifecycleRuntime() = %#v, %v", runtime, err)
	}
	waitForQuotaRuntimeReads(t, requests, 2)
	for _, sourceInstanceID := range quotaRuntimeInstances(t, repository) {
		waitForQuotaRuntimeSchedule(t, repository, sourceInstanceID, func(schedule store.SourceRefreshSchedule) bool {
			return schedule.NextDueAtMS != nil && schedule.ActiveClaimID == nil
		})
	}
	current, err := preferenceStore.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences(before settings) error = %v", err)
	}
	invalidation.reset()
	committed, err := runtime.UpdateQuotaSettings(context.Background(), preferences.SettingsUpdate{
		ExpectedRevision: current.Revision,
		Providers:        current.Providers,
		Online:           preferences.OnlinePreferences{},
		Refresh:          current.Refresh,
		Updates:          current.Updates,
		UI:               current.UI,
	})
	if err != nil {
		t.Fatalf("UpdateQuotaSettings() error = %v", err)
	}
	if committed.Revision != current.Revision+1 || committed.Online != (preferences.OnlinePreferences{}) {
		t.Fatalf("committed settings = %#v", committed)
	}
	if invalidation.count(core.InvalidationSettings) != 1 ||
		invalidation.count(core.InvalidationQuota) != 1 {
		t.Fatalf(
			"settings invalidation counts = settings:%d quota:%d",
			invalidation.count(core.InvalidationSettings),
			invalidation.count(core.InvalidationQuota),
		)
	}
	readback, err := preferenceStore.LoadPreferences(context.Background())
	if err != nil || readback.Revision != committed.Revision || readback.Online != committed.Online {
		t.Fatalf("LoadPreferences(after settings) = %#v, %v", readback, err)
	}
	for _, sourceInstanceID := range quotaRuntimeInstances(t, repository) {
		schedule, scheduleErr := repository.SourceRefreshSchedule(context.Background(), sourceInstanceID)
		if scheduleErr != nil || schedule.Reason != store.RefreshReasonDisabled || schedule.NextDueAtMS != nil {
			t.Fatalf("SourceRefreshSchedule(%q) = %#v, %v", sourceInstanceID, schedule, scheduleErr)
		}
	}

	closeContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runtime.Close(closeContext); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	if err := database.Close(closeContext); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func TestApplicationLifecycleRuntimeReturnsCommittedSettingsOnReconcileFailure(t *testing.T) {
	t.Parallel()

	database, repository := openQuotaRuntimeStore(t)
	home := writeSyntheticAuthHome(t, "synthetic-post-commit-access-token")
	for _, directory := range []string{"sessions", "archived_sessions"} {
		if err := os.Mkdir(filepath.Join(home, directory), 0o700); err != nil {
			t.Fatalf("os.Mkdir(%s) error = %v", directory, err)
		}
	}
	preferenceStore := confirmedQuotaRuntimeFileStore(t, home, false, false)
	invalidation := &recordingQueryInvalidationNotifier{}
	runtime, err := startApplicationLifecycleRuntime(context.Background(), withBoundLifecycleQuota(t, repository, ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		QuotaReader:  newQuotaRuntimeSuccessReader(nil),
		Invalidation: invalidation,
		EventTimeout: time.Second,
		QuotaClock:   func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	}))
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationLifecycleRuntime() = %#v, %v", runtime, err)
	}
	for _, sourceInstanceID := range quotaRuntimeInstances(t, repository) {
		waitForQuotaRuntimeSchedule(t, repository, sourceInstanceID, func(schedule store.SourceRefreshSchedule) bool {
			return schedule.Reason == store.RefreshReasonDisabled && schedule.NextDueAtMS == nil
		})
	}
	current, err := preferenceStore.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences(before settings) error = %v", err)
	}
	reconcileFailure := errors.New("synthetic reconcile failure")
	runtime.quota.reconcilePreferences = func(context.Context) error { return reconcileFailure }
	invalidation.reset()
	committed, err := runtime.UpdateQuotaSettings(context.Background(), preferences.SettingsUpdate{
		ExpectedRevision: current.Revision,
		Providers:        current.Providers,
		Online: preferences.OnlinePreferences{
			QuotaEnabled: true,
		},
		Refresh: current.Refresh,
		Updates: current.Updates,
		UI:      current.UI,
	})
	if !errors.Is(err, ErrApplicationPreferencesPostCommit) || !errors.Is(err, reconcileFailure) {
		t.Fatalf("UpdateQuotaSettings() error = %v", err)
	}
	var postCommitError *ApplicationPreferencesPostCommitError
	if !errors.As(err, &postCommitError) || postCommitError.Committed.Revision != committed.Revision {
		t.Fatalf("post-commit error = %#v, committed = %#v", postCommitError, committed)
	}
	if invalidation.count(core.InvalidationSettings) != 1 ||
		invalidation.count(core.InvalidationQuota) != 1 {
		t.Fatalf(
			"post-commit invalidation counts = settings:%d quota:%d",
			invalidation.count(core.InvalidationSettings),
			invalidation.count(core.InvalidationQuota),
		)
	}
	readback, err := preferenceStore.LoadPreferences(context.Background())
	if err != nil || readback.Revision != current.Revision+1 || !readback.Online.QuotaEnabled {
		t.Fatalf("LoadPreferences(after failed reconcile) = %#v, %v", readback, err)
	}

	closeContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runtime.Close(closeContext); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	if err := database.Close(closeContext); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func TestApplicationLifecycleRuntimeBeginDrainSealsAdmissionAndDrainsSettingsUpdate(t *testing.T) {
	t.Parallel()

	database, repository := openQuotaRuntimeStore(t)
	home := writeSyntheticAuthHome(t, "synthetic-close-settings-access-token")
	for _, directory := range []string{"sessions", "archived_sessions"} {
		if err := os.Mkdir(filepath.Join(home, directory), 0o700); err != nil {
			t.Fatalf("os.Mkdir(%s) error = %v", directory, err)
		}
	}
	preferenceStore := confirmedQuotaRuntimeFileStore(t, home, false, false)
	runtime, err := startApplicationLifecycleRuntime(context.Background(), withBoundLifecycleQuota(t, repository, ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		QuotaReader:  newQuotaRuntimeSuccessReader(nil),
		EventTimeout: time.Second,
		QuotaClock:   func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	}))
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationLifecycleRuntime() = %#v, %v", runtime, err)
	}
	current, err := preferenceStore.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences(before settings) error = %v", err)
	}
	reconcileStarted := make(chan struct{})
	releaseReconcile := make(chan struct{})
	runtime.quota.reconcilePreferences = func(context.Context) error {
		close(reconcileStarted)
		<-releaseReconcile
		return nil
	}
	updateDone := make(chan error, 1)
	go func() {
		_, updateErr := runtime.UpdateQuotaSettings(context.Background(), preferences.SettingsUpdate{
			ExpectedRevision: current.Revision,
			Providers:        current.Providers,
			Online: preferences.OnlinePreferences{
				QuotaEnabled: true,
			},
			Refresh: current.Refresh,
			Updates: current.Updates,
			UI:      current.UI,
		})
		updateDone <- updateErr
	}()
	select {
	case <-reconcileStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("settings update did not reach reconcile barrier")
	}
	drainDone := make(chan error, 1)
	go func() { drainDone <- runtime.BeginDrain(context.Background()) }()
	select {
	case <-runtime.controlCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("BeginDrain did not seal application control admission")
	}
	select {
	case err := <-drainDone:
		t.Fatalf("BeginDrain returned before settings update drained: %v", err)
	default:
	}
	if _, err := runtime.UpdateQuotaSettings(context.Background(), preferences.SettingsUpdate{}); !errors.Is(
		err,
		ErrApplicationLifecycleRuntime,
	) {
		t.Fatalf("UpdateQuotaSettings(after seal) error = %v", err)
	}
	close(releaseReconcile)
	select {
	case err := <-updateDone:
		if err != nil {
			t.Fatalf("UpdateQuotaSettings() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("settings update did not drain")
	}
	select {
	case err := <-drainDone:
		if err != nil {
			t.Fatalf("BeginDrain() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("BeginDrain did not return after settings update drained")
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func TestApplicationLifecycleRuntimeSettingsAndHomeConfirmDoNotDeadlock(t *testing.T) {
	t.Parallel()

	database, repository := openQuotaRuntimeStore(t)
	homeA := writeSyntheticAuthHome(t, "synthetic-settings-home-a-token")
	homeB := writeSyntheticAuthHome(t, "synthetic-settings-home-b-token")
	for _, home := range []string{homeA, homeB} {
		for _, directory := range []string{"sessions", "archived_sessions"} {
			if err := os.Mkdir(filepath.Join(home, directory), 0o700); err != nil {
				t.Fatalf("os.Mkdir(%s) error = %v", directory, err)
			}
		}
	}
	preferenceStore := confirmedQuotaRuntimeFileStore(t, homeA, false, false)
	settingsAdmitted := make(chan struct{})
	releaseSettings := make(chan struct{})
	var admissionOnce sync.Once
	runtime, err := startApplicationLifecycleRuntime(context.Background(), withBoundLifecycleQuota(t, repository, ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		QuotaReader:  newQuotaRuntimeSuccessReader(nil),
		EventTimeout: time.Second,
		QuotaClock:   func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		quotaHooks: quotaRuntimeHooks{
			afterAdmission: func() {
				admissionOnce.Do(func() {
					close(settingsAdmitted)
					<-releaseSettings
				})
			},
		},
	}))
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationLifecycleRuntime() = %#v, %v", runtime, err)
	}
	plan, err := runtime.PlanQuotaHomeSwitch(
		context.Background(), homeB, preferences.HomeSwitchClearAndRebuild,
	)
	if err != nil {
		t.Fatalf("PlanQuotaHomeSwitch() error = %v", err)
	}
	current, err := preferenceStore.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences(before settings) error = %v", err)
	}
	settingsDone := make(chan error, 1)
	go func() {
		_, settingsErr := runtime.UpdateQuotaSettings(context.Background(), preferences.SettingsUpdate{
			ExpectedRevision: current.Revision,
			Providers:        current.Providers,
			Online: preferences.OnlinePreferences{
				QuotaEnabled: true,
			},
			Refresh: current.Refresh,
			Updates: current.Updates,
			UI:      current.UI,
		})
		settingsDone <- settingsErr
	}()
	select {
	case <-settingsAdmitted:
	case <-time.After(2 * time.Second):
		t.Fatal("settings update did not reach quota admission barrier")
	}
	confirmDone := make(chan error, 1)
	go func() {
		_, confirmErr := runtime.ConfirmQuotaHomeSwitch(context.Background(), plan.ID)
		confirmDone <- confirmErr
	}()
	close(releaseSettings)
	select {
	case settingsErr := <-settingsDone:
		if settingsErr != nil {
			t.Fatalf("UpdateQuotaSettings() error = %v", settingsErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("settings update deadlocked with Home confirm")
	}
	select {
	case confirmErr := <-confirmDone:
		if !errors.Is(confirmErr, preferences.ErrSwitchPlanStale) {
			t.Fatalf("ConfirmQuotaHomeSwitch() error = %v, want stale plan", confirmErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Home confirm deadlocked with settings update")
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func TestApplicationLifecycleRuntimeRecoversPendingResumeBeforeQuotaStart(t *testing.T) {
	t.Parallel()

	database, repository := openQuotaRuntimeStore(t)
	home := writeSyntheticAuthHome(t, "synthetic-pending-resume-token")
	for _, directory := range []string{"sessions", "archived_sessions"} {
		if err := os.Mkdir(filepath.Join(home, directory), 0o700); err != nil {
			t.Fatalf("os.Mkdir(%s) error = %v", directory, err)
		}
	}
	preferenceStore := confirmedQuotaRuntimeFileStore(t, home, true, false)
	installQuotaRuntimePendingResume(t, preferenceStore)
	requests := make(chan quotaHomeRequestEvent, 2)
	runtime, err := startApplicationLifecycleRuntime(context.Background(), withBoundLifecycleQuota(t, repository, ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		QuotaReader:  newQuotaRuntimeHomeReader(preferenceStore, requests),
		EventTimeout: time.Second,
		QuotaClock:   func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	}))
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationLifecycleRuntime() = %#v, %v", runtime, err)
	}
	readback, err := preferenceStore.LoadPreferences(context.Background())
	if err != nil || readback.PendingResume != nil || readback.PendingSwitch != nil ||
		readback.CodexHome.Generation != 1 || readback.LastSwitch == nil ||
		readback.LastSwitch.Outcome != preferences.HomeSwitchRolledBack {
		t.Fatalf("recovered pending resume = %#v, %v", readback, err)
	}
	assertQuotaRuntimeLifecycleGeneration(t, database, 1)
	request := waitForQuotaHomeRequest(t, requests)
	assertQuotaRuntimeHome(t, request, home)
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func TestApplicationLifecycleRuntimeRollsBackPendingSwitchBeforeQuotaStart(t *testing.T) {
	t.Parallel()

	database, repository := openQuotaRuntimeStore(t)
	homeA := writeSyntheticAuthHome(t, "synthetic-pending-old-token")
	homeB := writeSyntheticAuthHome(t, "synthetic-pending-target-token")
	for _, home := range []string{homeA, homeB} {
		for _, directory := range []string{"sessions", "archived_sessions"} {
			if err := os.Mkdir(filepath.Join(home, directory), 0o700); err != nil {
				t.Fatalf("os.Mkdir(%s) error = %v", directory, err)
			}
		}
	}
	preferenceStore := confirmedQuotaRuntimeFileStore(t, homeA, true, false)
	installQuotaRuntimePendingSwitch(t, preferenceStore, homeB)
	requests := make(chan quotaHomeRequestEvent, 2)
	runtime, err := startApplicationLifecycleRuntime(context.Background(), withBoundLifecycleQuota(t, repository, ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		QuotaReader:  newQuotaRuntimeHomeReader(preferenceStore, requests),
		EventTimeout: time.Second,
		QuotaClock:   func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	}))
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationLifecycleRuntime() = %#v, %v", runtime, err)
	}
	readback, err := preferenceStore.LoadPreferences(context.Background())
	if err != nil || readback.PendingResume != nil || readback.PendingSwitch != nil ||
		readback.CodexHome.Generation != 1 || readback.LastSwitch == nil ||
		readback.LastSwitch.Outcome != preferences.HomeSwitchRolledBack {
		t.Fatalf("rolled-back pending switch = %#v, %v", readback, err)
	}
	assertQuotaRuntimeLifecycleGeneration(t, database, 1)
	request := waitForQuotaHomeRequest(t, requests)
	assertQuotaRuntimeHome(t, request, homeA)
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func TestApplicationLifecycleRuntimeFinalizesPendingSwitchBeforeQuotaStart(t *testing.T) {
	t.Parallel()

	database, repository := openQuotaRuntimeStore(t)
	homeA := writeSyntheticAuthHome(t, "synthetic-finalize-old-token")
	homeB := writeSyntheticAuthHome(t, "synthetic-finalize-target-token")
	for _, home := range []string{homeA, homeB} {
		for _, directory := range []string{"sessions", "archived_sessions"} {
			if err := os.Mkdir(filepath.Join(home, directory), 0o700); err != nil {
				t.Fatalf("os.Mkdir(%s) error = %v", directory, err)
			}
		}
	}
	preferenceStore := confirmedQuotaRuntimeFileStore(t, homeA, true, false)
	pending := installQuotaRuntimePendingSwitch(t, preferenceStore, homeB)
	bootstrapRuntime, err := bootstrap.NewRuntime(bootstrap.RuntimeConfig{Repository: repository})
	if err != nil {
		t.Fatalf("bootstrap.NewRuntime() error = %v", err)
	}
	if err := bootstrapRuntime.StartBootstrap(context.Background(), preferences.BootstrapRequest{
		SwitchID: pending.SwitchID, Generation: pending.Target.Generation,
		Source: pending.Target.Source, DataStoreKey: pending.Target.DataStoreKey,
		Strategy: pending.Strategy,
	}); err != nil {
		t.Fatalf("StartBootstrap(pending target) error = %v", err)
	}
	requests := make(chan quotaHomeRequestEvent, 2)
	runtime, err := startApplicationLifecycleRuntime(context.Background(), withBoundLifecycleQuota(t, repository, ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		QuotaReader:  newQuotaRuntimeHomeReader(preferenceStore, requests),
		EventTimeout: time.Second,
		QuotaClock:   func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	}))
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationLifecycleRuntime() = %#v, %v", runtime, err)
	}
	job, _, err := repository.LatestBootstrapRunByGeneration(context.Background(), 2)
	if err != nil {
		t.Fatalf("LatestBootstrapRunByGeneration() error = %v", err)
	}
	task, err := repository.SchedulerTask(context.Background(), "task-"+job.JobID)
	if err != nil {
		t.Fatalf("SchedulerTask(application bootstrap) error = %v", err)
	}
	if task.TargetID != job.JobID || task.Lane != store.SchedulerLaneBackfill ||
		task.ServiceClass != store.SchedulerServiceInteractive {
		t.Fatalf("application bootstrap scheduler task = %#v", task)
	}
	readback, err := preferenceStore.LoadPreferences(context.Background())
	if err != nil || readback.PendingResume != nil || readback.PendingSwitch != nil ||
		readback.CodexHome.Generation != 2 || readback.LastSwitch == nil ||
		readback.LastSwitch.Outcome != preferences.HomeSwitchCompleted {
		t.Fatalf("finalized pending switch = %#v, %v", readback, err)
	}
	assertQuotaRuntimeLifecycleGeneration(t, database, 2)
	request := waitForQuotaHomeRequest(t, requests)
	assertQuotaRuntimeHome(t, request, homeB)
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func TestApplicationLifecycleRuntimeKeepsUnknownPendingSwitchSuspended(t *testing.T) {
	t.Parallel()

	database, repository := openQuotaRuntimeStore(t)
	homeA := writeSyntheticAuthHome(t, "synthetic-unknown-old-token")
	homeB := writeSyntheticAuthHome(t, "synthetic-unknown-target-token")
	for _, home := range []string{homeA, homeB} {
		for _, directory := range []string{"sessions", "archived_sessions"} {
			if err := os.Mkdir(filepath.Join(home, directory), 0o700); err != nil {
				t.Fatalf("os.Mkdir(%s) error = %v", directory, err)
			}
		}
	}
	preferenceStore := confirmedQuotaRuntimeFileStore(t, homeA, true, false)
	pending := installQuotaRuntimePendingSwitch(t, preferenceStore, homeB)
	statusFailure := errors.New("synthetic bootstrap status unavailable")
	transportCalls := make(chan struct{}, 1)
	runtime, err := startApplicationLifecycleRuntime(context.Background(), withBoundLifecycleQuota(t, repository, ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		QuotaReader:  newQuotaRuntimeSuccessReader(nil),
		EventTimeout: time.Second,
		QuotaClock:   func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		homeRuntime: &quotaStartupHomeRuntime{
			status: preferences.BootstrapStatusNotStarted,
			err:    statusFailure,
		},
	}))
	if runtime != nil || !errors.Is(err, ErrApplicationLifecycleRuntime) {
		t.Fatalf("startApplicationLifecycleRuntime(unknown) = %#v, %v", runtime, err)
	}
	readback, loadErr := preferenceStore.LoadPreferences(context.Background())
	if loadErr != nil || readback.PendingSwitch == nil ||
		readback.PendingSwitch.SwitchID != pending.SwitchID || readback.CodexHome.Generation != 2 {
		t.Fatalf("unknown pending switch = %#v, %v", readback, loadErr)
	}
	select {
	case <-transportCalls:
		t.Fatal("quota transport ran before unknown Home recovery resolved")
	default:
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func TestApplicationLifecycleRuntimeDrainsQuotaBeforeHomeSwitch(t *testing.T) {
	t.Parallel()

	database, repository := openQuotaRuntimeStore(t)
	homeA := writeSyntheticAuthHome(t, "synthetic-home-a-access-token")
	homeB := writeSyntheticAuthHome(t, "synthetic-home-b-access-token")
	homeACanonical := quotaRuntimePreferencesForHome(t, homeA).CodexHome.Source.Path
	homeBCanonical := quotaRuntimePreferencesForHome(t, homeB).CodexHome.Source.Path
	for _, home := range []string{homeA, homeB} {
		for _, directory := range []string{"sessions", "archived_sessions"} {
			if err := os.Mkdir(filepath.Join(home, directory), 0o700); err != nil {
				t.Fatalf("os.Mkdir(%s) error = %v", directory, err)
			}
		}
	}
	preferenceStore := confirmedQuotaRuntimeFileStore(t, homeA, true, false)
	requests := make(chan quotaHomeRequestEvent, 8)
	oldRequestStarted := make(chan struct{}, 1)
	releaseOldRequest := make(chan struct{})
	var nowMS atomic.Int64
	nowMS.Store(quotaRuntimeNowMS)
	reader := newQuotaRuntimeHomeReader(preferenceStore, requests)
	runtime, err := startApplicationLifecycleRuntime(context.Background(), withBoundLifecycleQuota(t, repository, ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		QuotaReader:  reader,
		EventTimeout: time.Second, QuotaClock: func() time.Time { return time.UnixMilli(nowMS.Load()).UTC() },
	}))
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationLifecycleRuntime() = %#v, %v", runtime, err)
	}
	initialRequest := waitForQuotaHomeRequest(t, requests)
	assertQuotaRuntimeHome(t, initialRequest, homeA)
	waitForQuotaRuntimeSchedule(t, repository, store.QuotaSourceInstanceWhamDefault, func(schedule store.SourceRefreshSchedule) bool {
		return schedule.NextDueAtMS != nil && schedule.ActiveClaimID == nil
	})
	plan, err := runtime.PlanQuotaHomeSwitch(
		context.Background(), homeB, preferences.HomeSwitchClearAndRebuild,
	)
	if err != nil {
		t.Fatalf("PlanQuotaHomeSwitch() error = %v", err)
	}
	reader.started = oldRequestStarted
	reader.block = releaseOldRequest
	manualDone := make(chan error, 1)
	go func() {
		_, manualErr := runtime.RequestQuotaRefresh(context.Background(), quotaonline.RefreshSourceQuota)
		manualDone <- manualErr
	}()
	oldRequest := waitForQuotaHomeRequest(t, requests)
	assertQuotaRuntimeHome(t, oldRequest, homeA)
	select {
	case <-oldRequestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("old Home request did not reach transport barrier")
	}
	confirmDone := make(chan struct {
		snapshot preferences.Snapshot
		err      error
	}, 1)
	go func() {
		snapshot, confirmErr := runtime.ConfirmQuotaHomeSwitch(context.Background(), plan.ID)
		confirmDone <- struct {
			snapshot preferences.Snapshot
			err      error
		}{snapshot: snapshot, err: confirmErr}
	}()
	waitForAppCondition(t, func() bool {
		visible, loadErr := preferenceStore.LoadPreferences(context.Background())
		lifecycle, lifecycleErr := repository.SchedulerLifecycle(context.Background())
		return loadErr == nil && visible.PendingResume != nil && lifecycleErr == nil &&
			lifecycle.HomeGeneration == 1 &&
			lifecycle.Transition == store.LifecycleTransitionBlocked &&
			lifecycle.SourceState == store.LifecycleSourceUnavailable
	}, "Home switch did not publish the old-generation resume guard")
	guard, err := preferenceStore.LoadPreferences(context.Background())
	if err != nil || guard.CodexHome.Generation != 1 || guard.CodexHome.Source.Path != homeACanonical {
		t.Fatalf("preferences while draining = %#v, %v", guard, err)
	}
	lifecycleWhileDraining, err := repository.SchedulerLifecycle(context.Background())
	if err != nil || lifecycleWhileDraining.HomeGeneration != 1 ||
		lifecycleWhileDraining.Transition != store.LifecycleTransitionBlocked ||
		lifecycleWhileDraining.SourceState != store.LifecycleSourceUnavailable {
		t.Fatalf("lifecycle while draining = %#v, %v", lifecycleWhileDraining, err)
	}
	select {
	case result := <-confirmDone:
		t.Fatalf("ConfirmQuotaHomeSwitch returned before old request drained: %#v", result)
	default:
	}
	close(releaseOldRequest)
	select {
	case err := <-manualDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("old manual request error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("old manual request did not drain")
	}
	var switched preferences.Snapshot
	select {
	case result := <-confirmDone:
		if result.err != nil {
			t.Fatalf("ConfirmQuotaHomeSwitch() error = %v", result.err)
		}
		switched = result.snapshot
	case <-time.After(2 * time.Second):
		t.Fatal("Home switch did not finish after old request drained")
	}
	if switched.CodexHome.Generation != 2 || switched.CodexHome.Source.Path != homeBCanonical ||
		switched.PendingResume != nil || switched.PendingSwitch != nil {
		t.Fatalf("switched preferences = %#v", switched)
	}
	lifecycleAfterSwitch, err := repository.SchedulerLifecycle(context.Background())
	if err != nil || lifecycleAfterSwitch.HomeGeneration != 2 ||
		lifecycleAfterSwitch.Transition != store.LifecycleTransitionSteady ||
		lifecycleAfterSwitch.SourceState != store.LifecycleSourceAvailable {
		t.Fatalf("lifecycle after switch = %#v, %v", lifecycleAfterSwitch, err)
	}
	reader.block = nil
	nowMS.Store(quotaRuntimeNowMS + 61*time.Second.Milliseconds())
	if _, err := runtime.RequestQuotaRefresh(context.Background(), quotaonline.RefreshSourceQuota); err != nil {
		t.Fatalf("RequestQuotaRefresh(new Home) error = %v", err)
	}
	newRequest := waitForQuotaHomeRequest(t, requests)
	assertQuotaRuntimeHome(t, newRequest, homeB)

	closeContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runtime.Close(closeContext); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	restarted, err := startApplicationLifecycleRuntime(context.Background(), withBoundLifecycleQuota(t, repository, ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		QuotaReader:  newQuotaRuntimeHomeReader(preferenceStore, requests),
		EventTimeout: time.Second, QuotaClock: func() time.Time { return time.UnixMilli(nowMS.Load()).UTC() },
	}))
	if err != nil || restarted == nil {
		t.Fatalf("startApplicationLifecycleRuntime(restart) = %#v, %v", restarted, err)
	}
	if restarted.quota.generation != 2 || !restarted.quota.accepting {
		t.Fatalf("restarted quota generation=%d accepting=%t", restarted.quota.generation, restarted.quota.accepting)
	}
	assertQuotaRuntimeLifecycleGeneration(t, database, 2)
	if err := restarted.Close(closeContext); err != nil {
		t.Fatalf("restarted.Close() error = %v", err)
	}
	if err := database.Close(closeContext); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func TestApplicationLifecycleRuntimeHomeSwitchRearmsCredentialBackedOffQuotaSources(t *testing.T) {
	database, repository := openQuotaRuntimeStore(t)
	homeWithoutCredentials := t.TempDir()
	homeWithCredentials := writeSyntheticAuthHome(t, "synthetic-home-switch-recovery-token")
	for _, home := range []string{homeWithoutCredentials, homeWithCredentials} {
		for _, directory := range []string{"sessions", "archived_sessions"} {
			if err := os.Mkdir(filepath.Join(home, directory), 0o700); err != nil {
				t.Fatalf("os.Mkdir(%s) error = %v", directory, err)
			}
		}
	}
	preferenceStore := confirmedQuotaRuntimeFileStore(
		t, homeWithoutCredentials, true, true,
	)
	requests := make(chan quotaHomeRequestEvent, 16)
	reader := newQuotaRuntimeHomeReader(preferenceStore, requests)
	reader.failWhenHome = func(path string) bool {
		return path == quotaRuntimePreferencesForHome(t, homeWithoutCredentials).CodexHome.Source.Path
	}
	runtime, err := startApplicationLifecycleRuntime(context.Background(), withBoundLifecycleQuota(t, repository, ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		QuotaReader:  reader,
		EventTimeout: time.Second, QuotaClock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	}))
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationLifecycleRuntime() = %#v, %v", runtime, err)
	}
	for _, sourceInstanceID := range quotaRuntimeInstances(t, repository) {
		waitForQuotaRuntimeState(t, repository, sourceInstanceID, func(state store.SourceState) bool {
			return state.LastFailureCode != nil &&
				*state.LastFailureCode == store.SourceFailureNetworkUnavailable
		})
		waitForQuotaRuntimeSchedule(t, repository, sourceInstanceID, func(schedule store.SourceRefreshSchedule) bool {
			return schedule.NextDueAtMS != nil && *schedule.NextDueAtMS > quotaRuntimeNowMS &&
				*schedule.NextDueAtMS <= quotaRuntimeNowMS+330_000 && schedule.ActiveClaimID == nil &&
				schedule.Reason == store.RefreshReasonNetworkBackoff
		})
	}
	for {
		select {
		case <-requests:
		default:
			goto drainedHomeFailureEvents
		}
	}
drainedHomeFailureEvents:

	plan, err := runtime.PlanQuotaHomeSwitch(
		context.Background(), homeWithCredentials, preferences.HomeSwitchClearAndRebuild,
	)
	if err != nil {
		t.Fatalf("PlanQuotaHomeSwitch() error = %v", err)
	}
	if _, err := runtime.ConfirmQuotaHomeSwitch(context.Background(), plan.ID); err != nil {
		t.Fatalf("ConfirmQuotaHomeSwitch() error = %v", err)
	}

	seen := make(map[string]bool, 2)
	for len(seen) < 2 {
		request := waitForQuotaHomeRequest(t, requests)
		assertQuotaRuntimeHome(t, request, homeWithCredentials)
		seen[request.kind] = true
	}
	if !seen[quotaRuntimeReadQuota] || !seen[quotaRuntimeReadReset] {
		t.Fatalf("Home switch requests = %#v", seen)
	}
	for _, sourceInstanceID := range quotaRuntimeInstances(t, repository) {
		waitForQuotaRuntimeState(t, repository, sourceInstanceID, func(state store.SourceState) bool {
			return state.LastSuccessAtMS != nil && state.LastFailureCode == nil
		})
		waitForQuotaRuntimeSchedule(t, repository, sourceInstanceID, func(schedule store.SourceRefreshSchedule) bool {
			return schedule.NextDueAtMS != nil && schedule.ActiveClaimID == nil
		})
	}

	closeContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runtime.Close(closeContext); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	if err := database.Close(closeContext); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func TestApplicationLifecycleRuntimeHomeSwitchRollbackRearmsQuotaOnce(t *testing.T) {
	database, repository := openQuotaRuntimeStore(t)
	homeA := writeSyntheticAuthHome(t, "synthetic-home-switch-rollback-a-token")
	homeB := writeSyntheticAuthHome(t, "synthetic-home-switch-rollback-b-token")
	for _, home := range []string{homeA, homeB} {
		for _, directory := range []string{"sessions", "archived_sessions"} {
			if err := os.Mkdir(filepath.Join(home, directory), 0o700); err != nil {
				t.Fatalf("os.Mkdir(%s) error = %v", directory, err)
			}
		}
	}
	preferenceStore := confirmedQuotaRuntimeFileStore(t, homeA, true, false)
	requests := make(chan quotaHomeRequestEvent, 4)
	startFailure := errors.New("synthetic Home bootstrap did not start")
	runtime, err := startApplicationLifecycleRuntime(context.Background(), withBoundLifecycleQuota(t, repository, ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		QuotaReader:  newQuotaRuntimeHomeReader(preferenceStore, requests),
		EventTimeout: time.Second, QuotaClock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		homeRuntime: &quotaStartupHomeRuntime{
			status:   preferences.BootstrapStatusNotStarted,
			startErr: startFailure,
		},
	}))
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationLifecycleRuntime() = %#v, %v", runtime, err)
	}
	initialRequest := waitForQuotaHomeRequest(t, requests)
	assertQuotaRuntimeHome(t, initialRequest, homeA)
	waitForQuotaRuntimeSchedule(t, repository, store.QuotaSourceInstanceWhamDefault, func(schedule store.SourceRefreshSchedule) bool {
		return schedule.NextDueAtMS != nil && schedule.ActiveClaimID == nil
	})

	plan, err := runtime.PlanQuotaHomeSwitch(
		context.Background(), homeB, preferences.HomeSwitchClearAndRebuild,
	)
	if err != nil {
		t.Fatalf("PlanQuotaHomeSwitch() error = %v", err)
	}
	rolledBack, err := runtime.ConfirmQuotaHomeSwitch(context.Background(), plan.ID)
	if !errors.Is(err, startFailure) {
		t.Fatalf("ConfirmQuotaHomeSwitch() error = %v, want start failure", err)
	}
	if rolledBack.CodexHome.Generation != 1 || rolledBack.LastSwitch == nil ||
		rolledBack.LastSwitch.Outcome != preferences.HomeSwitchRolledBack {
		t.Fatalf("rolled-back preferences = %#v", rolledBack)
	}
	recoveryRequest := waitForQuotaHomeRequest(t, requests)
	assertQuotaRuntimeHome(t, recoveryRequest, homeA)
	select {
	case duplicate := <-requests:
		t.Fatalf("rollback issued duplicate recovery request to %#v", duplicate)
	case <-time.After(100 * time.Millisecond):
	}

	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

type quotaHomeRequestEvent struct {
	home string
	kind string
}

type quotaStartupHomeRuntime struct {
	status   preferences.BootstrapStatus
	startErr error
	err      error
}

func (runtime *quotaStartupHomeRuntime) Drain(context.Context, uint64) error {
	return nil
}

func (runtime *quotaStartupHomeRuntime) StartBootstrap(
	context.Context,
	preferences.BootstrapRequest,
) error {
	return runtime.startErr
}

func (runtime *quotaStartupHomeRuntime) BootstrapStatus(
	context.Context,
	string,
	uint64,
) (preferences.BootstrapStatus, error) {
	return runtime.status, runtime.err
}

func (runtime *quotaStartupHomeRuntime) Resume(context.Context, uint64) error {
	return nil
}

func openQuotaRuntimeStore(t testing.TB) (*storesqlite.Store, *store.Repository) {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("os.Mkdir(database directory) error = %v", err)
	}
	database, err := storesqlite.Open(context.Background(), storesqlite.Config{
		Path: filepath.Join(directory, "codex-pulse.db"),
	})
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	repository := store.NewRepository(database)
	if err := repository.EnsureApplicationSchema(context.Background()); err != nil {
		_ = database.Close(context.Background())
		t.Fatalf("EnsureApplicationSchema() error = %v", err)
	}
	return database, repository
}

func enabledQuotaRuntimePreferences(t testing.TB, home string) preferences.Snapshot {
	t.Helper()
	snapshot := quotaRuntimePreferencesForHome(t, home)
	snapshot.SchemaVersion = preferences.CurrentPreferencesSchemaVersion
	snapshot.Revision = 1
	snapshot.Onboarding = preferences.OnboardingPreferences{Version: 1, Completed: true}
	snapshot.Online = preferences.OnlinePreferences{QuotaEnabled: true, ResetCreditsEnabled: true}
	snapshot.Refresh = preferences.DefaultRefreshPreferences()
	return snapshot
}

func confirmedQuotaRuntimeFileStore(
	t testing.TB,
	home string,
	quotaEnabled bool,
	resetCreditsEnabled bool,
) *preferences.FileStore {
	t.Helper()
	snapshot := quotaRuntimePreferencesForHome(t, home)
	store, err := preferences.NewFileStore(filepath.Join(t.TempDir(), "private", "preferences.json"))
	if err != nil {
		t.Fatalf("preferences.NewFileStore() error = %v", err)
	}
	if err := store.Confirm(context.Background(), preferences.OnboardingSnapshot{
		SchemaVersion:       preferences.CurrentSchemaVersion,
		OnboardingVersion:   preferences.CurrentOnboardingVersion,
		OnboardingCompleted: true,
		CodexHome:           snapshot.CodexHome.Source,
		OnlineQuotaEnabled:  quotaEnabled,
		ResetCreditsEnabled: resetCreditsEnabled,
	}); err != nil {
		t.Fatalf("preferences.FileStore.Confirm() error = %v", err)
	}
	return store
}

func installQuotaRuntimePendingResume(t testing.TB, preferenceStore *preferences.FileStore) {
	t.Helper()
	current, err := preferenceStore.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences(before pending resume) error = %v", err)
	}
	next := current
	next.Revision++
	next.PendingResume = &preferences.HomeResumeJournal{
		SwitchID:         "home-switch:quota-runtime-pending-resume",
		AttemptID:        strings.Repeat("b", 32),
		Generation:       current.CodexHome.Generation,
		TargetGeneration: current.CodexHome.Generation + 1,
		Strategy:         preferences.HomeSwitchClearAndRebuild,
		StartedAtMS:      quotaRuntimeNowMS,
	}
	if err := preferenceStore.CompareAndSwap(context.Background(), current.Revision, next); err != nil {
		t.Fatalf("CompareAndSwap(pending resume) error = %v", err)
	}
}

func installQuotaRuntimePendingSwitch(
	t testing.TB,
	preferenceStore *preferences.FileStore,
	targetHome string,
) preferences.HomeSwitchJournal {
	t.Helper()
	current, err := preferenceStore.LoadPreferences(context.Background())
	if err != nil {
		t.Fatalf("LoadPreferences(before pending switch) error = %v", err)
	}
	target := *quotaRuntimePreferencesForHome(t, targetHome).CodexHome
	target.Generation = current.CodexHome.Generation + 1
	target.DataStoreKey = current.CodexHome.DataStoreKey
	pending := preferences.HomeSwitchJournal{
		SwitchID:    "home-switch:quota-runtime-pending-switch",
		AttemptID:   strings.Repeat("c", 32),
		Previous:    *current.CodexHome,
		Target:      target,
		Strategy:    preferences.HomeSwitchClearAndRebuild,
		StartedAtMS: quotaRuntimeNowMS,
	}
	next := current
	next.Revision++
	next.CodexHome = preferences.CodexHomePointer(target)
	next.PendingSwitch = &pending
	if err := preferenceStore.CompareAndSwap(context.Background(), current.Revision, next); err != nil {
		t.Fatalf("CompareAndSwap(pending switch) error = %v", err)
	}
	return pending
}

func waitForQuotaRuntimeState(
	t testing.TB,
	repository *store.Repository,
	sourceInstanceID string,
	accepted func(store.SourceState) bool,
) {
	t.Helper()
	sourceInstanceID = quotaRuntimeInstance(t, repository, sourceInstanceID)
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := repository.SourceState(context.Background(), sourceInstanceID)
		if err == nil && accepted(state) {
			return
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("SourceState(%q) error = %v", sourceInstanceID, err)
		}
		select {
		case <-deadline.C:
			t.Fatalf("SourceState(%q) did not reach expected state", sourceInstanceID)
		case <-ticker.C:
		}
	}
}

func waitForQuotaRuntimeBinding(
	t testing.TB,
	repository *store.Repository,
	accepted func(store.CodexAccountBinding) bool,
) store.CodexAccountBinding {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		binding, err := repository.CodexAccountBinding(t.Context())
		if err != nil {
			t.Fatalf("CodexAccountBinding() error = %v", err)
		}
		if accepted(binding) {
			return binding
		}
		select {
		case <-deadline.C:
			t.Fatalf("CodexAccountBinding() did not reach expected state: %#v", binding)
		case <-ticker.C:
		}
	}
}

func assertQuotaRuntimeLifecycleGeneration(
	t testing.TB,
	database *storesqlite.Store,
	wantGeneration int64,
) {
	t.Helper()
	lifecycle, err := store.NewRepository(database).SchedulerLifecycle(context.Background())
	if err != nil || lifecycle.HomeGeneration != wantGeneration ||
		lifecycle.Transition != store.LifecycleTransitionSteady ||
		lifecycle.SourceState != store.LifecycleSourceAvailable {
		t.Fatalf("SchedulerLifecycle(generation %d) = %#v, %v", wantGeneration, lifecycle, err)
	}
}

func waitForQuotaRuntimeRequest(t testing.TB, requests <-chan string, want string) {
	t.Helper()
	select {
	case endpoint := <-requests:
		if endpoint != want {
			t.Fatalf("quota runtime request = %q, want %q", endpoint, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("quota runtime did not request %q", want)
	}
}

func waitForQuotaRuntimeSchedule(
	t testing.TB,
	repository *store.Repository,
	sourceInstanceID string,
	accepted func(store.SourceRefreshSchedule) bool,
) {
	t.Helper()
	sourceInstanceID = quotaRuntimeInstance(t, repository, sourceInstanceID)
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		schedule, err := repository.SourceRefreshSchedule(context.Background(), sourceInstanceID)
		if err == nil && accepted(schedule) {
			return
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("SourceRefreshSchedule(%q) error = %v", sourceInstanceID, err)
		}
		select {
		case <-deadline.C:
			t.Fatalf("SourceRefreshSchedule(%q) did not reach expected state", sourceInstanceID)
		case <-ticker.C:
		}
	}
}

func waitForQuotaHomeRequest(
	t testing.TB,
	requests <-chan quotaHomeRequestEvent,
) quotaHomeRequestEvent {
	t.Helper()
	select {
	case request := <-requests:
		return request
	case <-time.After(2 * time.Second):
		t.Fatal("quota Home request did not reach transport")
		return quotaHomeRequestEvent{}
	}
}
