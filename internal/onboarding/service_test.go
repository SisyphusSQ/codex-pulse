package onboarding

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
)

func TestServiceDetectOrdersDeduplicatesAndDescribesPrivacy(t *testing.T) {
	t.Parallel()

	probe := newFakeProbe(map[string][]probeResult{
		"/env":      {{metadata: homeMetadata("/env", "1", 1, 2, 20)}},
		"/default":  {{metadata: homeMetadata("/default", "1", 2, 3, 30)}},
		"/selected": {{metadata: homeMetadata("/selected", "1", 3, 4, 40)}},
	})
	service := newTestService(t, probe, &fakeStore{}, "/env/", "/default/./", "/data/tracker.sqlite")
	state, err := service.Detect(context.Background(), "/selected//")
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if state.Phase != PhaseAwaitingConfirmation {
		t.Fatalf("Detect().Phase = %q, want %q", state.Phase, PhaseAwaitingConfirmation)
	}
	if len(state.Candidates) != 3 {
		t.Fatalf("len(Candidates) = %d, want 3", len(state.Candidates))
	}
	for index, want := range []struct {
		source CandidateSource
		path   string
	}{{CandidateSourceEnvironment, "/env"}, {CandidateSourceDefault, "/default"}, {CandidateSourceSelected, "/selected"}} {
		candidate := state.Candidates[index]
		if candidate.Source != want.source || candidate.Path != want.path ||
			candidate.Status != CandidateStatusReady || candidate.Reason != CandidateReasonNone ||
			candidate.ID == "" || candidate.Retryable {
			t.Fatalf("Candidates[%d] = %#v, want ready %q %q", index, candidate, want.source, want.path)
		}
	}
	privacy := state.Privacy
	if privacy.TrackerDatabasePath != "/data/tracker.sqlite" || !privacy.ReadsSessionFiles ||
		privacy.StoresContent || !privacy.OnlineTokenInMemory {
		t.Fatalf("Privacy = %#v", privacy)
	}
	for _, phrase := range []string{"本机 SQLite", "不保存", "仅驻内存", "quota", "reset credits"} {
		if !strings.Contains(privacy.BodyZH, phrase) {
			t.Fatalf("Privacy.BodyZH = %q, want phrase %q", privacy.BodyZH, phrase)
		}
	}
}

func TestServiceDetectStableIDIgnoresLiveAppendAndCanonicalDeduplicates(t *testing.T) {
	t.Parallel()

	probe := newFakeProbe(map[string][]probeResult{
		"/alias": {
			{metadata: homeMetadata("/physical", "7", 11, 1, 10)},
			{metadata: homeMetadata("/physical", "7", 11, 9, 999)},
		},
		"/physical": {{metadata: homeMetadata("/physical", "7", 11, 2, 20)}},
	})
	service := newTestService(t, probe, &fakeStore{}, "/alias", "/physical", "/data/tracker.sqlite")
	first, err := service.Detect(context.Background(), "")
	if err != nil {
		t.Fatalf("Detect(first) error = %v", err)
	}
	if len(first.Candidates) != 1 {
		t.Fatalf("len(first.Candidates) = %d, want canonical dedupe 1", len(first.Candidates))
	}
	second, err := service.Detect(context.Background(), "")
	if err != nil {
		t.Fatalf("Detect(second) error = %v", err)
	}
	if len(second.Candidates) != 1 || second.Candidates[0].ID != first.Candidates[0].ID {
		t.Fatalf("candidate ID changed across append: first=%#v second=%#v", first.Candidates, second.Candidates)
	}
}

func TestServiceDetectClassifiesOnlyAllowlistedFailures(t *testing.T) {
	t.Parallel()

	secretError := errors.New("raw filesystem detail must not escape")
	tests := []struct {
		path      string
		err       error
		status    CandidateStatus
		reason    CandidateReason
		retryable bool
	}{
		{"/missing", logs.ErrInvalidHome, CandidateStatusUnavailable, CandidateReasonMissing, false},
		{"/permission", fs.ErrPermission, CandidateStatusUnavailable, CandidateReasonPermission, true},
		{"/symlink", logs.ErrUnsafeHome, CandidateStatusUnsafe, CandidateReasonUnsafeSymlink, false},
		{"/nested-symlink", logs.ErrUnsafeSource, CandidateStatusUnsafe, CandidateReasonUnsafeSymlink, false},
		{"/unsupported", logs.ErrUnsupportedFile, CandidateStatusUnsafe, CandidateReasonUnsupportedEntry, false},
		{"/changed", logs.ErrChangedDuringScan, CandidateStatusUnavailable, CandidateReasonChanged, true},
		{"/io", secretError, CandidateStatusUnavailable, CandidateReasonIO, true},
	}
	results := make(map[string][]probeResult, len(tests))
	for _, test := range tests {
		results[test.path] = []probeResult{{err: test.err}}
	}
	probe := newFakeProbe(results)
	service := newTestService(t, probe, &fakeStore{}, tests[0].path, tests[1].path, "/data/tracker.sqlite")
	for _, test := range tests[2:] {
		state, err := service.Detect(context.Background(), test.path)
		if err != nil {
			t.Fatalf("Detect(%q) error = %v", test.path, err)
		}
		candidate := candidateByPath(t, state.Candidates, test.path)
		if candidate.Status != test.status || candidate.Reason != test.reason || candidate.Retryable != test.retryable {
			t.Fatalf("candidate(%q) = %#v, want status=%q reason=%q retryable=%v", test.path, candidate, test.status, test.reason, test.retryable)
		}
		if strings.Contains(candidate.ID, secretError.Error()) || strings.Contains(string(candidate.Reason), secretError.Error()) {
			t.Fatalf("candidate leaked raw error: %#v", candidate)
		}
	}
	state, err := service.Detect(context.Background(), "relative")
	if err != nil {
		t.Fatalf("Detect(relative) error = %v", err)
	}
	relative := candidateByPath(t, state.Candidates, "relative")
	if relative.Status != CandidateStatusUnavailable || relative.Reason != CandidateReasonInvalidPath || relative.Retryable {
		t.Fatalf("relative candidate = %#v", relative)
	}
}

func TestServiceDetectNeedsSelectionWhenNoCandidateIsReady(t *testing.T) {
	t.Parallel()

	probe := newFakeProbe(map[string][]probeResult{
		"/env":     {{err: logs.ErrInvalidHome}},
		"/default": {{err: logs.ErrInvalidHome}},
	})
	service := newTestService(t, probe, &fakeStore{}, "/env", "/default", "/data/tracker.sqlite")
	state, err := service.Detect(context.Background(), "")
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if state.Phase != PhaseNeedsSelection || len(state.Candidates) != 2 {
		t.Fatalf("Detect() = %#v, want needs_selection with two candidates", state)
	}
}

func TestServiceConfirmReprobesIdentityAllowsAppendAndPersistsAtomicChoice(t *testing.T) {
	t.Parallel()

	probe := newFakeProbe(map[string][]probeResult{
		"/home": {
			{metadata: homeMetadata("/home", "5", 9, 1, 10)},
			{metadata: homeMetadata("/home", "5", 9, 2, 25)},
		},
		"/default": {{err: logs.ErrInvalidHome}},
	})
	store := &fakeStore{}
	service := newTestService(t, probe, store, "/home", "/default", "/data/tracker.sqlite")
	detected, err := service.Detect(context.Background(), "")
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	confirmation := Confirmation{
		CandidateID: detected.Candidates[0].ID, OnlineQuotaEnabled: false, ResetCreditsEnabled: true,
	}
	confirmed, err := service.Confirm(context.Background(), confirmation)
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if confirmed.Phase != PhaseConfirmed || confirmed.Confirmed == nil {
		t.Fatalf("Confirm() = %#v, want confirmed", confirmed)
	}
	want := preferences.OnboardingSnapshot{
		SchemaVersion:       preferences.CurrentSchemaVersion,
		OnboardingVersion:   preferences.CurrentOnboardingVersion,
		OnboardingCompleted: true,
		CodexHome: preferences.ConfirmedSource{
			Path: "/home", DeviceID: "5", Inode: 9, ConfirmedAtMS: 1_720_000_000_000,
		},
		OnlineQuotaEnabled: false, ResetCreditsEnabled: true,
	}
	if !reflect.DeepEqual(*confirmed.Confirmed, want) || !reflect.DeepEqual(store.snapshot, &want) {
		t.Fatalf("confirmed = %#v store = %#v, want %#v", confirmed.Confirmed, store.snapshot, want)
	}
	if store.confirmCalls != 1 {
		t.Fatalf("store.confirmCalls = %d, want 1", store.confirmCalls)
	}
}

func TestServiceConfirmRejectsStaleOrUnavailableCandidateWithoutWriting(t *testing.T) {
	t.Parallel()

	probe := newFakeProbe(map[string][]probeResult{
		"/home": {
			{metadata: homeMetadata("/home", "5", 9, 1, 10)},
			{metadata: homeMetadata("/home", "5", 10, 1, 10)},
		},
		"/default": {{err: logs.ErrInvalidHome}},
	})
	store := &fakeStore{}
	service := newTestService(t, probe, store, "/home", "/default", "/data/tracker.sqlite")
	detected, err := service.Detect(context.Background(), "")
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if _, err := service.Confirm(context.Background(), Confirmation{CandidateID: "wrong"}); !errors.Is(err, ErrCandidateNotFound) {
		t.Fatalf("Confirm(wrong ID) error = %v, want ErrCandidateNotFound", err)
	}
	state, err := service.Confirm(context.Background(), Confirmation{CandidateID: detected.Candidates[0].ID})
	if !errors.Is(err, ErrCandidateChanged) || state.Phase != PhaseRetryableError || state.Reason != CandidateReasonChanged {
		t.Fatalf("Confirm(stale) = %#v, %v, want retryable changed", state, err)
	}
	if store.confirmCalls != 0 || store.snapshot != nil {
		t.Fatalf("stale confirmation wrote store: calls=%d snapshot=%#v", store.confirmCalls, store.snapshot)
	}
}

func TestServiceConfirmMasksPersistenceFailureAndRecoversUncertainPublish(t *testing.T) {
	t.Parallel()

	newDetectedService := func(t *testing.T, store *fakeStore) (*Service, Confirmation) {
		t.Helper()
		probe := newFakeProbe(map[string][]probeResult{
			"/home":    {{metadata: homeMetadata("/home", "5", 9, 1, 10)}},
			"/default": {{err: logs.ErrInvalidHome}},
		})
		service := newTestService(t, probe, store, "/home", "/default", "/data/tracker.sqlite")
		detected, err := service.Detect(context.Background(), "")
		if err != nil {
			t.Fatalf("Detect() error = %v", err)
		}
		return service, Confirmation{CandidateID: firstReady(t, detected.Candidates).ID}
	}

	t.Run("masked failure", func(t *testing.T) {
		secret := errors.New("private path and implementation detail")
		store := &fakeStore{confirmErr: secret}
		service, confirmation := newDetectedService(t, store)
		state, err := service.Confirm(context.Background(), confirmation)
		if !errors.Is(err, ErrPersistenceFailed) || strings.Contains(err.Error(), secret.Error()) {
			t.Fatalf("Confirm() error = %v, want masked ErrPersistenceFailed", err)
		}
		if state.Phase != PhaseRetryableError || state.Reason != CandidateReasonIO || state.Confirmed != nil {
			t.Fatalf("Confirm(failure) = %#v", state)
		}
	})

	t.Run("durability unknown", func(t *testing.T) {
		store := &fakeStore{confirmAfterWriteErr: preferences.ErrDurabilityUnknown}
		service, confirmation := newDetectedService(t, store)
		state, err := service.Confirm(context.Background(), confirmation)
		if !errors.Is(err, preferences.ErrDurabilityUnknown) {
			t.Fatalf("Confirm() error = %v, want ErrDurabilityUnknown", err)
		}
		if state.Phase != PhaseRetryableError || state.Reason != CandidateReasonDurabilityUnknown || state.Confirmed == nil {
			t.Fatalf("Confirm(durability unknown) = %#v", state)
		}
		resumed, err := service.Resume(context.Background())
		if err != nil || resumed.Phase != PhaseConfirmed || resumed.Confirmed == nil {
			t.Fatalf("Resume(after uncertain publish) = %#v, %v", resumed, err)
		}
	})
}

func TestServiceConfirmPreservesAlreadyConfirmedConflict(t *testing.T) {
	t.Parallel()

	existing := preferences.OnboardingSnapshot{
		SchemaVersion:       preferences.CurrentSchemaVersion,
		OnboardingVersion:   preferences.CurrentOnboardingVersion,
		OnboardingCompleted: true,
		CodexHome: preferences.ConfirmedSource{
			Path: "/other", DeviceID: "8", Inode: 12, ConfirmedAtMS: 1_720_000_000_000,
		},
		OnlineQuotaEnabled: true, ResetCreditsEnabled: true,
	}
	probe := newFakeProbe(map[string][]probeResult{
		"/home":    {{metadata: homeMetadata("/home", "5", 9, 1, 10)}},
		"/default": {{err: logs.ErrInvalidHome}},
	})
	store := &fakeStore{snapshot: &existing}
	service := newTestService(t, probe, store, "/home", "/default", "/data/tracker.sqlite")
	detected, err := service.Detect(context.Background(), "")
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	state, err := service.Confirm(context.Background(), Confirmation{
		CandidateID: firstReady(t, detected.Candidates).ID,
	})
	if !errors.Is(err, preferences.ErrAlreadyConfirmed) {
		t.Fatalf("Confirm(conflict) error = %v, want ErrAlreadyConfirmed", err)
	}
	if state.Phase != PhaseAwaitingConfirmation || state.Reason != CandidateReasonNone || state.Confirmed != nil {
		t.Fatalf("Confirm(conflict) = %#v", state)
	}
	if !reflect.DeepEqual(store.snapshot, &existing) {
		t.Fatalf("conflict changed existing snapshot: %#v", store.snapshot)
	}
}

func TestServiceConfirmCommitPointIgnoresLateCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	store := &cancelAfterCommitStore{cancel: cancel}
	probe := newFakeProbe(map[string][]probeResult{
		"/home":    {{metadata: homeMetadata("/home", "5", 9, 1, 10)}},
		"/default": {{err: logs.ErrInvalidHome}},
	})
	service := newTestService(t, probe, store, "/home", "/default", "/data/tracker.sqlite")
	detected, err := service.Detect(ctx, "")
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	state, err := service.Confirm(ctx, Confirmation{
		CandidateID: firstReady(t, detected.Candidates).ID,
	})
	if err != nil {
		t.Fatalf("Confirm(commit then cancel) error = %v", err)
	}
	if state.Phase != PhaseConfirmed || state.Confirmed == nil {
		t.Fatalf("Confirm(commit then cancel) = %#v, want confirmed", state)
	}
}

func TestServiceConfirmLateCancellationWithUnreadableCommitIsDurabilityUnknown(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	store := &cancelAfterCommitUnreadableStore{cancel: cancel}
	probe := newFakeProbe(map[string][]probeResult{
		"/home":    {{metadata: homeMetadata("/home", "5", 9, 1, 10)}},
		"/default": {{err: logs.ErrInvalidHome}},
	})
	service := newTestService(t, probe, store, "/home", "/default", "/data/tracker.sqlite")
	detected, err := service.Detect(ctx, "")
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	state, err := service.Confirm(ctx, Confirmation{
		CandidateID: firstReady(t, detected.Candidates).ID,
	})
	if !errors.Is(err, preferences.ErrDurabilityUnknown) {
		t.Fatalf("Confirm(commit then cancel with failed readback) error = %v, want ErrDurabilityUnknown", err)
	}
	if state.Phase != PhaseRetryableError || state.Reason != CandidateReasonDurabilityUnknown ||
		state.Confirmed != nil {
		t.Fatalf("Confirm(commit then cancel with failed readback) = %#v", state)
	}
	if canceled := service.Cancel(); canceled.Phase == PhaseCanceled {
		t.Fatalf("Cancel(uncertain commit) = %#v, must preserve recovery boundary", canceled)
	}
}

func TestServiceConfirmCanceledStoreDistinguishesReadbackOutcomes(t *testing.T) {
	t.Parallel()

	newDetectedService := func(
		t *testing.T,
		store Store,
		ctx context.Context,
	) (*Service, Confirmation) {
		t.Helper()
		probe := newFakeProbe(map[string][]probeResult{
			"/home":    {{metadata: homeMetadata("/home", "5", 9, 1, 10)}},
			"/default": {{err: logs.ErrInvalidHome}},
		})
		service := newTestService(t, probe, store, "/home", "/default", "/data/tracker.sqlite")
		detected, err := service.Detect(ctx, "")
		if err != nil {
			t.Fatalf("Detect() error = %v", err)
		}
		return service, Confirmation{CandidateID: firstReady(t, detected.Candidates).ID}
	}

	t.Run("not configured preserves cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		store := &cancelBeforeCommitStore{cancel: cancel}
		service, confirmation := newDetectedService(t, store, ctx)
		state, err := service.Confirm(ctx, confirmation)
		if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(state, State{}) {
			t.Fatalf("Confirm(canceled before commit) = %#v, %v, want zero state/context.Canceled", state, err)
		}
		if canceled := service.Cancel(); canceled.Phase != PhaseCanceled {
			t.Fatalf("Cancel(after confirmed not configured) = %#v, want canceled", canceled)
		}
	})

	t.Run("conflicting snapshot preserves configured boundary", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		conflict := preferences.OnboardingSnapshot{
			SchemaVersion:       preferences.CurrentSchemaVersion,
			OnboardingVersion:   preferences.CurrentOnboardingVersion,
			OnboardingCompleted: true,
			CodexHome: preferences.ConfirmedSource{
				Path: "/other", DeviceID: "7", Inode: 11, ConfirmedAtMS: 1_720_000_000_000,
			},
			OnlineQuotaEnabled: true, ResetCreditsEnabled: true,
		}
		store := &cancelBeforeCommitStore{fakeStore: fakeStore{snapshot: &conflict}, cancel: cancel}
		service, confirmation := newDetectedService(t, store, ctx)
		state, err := service.Confirm(ctx, confirmation)
		if !errors.Is(err, preferences.ErrAlreadyConfirmed) || state.Phase != PhaseAwaitingConfirmation {
			t.Fatalf("Confirm(canceled with conflict) = %#v, %v, want awaiting/ErrAlreadyConfirmed", state, err)
		}
		if canceled := service.Cancel(); canceled.Phase == PhaseCanceled {
			t.Fatalf("Cancel(conflicting configured snapshot) = %#v, must preserve configured boundary", canceled)
		}
	})
}

func TestServiceResumeNotConfiguredClearsDurabilityUnknownLatch(t *testing.T) {
	t.Parallel()

	store := &unavailableThenUnconfiguredStore{}
	probe := newFakeProbe(map[string][]probeResult{
		"/home":    {{metadata: homeMetadata("/home", "5", 9, 1, 10)}},
		"/default": {{err: logs.ErrInvalidHome}},
	})
	service := newTestService(t, probe, store, "/home", "/default", "/data/tracker.sqlite")
	detected, err := service.Detect(context.Background(), "")
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	state, err := service.Confirm(context.Background(), Confirmation{
		CandidateID: firstReady(t, detected.Candidates).ID,
	})
	if !errors.Is(err, preferences.ErrDurabilityUnknown) ||
		state.Phase != PhaseRetryableError || state.Reason != CandidateReasonDurabilityUnknown {
		t.Fatalf("Confirm(unknown) = %#v, %v", state, err)
	}
	resumed, err := service.Resume(context.Background())
	if err != nil || resumed.Phase != PhaseNeedsSelection || resumed.Confirmed != nil {
		t.Fatalf("Resume(not configured) = %#v, %v", resumed, err)
	}
	redetected, err := service.Detect(context.Background(), "")
	if err != nil || redetected.Phase != PhaseAwaitingConfirmation || len(redetected.Candidates) == 0 {
		t.Fatalf("Detect(after authoritative not configured) = %#v, %v", redetected, err)
	}
	if canceled := service.Cancel(); canceled.Phase != PhaseCanceled {
		t.Fatalf("Cancel(after authoritative not configured) = %#v, want canceled", canceled)
	}
}

func TestServiceConcurrentConfirmAndCancelLinearizeAtCommit(t *testing.T) {
	t.Parallel()

	store := newBlockingCommitStore()
	probe := newFakeProbe(map[string][]probeResult{
		"/home":    {{metadata: homeMetadata("/home", "5", 9, 1, 10)}},
		"/default": {{err: logs.ErrInvalidHome}},
	})
	service := newTestService(t, probe, store, "/home", "/default", "/data/tracker.sqlite")
	detected, err := service.Detect(context.Background(), "")
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	candidateID := firstReady(t, detected.Candidates).ID
	confirmResult := make(chan State, 1)
	confirmError := make(chan error, 1)
	go func() {
		state, confirmErr := service.Confirm(context.Background(), Confirmation{
			CandidateID: candidateID,
		})
		confirmResult <- state
		confirmError <- confirmErr
	}()
	<-store.entered
	cancelStarted := make(chan struct{})
	cancelResult := make(chan State, 1)
	go func() {
		close(cancelStarted)
		cancelResult <- service.Cancel()
	}()
	<-cancelStarted
	close(store.release)
	confirmed := <-confirmResult
	if err := <-confirmError; err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	canceled := <-cancelResult
	if confirmed.Phase != PhaseConfirmed || confirmed.Confirmed == nil {
		t.Fatalf("Confirm() = %#v, want confirmed", confirmed)
	}
	if canceled.Phase != PhaseConfirmed || canceled.Confirmed == nil {
		t.Fatalf("Cancel(after commit) = %#v, want confirmed", canceled)
	}
}

func TestServicePropagatesCancellationWithoutWriting(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	probe := newFakeProbe(map[string][]probeResult{
		"/home":    {{err: context.Canceled}},
		"/default": {{err: logs.ErrInvalidHome}},
	})
	service := newTestService(t, probe, store, "/home", "/default", "/data/tracker.sqlite")
	if _, err := service.Detect(context.Background(), ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("Detect() error = %v, want context.Canceled", err)
	}
	if store.confirmCalls != 0 || store.snapshot != nil {
		t.Fatalf("canceled detect wrote store: %#v", store)
	}
}

func TestServiceCancelInvalidatesDetectionAndWritesNothing(t *testing.T) {
	t.Parallel()

	probe := newFakeProbe(map[string][]probeResult{
		"/home":    {{metadata: homeMetadata("/home", "5", 9, 0, 0)}},
		"/default": {{err: logs.ErrInvalidHome}},
	})
	store := &fakeStore{}
	service := newTestService(t, probe, store, "/home", "/default", "/data/tracker.sqlite")
	detected, err := service.Detect(context.Background(), "")
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	canceled := service.Cancel()
	if canceled.Phase != PhaseCanceled || store.confirmCalls != 0 || store.snapshot != nil {
		t.Fatalf("Cancel() = %#v store=%#v", canceled, store)
	}
	if _, err := service.Confirm(context.Background(), Confirmation{CandidateID: detected.Candidates[0].ID}); !errors.Is(err, ErrCandidateNotFound) {
		t.Fatalf("Confirm(after cancel) error = %v, want ErrCandidateNotFound", err)
	}
}

func TestServiceResumeDetectsSourceReplacement(t *testing.T) {
	t.Parallel()

	snapshot := preferences.OnboardingSnapshot{
		SchemaVersion:       preferences.CurrentSchemaVersion,
		OnboardingVersion:   preferences.CurrentOnboardingVersion,
		OnboardingCompleted: true,
		CodexHome: preferences.ConfirmedSource{
			Path: "/home", DeviceID: "5", Inode: 9, ConfirmedAtMS: 1_720_000_000_000,
		},
		OnlineQuotaEnabled: true, ResetCreditsEnabled: true,
	}
	probe := newFakeProbe(map[string][]probeResult{
		"/home": {{metadata: homeMetadata("/home", "5", 10, 0, 0)}},
	})
	service := newTestService(t, probe, &fakeStore{snapshot: &snapshot}, "", "/default", "/data/tracker.sqlite")
	state, err := service.Resume(context.Background())
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if state.Phase != PhaseSourceChanged || state.Reason != CandidateReasonChanged || state.Confirmed != nil {
		t.Fatalf("Resume(replaced) = %#v, want source_changed without authorization", state)
	}
}

func TestServiceResumeUnconfiguredNeedsSelection(t *testing.T) {
	t.Parallel()

	service := newTestService(t, newFakeProbe(nil), &fakeStore{}, "", "/default", "/data/tracker.sqlite")
	state, err := service.Resume(context.Background())
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if state.Phase != PhaseNeedsSelection || state.Confirmed != nil {
		t.Fatalf("Resume(unconfigured) = %#v", state)
	}
}

func TestServiceRealAdaptersNeverReadContentBeforeConfirmation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	home := filepath.Join(root, "codex-home")
	if err := os.MkdirAll(filepath.Join(home, "sessions", "2026"), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(home) error = %v", err)
	}
	session := filepath.Join(home, "sessions", "2026", "session.jsonl")
	content := []byte("private-content-must-not-be-read\n")
	if err := os.WriteFile(session, content, 0o600); err != nil {
		t.Fatalf("os.WriteFile(session) error = %v", err)
	}
	before, err := os.Stat(session)
	if err != nil {
		t.Fatalf("os.Stat(before) error = %v", err)
	}
	if err := os.Chmod(session, 0); err != nil {
		t.Fatalf("os.Chmod(session) error = %v", err)
	}
	defer func() { _ = os.Chmod(session, 0o600) }()

	preferencesPath := filepath.Join(root, "private", "preferences.json")
	store, err := preferences.NewFileStore(preferencesPath)
	if err != nil {
		t.Fatalf("preferences.NewFileStore() error = %v", err)
	}
	service, err := NewService(Config{
		Probe: logs.NewHomeProbe(), Store: store,
		Getenv: func(key string) string {
			if key == "CODEX_HOME" {
				return home
			}
			return ""
		},
		UserHomeDir:         func() (string, error) { return filepath.Join(root, "missing-user-home"), nil },
		TrackerDatabasePath: "/data/tracker.sqlite",
		Clock:               func() time.Time { return time.UnixMilli(1_720_000_000_000) },
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	detected, err := service.Detect(context.Background(), "")
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	ready := firstReady(t, detected.Candidates)
	confirmed, err := service.Confirm(context.Background(), Confirmation{
		CandidateID: ready.ID, OnlineQuotaEnabled: true, ResetCreditsEnabled: true,
	})
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if confirmed.Phase != PhaseConfirmed {
		t.Fatalf("Confirm().Phase = %q", confirmed.Phase)
	}
	preferenceBeforeReplay, err := os.Stat(preferencesPath)
	if err != nil {
		t.Fatalf("os.Stat(preferences before replay) error = %v", err)
	}
	if replayed, err := service.Confirm(context.Background(), Confirmation{
		CandidateID: ready.ID, OnlineQuotaEnabled: true, ResetCreditsEnabled: true,
	}); err != nil || replayed.Phase != PhaseConfirmed {
		t.Fatalf("Confirm(replay) = %#v, %v", replayed, err)
	}
	preferenceAfterReplay, err := os.Stat(preferencesPath)
	if err != nil {
		t.Fatalf("os.Stat(preferences after replay) error = %v", err)
	}
	if !preferenceAfterReplay.ModTime().Equal(preferenceBeforeReplay.ModTime()) {
		t.Fatalf("idempotent replay rewrote preferences")
	}
	resumed, err := service.Resume(context.Background())
	if err != nil || resumed.Phase != PhaseConfirmed || resumed.Confirmed == nil {
		t.Fatalf("Resume(real adapters) = %#v, %v", resumed, err)
	}
	after, err := os.Stat(session)
	if err != nil {
		t.Fatalf("os.Stat(after) error = %v", err)
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || after.Mode().Perm() != 0 {
		t.Fatalf("session metadata changed: before=%#v after=%#v", before, after)
	}
	preferenceInfo, err := os.Stat(preferencesPath)
	if err != nil {
		t.Fatalf("os.Stat(preferences) error = %v", err)
	}
	if preferenceInfo.Mode().Perm() != 0o600 {
		t.Fatalf("preferences mode = %v, want 0600", preferenceInfo.Mode())
	}
}

type probeResult struct {
	metadata logs.HomeMetadata
	err      error
}

type fakeProbe struct {
	mu      sync.Mutex
	results map[string][]probeResult
	calls   map[string]int
}

func newFakeProbe(results map[string][]probeResult) *fakeProbe {
	return &fakeProbe{results: results, calls: make(map[string]int)}
}

func (probe *fakeProbe) Probe(ctx context.Context, path string) (logs.HomeMetadata, error) {
	if err := ctx.Err(); err != nil {
		return logs.HomeMetadata{}, err
	}
	probe.mu.Lock()
	defer probe.mu.Unlock()
	sequence := probe.results[path]
	if len(sequence) == 0 {
		return logs.HomeMetadata{}, logs.ErrInvalidHome
	}
	index := probe.calls[path]
	probe.calls[path]++
	if index >= len(sequence) {
		index = len(sequence) - 1
	}
	return sequence[index].metadata, sequence[index].err
}

type fakeStore struct {
	mu                   sync.Mutex
	snapshot             *preferences.OnboardingSnapshot
	confirmErr           error
	confirmAfterWriteErr error
	confirmCalls         int
}

type cancelAfterCommitStore struct {
	fakeStore
	cancel func()
}

func (store *cancelAfterCommitStore) Confirm(
	ctx context.Context,
	next preferences.OnboardingSnapshot,
) error {
	store.mu.Lock()
	copy := next
	store.snapshot = &copy
	store.confirmCalls++
	store.mu.Unlock()
	store.cancel()
	return nil
}

type cancelAfterCommitUnreadableStore struct {
	fakeStore
	cancel func()
}

type cancelBeforeCommitStore struct {
	fakeStore
	cancel func()
}

func (store *cancelBeforeCommitStore) Confirm(
	ctx context.Context,
	_ preferences.OnboardingSnapshot,
) error {
	store.cancel()
	return ctx.Err()
}

type unavailableThenUnconfiguredStore struct {
	mu        sync.Mutex
	loadCalls int
}

func (store *unavailableThenUnconfiguredStore) Confirm(
	context.Context,
	preferences.OnboardingSnapshot,
) error {
	return preferences.ErrDurabilityUnknown
}

func (store *unavailableThenUnconfiguredStore) Load(
	context.Context,
) (preferences.OnboardingSnapshot, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.loadCalls++
	if store.loadCalls == 1 {
		return preferences.OnboardingSnapshot{}, errors.New("synthetic readback unavailable")
	}
	return preferences.OnboardingSnapshot{}, preferences.ErrNotConfigured
}

func (store *cancelAfterCommitUnreadableStore) Confirm(
	ctx context.Context,
	next preferences.OnboardingSnapshot,
) error {
	store.mu.Lock()
	copy := next
	store.snapshot = &copy
	store.confirmCalls++
	store.mu.Unlock()
	store.cancel()
	return ctx.Err()
}

func (store *cancelAfterCommitUnreadableStore) Load(
	context.Context,
) (preferences.OnboardingSnapshot, error) {
	return preferences.OnboardingSnapshot{}, errors.New("synthetic readback failure")
}

type blockingCommitStore struct {
	mu       sync.Mutex
	snapshot *preferences.OnboardingSnapshot
	entered  chan struct{}
	release  chan struct{}
}

func newBlockingCommitStore() *blockingCommitStore {
	return &blockingCommitStore{entered: make(chan struct{}), release: make(chan struct{})}
}

func (store *blockingCommitStore) Load(ctx context.Context) (preferences.OnboardingSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return preferences.OnboardingSnapshot{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.snapshot == nil {
		return preferences.OnboardingSnapshot{}, preferences.ErrNotConfigured
	}
	return *store.snapshot, nil
}

func (store *blockingCommitStore) Confirm(
	ctx context.Context,
	next preferences.OnboardingSnapshot,
) error {
	close(store.entered)
	<-store.release
	store.mu.Lock()
	copy := next
	store.snapshot = &copy
	store.mu.Unlock()
	return nil
}

func (store *fakeStore) Load(ctx context.Context) (preferences.OnboardingSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return preferences.OnboardingSnapshot{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.snapshot == nil {
		return preferences.OnboardingSnapshot{}, preferences.ErrNotConfigured
	}
	return *store.snapshot, nil
}

func (store *fakeStore) Confirm(ctx context.Context, next preferences.OnboardingSnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.confirmCalls++
	if store.confirmErr != nil {
		return store.confirmErr
	}
	if store.snapshot != nil && !reflect.DeepEqual(*store.snapshot, next) {
		return preferences.ErrAlreadyConfirmed
	}
	copy := next
	store.snapshot = &copy
	return store.confirmAfterWriteErr
}

func newTestService(
	t *testing.T,
	probe HomeProbe,
	store Store,
	environmentHome string,
	defaultHome string,
	trackerDatabasePath string,
) *Service {
	t.Helper()
	service, err := NewService(Config{
		Probe: probe, Store: store,
		Getenv: func(key string) string {
			if key == "CODEX_HOME" {
				return environmentHome
			}
			return ""
		},
		UserHomeDir: func() (string, error) {
			return filepath.Dir(defaultHome), nil
		},
		DefaultHome:         func(userHome string) string { return defaultHome },
		TrackerDatabasePath: trackerDatabasePath,
		Clock:               func() time.Time { return time.UnixMilli(1_720_000_000_000) },
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func homeMetadata(path, deviceID string, inode, files, bytes int64) logs.HomeMetadata {
	return logs.HomeMetadata{
		Path: path, DeviceID: deviceID, Inode: inode, SessionsDirectory: true,
		JSONLFiles: files, JSONLBytes: bytes,
	}
}

func candidateByPath(t *testing.T, candidates []Candidate, path string) Candidate {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.Path == path {
			return candidate
		}
	}
	t.Fatalf("candidate path %q not found in %#v", path, candidates)
	return Candidate{}
}

func firstReady(t *testing.T, candidates []Candidate) Candidate {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.Status == CandidateStatusReady {
			return candidate
		}
	}
	t.Fatalf("ready candidate not found in %#v", candidates)
	return Candidate{}
}
