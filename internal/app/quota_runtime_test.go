package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/bootstrap"
	quotaonline "github.com/SisyphusSQ/codex-pulse/internal/codex/quota"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	storesqlite "github.com/SisyphusSQ/codex-pulse/internal/store/sqlite"
)

const quotaRuntimeNowMS = int64(1_784_000_000_000)

func TestApplicationQuotaRuntimeStartsEnabledSourcesAndStops(t *testing.T) {
	t.Parallel()

	database, repository := openQuotaRuntimeStore(t)
	home := writeSyntheticAuthHome(t, "synthetic-runtime-access-token")
	initialPreferences := enabledQuotaRuntimePreferences(t, home)
	loader := &quotaRuntimePreferencesLoader{snapshot: initialPreferences}
	requests := make(chan string, 2)
	transportFailures := make(chan error, 1)
	transport := quotaRuntimeRoundTripper(func(request *http.Request) (*http.Response, error) {
		requests <- request.URL.String()
		if request.Header.Get("Authorization") != "Bearer synthetic-runtime-access-token" {
			failure := errors.New("Authorization header was not sourced from the confirmed Home")
			select {
			case transportFailures <- failure:
			default:
			}
			return nil, failure
		}
		body := validQuotaRuntimeUsagePayload()
		if request.URL.String() == quotaonline.WhamResetCreditsEndpoint {
			body = validQuotaRuntimeResetCreditsPayload()
		}
		return quotaRuntimeJSONResponse(body), nil
	})

	runtime, err := startApplicationQuotaRuntime(context.Background(), ApplicationQuotaRuntimeConfig{
		Repository:  repository,
		Preferences: loader,
		Transport:   transport,
		Clock: func() time.Time {
			return time.UnixMilli(quotaRuntimeNowMS).UTC()
		},
	})
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
	if !seen[quotaonline.WhamUsageEndpoint] || !seen[quotaonline.WhamResetCreditsEndpoint] {
		t.Fatalf("startup requests = %#v", seen)
	}
	assertNoQuotaRuntimeTransportFailure(t, transportFailures)

	waitForQuotaRuntimeState(t, repository, store.QuotaSourceInstanceWhamDefault, func(state store.SourceState) bool {
		return state.LastSuccessAtMS != nil && state.LastFailureCode == nil
	})
	waitForQuotaRuntimeState(t, repository, store.ResetCreditsSourceInstanceWhamDefault, func(state store.SourceState) bool {
		return state.LastSuccessAtMS != nil && state.LastFailureCode == nil
	})
	for _, sourceInstanceID := range []string{
		store.QuotaSourceInstanceWhamDefault,
		store.ResetCreditsSourceInstanceWhamDefault,
	} {
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
	restarted, err := startApplicationQuotaRuntime(context.Background(), ApplicationQuotaRuntimeConfig{
		Repository: repository, Preferences: loader, Transport: transport,
		Clock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	})
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

func TestApplicationQuotaRuntimeRecordsMissingCredentialAndManualRecovery(t *testing.T) {
	t.Parallel()

	database, repository := openQuotaRuntimeStore(t)
	home := t.TempDir()
	loader := &quotaRuntimePreferencesLoader{
		snapshot: enabledQuotaRuntimePreferences(t, home),
	}
	transportCalls := make(chan string, 2)
	runtime, err := startApplicationQuotaRuntime(context.Background(), ApplicationQuotaRuntimeConfig{
		Repository:  repository,
		Preferences: loader,
		Transport: quotaRuntimeRoundTripper(func(request *http.Request) (*http.Response, error) {
			transportCalls <- request.URL.String()
			if request.URL.String() == quotaonline.WhamResetCreditsEndpoint {
				return quotaRuntimeJSONResponse(validQuotaRuntimeResetCreditsPayload()), nil
			}
			return quotaRuntimeJSONResponse(validQuotaRuntimeUsagePayload()), nil
		}),
		Clock: func() time.Time {
			return time.UnixMilli(quotaRuntimeNowMS).UTC()
		},
	})
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationQuotaRuntime() = %#v, %v", runtime, err)
	}

	for _, sourceInstanceID := range []string{
		store.QuotaSourceInstanceWhamDefault,
		store.ResetCreditsSourceInstanceWhamDefault,
	} {
		waitForQuotaRuntimeState(t, repository, sourceInstanceID, func(state store.SourceState) bool {
			return state.LastFailureCode != nil && *state.LastFailureCode == store.SourceFailureAuthRequired
		})
		waitForQuotaRuntimeSchedule(t, repository, sourceInstanceID, func(schedule store.SourceRefreshSchedule) bool {
			return schedule.NextDueAtMS == nil && schedule.ActiveClaimID == nil &&
				schedule.Reason == store.RefreshReasonAuthRequired
		})
	}
	select {
	case <-transportCalls:
		t.Fatal("transport ran without credentials")
	default:
	}
	if err := os.WriteFile(
		filepath.Join(home, "auth.json"),
		[]byte(`{"tokens":{"access_token":"synthetic-recovered-access-token"}}`),
		0o600,
	); err != nil {
		t.Fatalf("os.WriteFile(recovered auth.json) error = %v", err)
	}
	for _, source := range []quotaonline.RefreshSource{
		quotaonline.RefreshSourceQuota,
		quotaonline.RefreshSourceResetCredits,
	} {
		if _, err := runtime.RequestRefresh(context.Background(), source, store.RefreshTriggerManual); err != nil {
			t.Fatalf("RequestRefresh(%q) error = %v", source, err)
		}
	}
	waitForQuotaRuntimeRequests(t, transportCalls, 2)
	waitForQuotaRuntimeState(t, repository, store.QuotaSourceInstanceWhamDefault, func(state store.SourceState) bool {
		return state.LastSuccessAtMS != nil && state.LastFailureCode == nil
	})
	waitForQuotaRuntimeState(t, repository, store.ResetCreditsSourceInstanceWhamDefault, func(state store.SourceState) bool {
		return state.LastSuccessAtMS != nil && state.LastFailureCode == nil
	})

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
	started := make(chan *http.Request, 1)
	transport := quotaRuntimeRoundTripper(func(request *http.Request) (*http.Response, error) {
		started <- request
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	runtime, err := startApplicationQuotaRuntime(context.Background(), ApplicationQuotaRuntimeConfig{
		Repository: repository, Preferences: loader, Transport: transport,
		Clock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	})
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationQuotaRuntime() = %#v, %v", runtime, err)
	}
	var retained *http.Request
	select {
	case retained = <-started:
		if retained.Header.Get("Authorization") != "Bearer synthetic-cancel-access-token" {
			t.Fatal("inflight request did not carry the leased credential")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("startup request did not reach transport")
	}

	closeContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runtime.Close(closeContext); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	if retained.Header.Get("Authorization") != "" {
		t.Fatal("Authorization header remained after cancellation")
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
	state, err := repository.SourceState(context.Background(), store.QuotaSourceInstanceWhamDefault)
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
	runtime, err := startApplicationQuotaRuntime(context.Background(), ApplicationQuotaRuntimeConfig{
		Repository: repository, Preferences: loader,
		Transport: quotaRuntimeRoundTripper(func(request *http.Request) (*http.Response, error) {
			requests <- request.URL.String()
			return quotaRuntimeJSONResponse(validQuotaRuntimeUsagePayload()), nil
		}),
		Clock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	})
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationQuotaRuntime() = %#v, %v", runtime, err)
	}
	for _, sourceInstanceID := range []string{
		store.QuotaSourceInstanceWhamDefault,
		store.ResetCreditsSourceInstanceWhamDefault,
	} {
		waitForQuotaRuntimeSchedule(t, repository, sourceInstanceID, func(schedule store.SourceRefreshSchedule) bool {
			return schedule.Reason == store.RefreshReasonDisabled && schedule.NextDueAtMS == nil
		})
	}
	for _, sourceInstanceID := range []string{
		store.QuotaSourceInstanceWhamDefault,
		store.ResetCreditsSourceInstanceWhamDefault,
	} {
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
		if endpoint != quotaonline.WhamUsageEndpoint {
			t.Fatalf("enabled source endpoint = %q", endpoint)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("newly enabled Quota source did not enter startup due")
	}
	stateBeforeDisable, err := repository.SourceState(context.Background(), store.QuotaSourceInstanceWhamDefault)
	if err != nil || stateBeforeDisable.LastSuccessAtMS == nil {
		t.Fatalf("SourceState(before disable) = %#v, %v", stateBeforeDisable, err)
	}
	attemptsBeforeDisable, err := repository.ListSourceAttempts(
		context.Background(), store.QuotaSourceInstanceWhamDefault, 10,
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
	schedule, err := repository.SourceRefreshSchedule(context.Background(), store.QuotaSourceInstanceWhamDefault)
	if err != nil || schedule.Reason != store.RefreshReasonDisabled || schedule.NextDueAtMS != nil {
		t.Fatalf("SourceRefreshSchedule(disabled quota) = %#v, %v", schedule, err)
	}
	stateAfterDisable, err := repository.SourceState(context.Background(), store.QuotaSourceInstanceWhamDefault)
	if err != nil || stateAfterDisable.LastSuccessAtMS == nil ||
		*stateAfterDisable.LastSuccessAtMS != *stateBeforeDisable.LastSuccessAtMS {
		t.Fatalf("SourceState(after disable) = %#v, %v", stateAfterDisable, err)
	}
	attemptsAfterDisable, err := repository.ListSourceAttempts(
		context.Background(), store.QuotaSourceInstanceWhamDefault, 10,
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
	runtime, err := startApplicationQuotaRuntime(context.Background(), ApplicationQuotaRuntimeConfig{
		Repository: repository, Preferences: loader,
		Transport: quotaRuntimeRoundTripper(func(request *http.Request) (*http.Response, error) {
			return quotaRuntimeJSONResponse(validQuotaRuntimeUsagePayload()), nil
		}),
		Clock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		hooks: quotaRuntimeHooks{
			afterAdmission: func() {
				admissionOnce.Do(func() {
					close(admitted)
					<-releaseAdmission
				})
			},
			afterAdmissionSealed: func() { close(admissionSealed) },
		},
	})
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationQuotaRuntime() = %#v, %v", runtime, err)
	}
	for _, sourceInstanceID := range []string{
		store.QuotaSourceInstanceWhamDefault,
		store.ResetCreditsSourceInstanceWhamDefault,
	} {
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
	runtime, err := startApplicationQuotaRuntime(context.Background(), ApplicationQuotaRuntimeConfig{
		Repository: repository, Preferences: loader,
		Transport: quotaRuntimeRoundTripper(func(request *http.Request) (*http.Response, error) {
			return quotaRuntimeJSONResponse(validQuotaRuntimeUsagePayload()), nil
		}),
		Clock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
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
	})
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
	runtime, err := startApplicationQuotaRuntime(context.Background(), ApplicationQuotaRuntimeConfig{
		Repository: repository, Preferences: loader,
		Transport: quotaRuntimeRoundTripper(func(request *http.Request) (*http.Response, error) {
			return quotaRuntimeJSONResponse(validQuotaRuntimeUsagePayload()), nil
		}),
		Clock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
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
	})
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
	transportFailures := make(chan error, 1)
	transport := quotaRuntimeRoundTripper(func(request *http.Request) (*http.Response, error) {
		requests <- request.URL.String()
		if request.Header.Get("Authorization") != "Bearer synthetic-lifecycle-access-token" {
			failure := errors.New("Authorization header was not sourced from the confirmed Home")
			select {
			case transportFailures <- failure:
			default:
			}
			return nil, failure
		}
		if request.URL.String() == quotaonline.WhamResetCreditsEndpoint {
			return quotaRuntimeJSONResponse(validQuotaRuntimeResetCreditsPayload()), nil
		}
		return quotaRuntimeJSONResponse(validQuotaRuntimeUsagePayload()), nil
	})
	var nowMS atomic.Int64
	nowMS.Store(quotaRuntimeNowMS)
	runtime, err := startApplicationLifecycleRuntime(context.Background(), ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: loader,
		EventTimeout: time.Second, QuotaTransport: transport,
		QuotaClock: func() time.Time { return time.UnixMilli(nowMS.Load()).UTC() },
	})
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationLifecycleRuntime() = %#v, %v", runtime, err)
	}
	waitForQuotaRuntimeRequests(t, requests, 2)
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
	waitForQuotaRuntimeRequests(t, requests, 2)
	for _, sourceInstanceID := range []string{
		store.QuotaSourceInstanceWhamDefault,
		store.ResetCreditsSourceInstanceWhamDefault,
	} {
		waitForQuotaRuntimeState(t, repository, sourceInstanceID, func(state store.SourceState) bool {
			return state.LastSuccessAtMS != nil && *state.LastSuccessAtMS == nowMS.Load() && state.LastFailureCode == nil
		})
	}

	disabled := initialPreferences
	disabled.Revision++
	disabled.Online = preferences.OnlinePreferences{}
	loader.setSnapshot(disabled)
	if err := runtime.ReconcileQuotaPreferences(context.Background()); err != nil {
		t.Fatalf("ReconcileQuotaPreferences(disabled) error = %v", err)
	}
	for _, sourceInstanceID := range []string{
		store.QuotaSourceInstanceWhamDefault,
		store.ResetCreditsSourceInstanceWhamDefault,
	} {
		schedule, scheduleErr := repository.SourceRefreshSchedule(context.Background(), sourceInstanceID)
		if scheduleErr != nil || schedule.Reason != store.RefreshReasonDisabled || schedule.NextDueAtMS != nil {
			t.Fatalf("SourceRefreshSchedule(%q) = %#v, %v", sourceInstanceID, schedule, scheduleErr)
		}
	}

	enabled := disabled
	enabled.Revision++
	enabled.Online = preferences.OnlinePreferences{QuotaEnabled: true, ResetCreditsEnabled: true}
	loader.setSnapshot(enabled)
	if err := runtime.ReconcileQuotaPreferences(context.Background()); err != nil {
		t.Fatalf("ReconcileQuotaPreferences(enabled) error = %v", err)
	}
	if _, err := runtime.RequestQuotaRefresh(context.Background(), quotaonline.RefreshSourceQuota); err != nil {
		t.Fatalf("RequestQuotaRefresh() error = %v", err)
	}
	select {
	case endpoint := <-requests:
		if endpoint != quotaonline.WhamUsageEndpoint {
			t.Fatalf("manual refresh endpoint = %q", endpoint)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("manual refresh did not call Quota endpoint")
	}
	assertNoQuotaRuntimeTransportFailure(t, transportFailures)

	closeContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runtime.Close(closeContext); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	if err := database.Close(closeContext); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
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
	runtime, err := startApplicationLifecycleRuntime(context.Background(), ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		Invalidation: invalidation,
		EventTimeout: time.Second,
		QuotaTransport: quotaRuntimeRoundTripper(func(request *http.Request) (*http.Response, error) {
			requests <- request.URL.String()
			if request.URL.String() == quotaonline.WhamResetCreditsEndpoint {
				return quotaRuntimeJSONResponse(validQuotaRuntimeResetCreditsPayload()), nil
			}
			return quotaRuntimeJSONResponse(validQuotaRuntimeUsagePayload()), nil
		}),
		QuotaClock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	})
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationLifecycleRuntime() = %#v, %v", runtime, err)
	}
	waitForQuotaRuntimeRequests(t, requests, 2)
	for _, sourceInstanceID := range []string{
		store.QuotaSourceInstanceWhamDefault,
		store.ResetCreditsSourceInstanceWhamDefault,
	} {
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
	if invalidation.count(QueryInvalidationSettings) != 1 ||
		invalidation.count(QueryInvalidationQuota) != 1 {
		t.Fatalf(
			"settings invalidation counts = settings:%d quota:%d",
			invalidation.count(QueryInvalidationSettings),
			invalidation.count(QueryInvalidationQuota),
		)
	}
	readback, err := preferenceStore.LoadPreferences(context.Background())
	if err != nil || readback.Revision != committed.Revision || readback.Online != committed.Online {
		t.Fatalf("LoadPreferences(after settings) = %#v, %v", readback, err)
	}
	for _, sourceInstanceID := range []string{
		store.QuotaSourceInstanceWhamDefault,
		store.ResetCreditsSourceInstanceWhamDefault,
	} {
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
	runtime, err := startApplicationLifecycleRuntime(context.Background(), ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		Invalidation: invalidation,
		EventTimeout: time.Second,
		QuotaTransport: quotaRuntimeRoundTripper(func(request *http.Request) (*http.Response, error) {
			return quotaRuntimeJSONResponse(validQuotaRuntimeUsagePayload()), nil
		}),
		QuotaClock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	})
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationLifecycleRuntime() = %#v, %v", runtime, err)
	}
	for _, sourceInstanceID := range []string{
		store.QuotaSourceInstanceWhamDefault,
		store.ResetCreditsSourceInstanceWhamDefault,
	} {
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
	if invalidation.count(QueryInvalidationSettings) != 1 ||
		invalidation.count(QueryInvalidationQuota) != 1 {
		t.Fatalf(
			"post-commit invalidation counts = settings:%d quota:%d",
			invalidation.count(QueryInvalidationSettings),
			invalidation.count(QueryInvalidationQuota),
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

	database, _ := openQuotaRuntimeStore(t)
	home := writeSyntheticAuthHome(t, "synthetic-close-settings-access-token")
	for _, directory := range []string{"sessions", "archived_sessions"} {
		if err := os.Mkdir(filepath.Join(home, directory), 0o700); err != nil {
			t.Fatalf("os.Mkdir(%s) error = %v", directory, err)
		}
	}
	preferenceStore := confirmedQuotaRuntimeFileStore(t, home, false, false)
	runtime, err := startApplicationLifecycleRuntime(context.Background(), ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		EventTimeout: time.Second,
		QuotaTransport: quotaRuntimeRoundTripper(func(request *http.Request) (*http.Response, error) {
			return quotaRuntimeJSONResponse(validQuotaRuntimeUsagePayload()), nil
		}),
		QuotaClock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	})
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

	database, _ := openQuotaRuntimeStore(t)
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
	runtime, err := startApplicationLifecycleRuntime(context.Background(), ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		EventTimeout: time.Second,
		QuotaTransport: quotaRuntimeRoundTripper(func(request *http.Request) (*http.Response, error) {
			return quotaRuntimeJSONResponse(validQuotaRuntimeUsagePayload()), nil
		}),
		QuotaClock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		quotaHooks: quotaRuntimeHooks{
			afterAdmission: func() {
				admissionOnce.Do(func() {
					close(settingsAdmitted)
					<-releaseSettings
				})
			},
		},
	})
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

	database, _ := openQuotaRuntimeStore(t)
	home := writeSyntheticAuthHome(t, "synthetic-pending-resume-token")
	for _, directory := range []string{"sessions", "archived_sessions"} {
		if err := os.Mkdir(filepath.Join(home, directory), 0o700); err != nil {
			t.Fatalf("os.Mkdir(%s) error = %v", directory, err)
		}
	}
	preferenceStore := confirmedQuotaRuntimeFileStore(t, home, true, false)
	installQuotaRuntimePendingResume(t, preferenceStore)
	requests := make(chan quotaHomeRequestEvent, 2)
	runtime, err := startApplicationLifecycleRuntime(context.Background(), ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		EventTimeout: time.Second,
		QuotaTransport: quotaRuntimeRoundTripper(func(request *http.Request) (*http.Response, error) {
			requests <- quotaHomeRequestEvent{
				authorization: request.Header.Get("Authorization"),
				request:       request,
			}
			return quotaRuntimeJSONResponse(validQuotaRuntimeUsagePayload()), nil
		}),
		QuotaClock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	})
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
	if request.authorization != "Bearer synthetic-pending-resume-token" {
		t.Fatalf("recovered Authorization = %q", request.authorization)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func TestApplicationLifecycleRuntimeRollsBackPendingSwitchBeforeQuotaStart(t *testing.T) {
	t.Parallel()

	database, _ := openQuotaRuntimeStore(t)
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
	runtime, err := startApplicationLifecycleRuntime(context.Background(), ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		EventTimeout: time.Second,
		QuotaTransport: quotaRuntimeRoundTripper(func(request *http.Request) (*http.Response, error) {
			requests <- quotaHomeRequestEvent{
				authorization: request.Header.Get("Authorization"),
				request:       request,
			}
			return quotaRuntimeJSONResponse(validQuotaRuntimeUsagePayload()), nil
		}),
		QuotaClock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	})
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
	if request.authorization != "Bearer synthetic-pending-old-token" {
		t.Fatalf("rollback Authorization = %q", request.authorization)
	}
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
	runtime, err := startApplicationLifecycleRuntime(context.Background(), ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		EventTimeout: time.Second,
		QuotaTransport: quotaRuntimeRoundTripper(func(request *http.Request) (*http.Response, error) {
			requests <- quotaHomeRequestEvent{
				authorization: request.Header.Get("Authorization"),
				request:       request,
			}
			return quotaRuntimeJSONResponse(validQuotaRuntimeUsagePayload()), nil
		}),
		QuotaClock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	})
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
	if request.authorization != "Bearer synthetic-finalize-target-token" {
		t.Fatalf("finalize Authorization = %q", request.authorization)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

func TestApplicationLifecycleRuntimeKeepsUnknownPendingSwitchSuspended(t *testing.T) {
	t.Parallel()

	database, _ := openQuotaRuntimeStore(t)
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
	runtime, err := startApplicationLifecycleRuntime(context.Background(), ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		EventTimeout: time.Second,
		QuotaTransport: quotaRuntimeRoundTripper(func(request *http.Request) (*http.Response, error) {
			transportCalls <- struct{}{}
			return quotaRuntimeJSONResponse(validQuotaRuntimeUsagePayload()), nil
		}),
		QuotaClock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		homeRuntime: &quotaStartupHomeRuntime{
			status: preferences.BootstrapStatusNotStarted,
			err:    statusFailure,
		},
	})
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
	var blockOldRequest atomic.Bool
	var nowMS atomic.Int64
	nowMS.Store(quotaRuntimeNowMS)
	transport := quotaRuntimeRoundTripper(func(request *http.Request) (*http.Response, error) {
		authorization := request.Header.Get("Authorization")
		requests <- quotaHomeRequestEvent{authorization: authorization, request: request}
		if blockOldRequest.Load() && authorization == "Bearer synthetic-home-a-access-token" {
			oldRequestStarted <- struct{}{}
			<-releaseOldRequest
		}
		return quotaRuntimeJSONResponse(validQuotaRuntimeUsagePayload()), nil
	})
	runtime, err := startApplicationLifecycleRuntime(context.Background(), ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		EventTimeout: time.Second, QuotaTransport: transport,
		QuotaClock: func() time.Time { return time.UnixMilli(nowMS.Load()).UTC() },
	})
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationLifecycleRuntime() = %#v, %v", runtime, err)
	}
	initialRequest := waitForQuotaHomeRequest(t, requests)
	if initialRequest.authorization != "Bearer synthetic-home-a-access-token" {
		t.Fatalf("startup Authorization = %q", initialRequest.authorization)
	}
	waitForQuotaRuntimeSchedule(t, repository, store.QuotaSourceInstanceWhamDefault, func(schedule store.SourceRefreshSchedule) bool {
		return schedule.NextDueAtMS != nil && schedule.ActiveClaimID == nil
	})
	plan, err := runtime.PlanQuotaHomeSwitch(
		context.Background(), homeB, preferences.HomeSwitchClearAndRebuild,
	)
	if err != nil {
		t.Fatalf("PlanQuotaHomeSwitch() error = %v", err)
	}
	blockOldRequest.Store(true)
	manualDone := make(chan error, 1)
	go func() {
		_, manualErr := runtime.RequestQuotaRefresh(context.Background(), quotaonline.RefreshSourceQuota)
		manualDone <- manualErr
	}()
	oldRequest := waitForQuotaHomeRequest(t, requests)
	if oldRequest.authorization != "Bearer synthetic-home-a-access-token" {
		t.Fatalf("old manual Authorization = %q", oldRequest.authorization)
	}
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
	if oldRequest.request.Header.Get("Authorization") != "" {
		t.Fatal("old request retained Authorization after drain")
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
	blockOldRequest.Store(false)
	nowMS.Store(quotaRuntimeNowMS + 61*time.Second.Milliseconds())
	if _, err := runtime.RequestQuotaRefresh(context.Background(), quotaonline.RefreshSourceQuota); err != nil {
		t.Fatalf("RequestQuotaRefresh(new Home) error = %v", err)
	}
	newRequest := waitForQuotaHomeRequest(t, requests)
	if newRequest.authorization != "Bearer synthetic-home-b-access-token" {
		t.Fatalf("new Home Authorization = %q", newRequest.authorization)
	}

	closeContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := runtime.Close(closeContext); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	restarted, err := startApplicationLifecycleRuntime(context.Background(), ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		EventTimeout: time.Second, QuotaTransport: transport,
		QuotaClock: func() time.Time { return time.UnixMilli(nowMS.Load()).UTC() },
	})
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

func TestApplicationLifecycleRuntimeHomeSwitchRearmsCredentialPausedQuotaSources(t *testing.T) {
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
	requests := make(chan quotaHomeRequestEvent, 2)
	transport := quotaRuntimeRoundTripper(func(request *http.Request) (*http.Response, error) {
		requests <- quotaHomeRequestEvent{
			authorization: request.Header.Get("Authorization"),
			request:       request,
		}
		if request.URL.String() == quotaonline.WhamResetCreditsEndpoint {
			return quotaRuntimeJSONResponse(validQuotaRuntimeResetCreditsPayload()), nil
		}
		return quotaRuntimeJSONResponse(validQuotaRuntimeUsagePayload()), nil
	})
	runtime, err := startApplicationLifecycleRuntime(context.Background(), ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		EventTimeout: time.Second, QuotaTransport: transport,
		QuotaClock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
	})
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationLifecycleRuntime() = %#v, %v", runtime, err)
	}
	for _, sourceInstanceID := range []string{
		store.QuotaSourceInstanceWhamDefault,
		store.ResetCreditsSourceInstanceWhamDefault,
	} {
		waitForQuotaRuntimeState(t, repository, sourceInstanceID, func(state store.SourceState) bool {
			return state.LastFailureCode != nil &&
				*state.LastFailureCode == store.SourceFailureAuthRequired
		})
		waitForQuotaRuntimeSchedule(t, repository, sourceInstanceID, func(schedule store.SourceRefreshSchedule) bool {
			return schedule.NextDueAtMS == nil && schedule.ActiveClaimID == nil &&
				schedule.Reason == store.RefreshReasonAuthRequired
		})
	}

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
		if request.authorization != "Bearer synthetic-home-switch-recovery-token" {
			t.Fatalf("Home switch Authorization = %q", request.authorization)
		}
		seen[request.request.URL.String()] = true
	}
	if !seen[quotaonline.WhamUsageEndpoint] || !seen[quotaonline.WhamResetCreditsEndpoint] {
		t.Fatalf("Home switch requests = %#v", seen)
	}
	for _, sourceInstanceID := range []string{
		store.QuotaSourceInstanceWhamDefault,
		store.ResetCreditsSourceInstanceWhamDefault,
	} {
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
	transport := quotaRuntimeRoundTripper(func(request *http.Request) (*http.Response, error) {
		requests <- quotaHomeRequestEvent{
			authorization: request.Header.Get("Authorization"),
			request:       request,
		}
		return quotaRuntimeJSONResponse(validQuotaRuntimeUsagePayload()), nil
	})
	startFailure := errors.New("synthetic Home bootstrap did not start")
	runtime, err := startApplicationLifecycleRuntime(context.Background(), ApplicationLifecycleRuntimeConfig{
		Database: database, Preferences: preferenceStore,
		EventTimeout: time.Second, QuotaTransport: transport,
		QuotaClock: func() time.Time { return time.UnixMilli(quotaRuntimeNowMS).UTC() },
		homeRuntime: &quotaStartupHomeRuntime{
			status:   preferences.BootstrapStatusNotStarted,
			startErr: startFailure,
		},
	})
	if err != nil || runtime == nil {
		t.Fatalf("startApplicationLifecycleRuntime() = %#v, %v", runtime, err)
	}
	initialRequest := waitForQuotaHomeRequest(t, requests)
	if initialRequest.authorization != "Bearer synthetic-home-switch-rollback-a-token" {
		t.Fatalf("initial Authorization = %q", initialRequest.authorization)
	}
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
	if recoveryRequest.authorization != "Bearer synthetic-home-switch-rollback-a-token" {
		t.Fatalf("rollback Authorization = %q", recoveryRequest.authorization)
	}
	select {
	case duplicate := <-requests:
		t.Fatalf("rollback issued duplicate recovery request to %q", duplicate.request.URL.String())
	case <-time.After(100 * time.Millisecond):
	}

	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("runtime.Close() error = %v", err)
	}
	if err := database.Close(context.Background()); err != nil {
		t.Fatalf("database.Close() error = %v", err)
	}
}

type quotaRuntimeRoundTripper func(*http.Request) (*http.Response, error)

type quotaHomeRequestEvent struct {
	authorization string
	request       *http.Request
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

func (roundTripper quotaRuntimeRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
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
	target := quotaRuntimePreferencesForHome(t, targetHome).CodexHome
	target.Generation = current.CodexHome.Generation + 1
	target.DataStoreKey = current.CodexHome.DataStoreKey
	pending := preferences.HomeSwitchJournal{
		SwitchID:    "home-switch:quota-runtime-pending-switch",
		AttemptID:   strings.Repeat("c", 32),
		Previous:    current.CodexHome,
		Target:      target,
		Strategy:    preferences.HomeSwitchClearAndRebuild,
		StartedAtMS: quotaRuntimeNowMS,
	}
	next := current
	next.Revision++
	next.CodexHome = target
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

func waitForQuotaRuntimeRequests(t testing.TB, requests <-chan string, count int) {
	t.Helper()
	seen := make(map[string]int, 2)
	for received := 0; received < count; received++ {
		select {
		case endpoint := <-requests:
			seen[endpoint]++
		case <-time.After(2 * time.Second):
			t.Fatalf("received %d/%d quota runtime requests: %#v", received, count, seen)
		}
	}
	if seen[quotaonline.WhamUsageEndpoint] != 1 || seen[quotaonline.WhamResetCreditsEndpoint] != 1 {
		t.Fatalf("quota runtime requests = %#v", seen)
	}
}

func waitForQuotaRuntimeSchedule(
	t testing.TB,
	repository *store.Repository,
	sourceInstanceID string,
	accepted func(store.SourceRefreshSchedule) bool,
) {
	t.Helper()
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

func assertNoQuotaRuntimeTransportFailure(t testing.TB, failures <-chan error) {
	t.Helper()
	select {
	case err := <-failures:
		t.Fatalf("quota runtime transport error = %v", err)
	default:
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

func quotaRuntimeJSONResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}

func validQuotaRuntimeUsagePayload() string {
	return fmt.Sprintf(`{
  "plan_type": "team",
  "rate_limit": {
    "allowed": true,
    "limit_reached": false,
    "primary_window": {
      "used_percent": 25,
      "limit_window_seconds": 18000,
      "reset_after_seconds": 3600,
      "reset_at": %d
    },
    "secondary_window": {
      "used_percent": 40,
      "limit_window_seconds": 604800,
      "reset_after_seconds": 604800,
      "reset_at": %d
    }
  },
  "credits": {"unlimited": false, "balance": "0"}
}`, quotaRuntimeNowMS/1000+3600, quotaRuntimeNowMS/1000+604800)
}

func validQuotaRuntimeResetCreditsPayload() string {
	return `{
  "available_count": 1,
  "credits": [{
    "id": "RateLimitResetCredit_runtime-synthetic",
    "status": "available",
    "reset_type": "codex_rate_limits",
    "granted_at": "2026-07-15T10:00:00Z",
    "expires_at": "2026-07-30T13:00:00Z"
  }]
}`
}
