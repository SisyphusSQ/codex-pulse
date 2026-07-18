package updater

import (
	"errors"
	"testing"
)

func TestReduceCheckFindDownloadCancel(t *testing.T) {
	t.Parallel()

	snapshot := Snapshot{Phase: PhaseIdle}
	snapshot = mustReduce(t, snapshot, Event{Kind: EventCheckStarted})
	if snapshot.Phase != PhaseChecking || snapshot.Fault != nil || !snapshot.CanCancel {
		t.Fatalf("check started = %#v, want cancellable checking without fault", snapshot)
	}

	update := Update{
		Version:             "42",
		DisplayVersion:      "0.2.0",
		ContentLength:       128,
		Architecture:        "arm64",
		FeedSignatureStatus: SignatureSucceeded,
	}
	snapshot = mustReduce(t, snapshot, Event{Kind: EventUpdateFound, Update: &update})
	if snapshot.Phase != PhaseAvailable || snapshot.Update == nil || snapshot.Update.Version != "42" {
		t.Fatalf("update found = %#v, want available update", snapshot)
	}
	if snapshot.Update.FeedSignatureStatus != SignatureSucceeded {
		t.Fatalf("signature status=%q, want %q", snapshot.Update.FeedSignatureStatus, SignatureSucceeded)
	}

	snapshot = mustReduce(t, snapshot, Event{Kind: EventDownloadStarted})
	if snapshot.Phase != PhaseDownloading || !snapshot.CanCancel {
		t.Fatalf("download started = %#v, want cancellable download", snapshot)
	}
	snapshot = mustReduce(t, snapshot, Event{Kind: EventDownloadProgress, Received: 32, Total: 128})
	if snapshot.Progress.Received != 32 || snapshot.Progress.Total != 128 || snapshot.Progress.Fraction != 0.25 {
		t.Fatalf("download progress = %#v, want 32/128", snapshot.Progress)
	}

	snapshot = mustReduce(t, snapshot, Event{Kind: EventDownloadCancelled})
	if snapshot.Phase != PhaseAvailable || snapshot.CanCancel || snapshot.Update == nil {
		t.Fatalf("download cancelled = %#v, want retained available update", snapshot)
	}
}

func TestReduceRefreshesReleaseNotesWithoutChangingUpdate(t *testing.T) {
	t.Parallel()

	before := availableSnapshot()
	after := mustReduce(t, before, Event{Kind: EventReleaseNotes, Update: &Update{ReleaseNotes: "安全更新"}})
	if after.Update == nil || after.Update.Version != before.Update.Version || after.Update.ReleaseNotes != "安全更新" {
		t.Fatalf("after=%#v, want existing update with refreshed notes", after)
	}
}

func TestReduceHandlesLateReleaseNotesWithoutCorruptingLifecycle(t *testing.T) {
	t.Parallel()

	downloading := mustReduce(t, availableSnapshot(), Event{Kind: EventDownloadStarted})
	downloading = mustReduce(t, downloading, Event{Kind: EventReleaseNotes, Update: &Update{ReleaseNotes: "迟到摘要"}})
	if downloading.Phase != PhaseDownloading || downloading.Update == nil || downloading.Update.ReleaseNotes != "迟到摘要" {
		t.Fatalf("downloading=%#v, want lifecycle preserved with notes", downloading)
	}
	idle := Snapshot{Phase: PhaseIdle}
	if after := mustReduce(t, idle, Event{Kind: EventReleaseNotes, Update: &Update{ReleaseNotes: "迟到摘要"}}); after != idle {
		t.Fatalf("idle after late notes=%#v, want no-op", after)
	}
}

func TestReduceResumableUpdateBecomesReadyWithoutInstallTransition(t *testing.T) {
	checking := mustReduce(t, Snapshot{Phase: PhaseIdle}, Event{Kind: EventCheckStarted})
	update := &Update{Version: "42", Architecture: "arm64"}
	ready := mustReduce(t, checking, Event{Kind: EventResumableUpdateFound, Update: update})
	if ready.Phase != PhaseAvailable || !ready.ReadyToInstall || ready.Update == nil {
		t.Fatalf("unexpected resumable state: %#v", ready)
	}
}

func TestReduceInformationOnlyRequiresHTTPSURL(t *testing.T) {
	checking := mustReduce(t, Snapshot{Phase: PhaseIdle}, Event{Kind: EventCheckStarted})
	valid := &Update{Version: "42", Architecture: "arm64", InformationOnly: true, InformationURL: "https://example.com/fallback"}
	if after := mustReduce(t, checking, Event{Kind: EventUpdateFound, Update: valid}); after.Update == nil || !after.Update.InformationOnly {
		t.Fatalf("unexpected information-only state: %#v", after)
	}
	invalid := *valid
	invalid.InformationURL = "file:///tmp/unsafe"
	if _, err := Reduce(checking, Event{Kind: EventUpdateFound, Update: &invalid}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("unsafe information URL error = %v", err)
	}
}

func TestReduceCheckCancellationReturnsIdle(t *testing.T) {
	t.Parallel()

	snapshot := mustReduce(t, Snapshot{Phase: PhaseIdle}, Event{Kind: EventCheckStarted})
	snapshot = mustReduce(t, snapshot, Event{Kind: EventCheckCancelled})
	if snapshot.Phase != PhaseIdle || snapshot.CanCancel || snapshot.Fault != nil {
		t.Fatalf("check cancelled = %#v, want clean idle", snapshot)
	}
}

func TestReduceExtractionAndInstall(t *testing.T) {
	t.Parallel()

	snapshot := availableSnapshot()
	snapshot = mustReduce(t, snapshot, Event{Kind: EventDownloadStarted})
	snapshot = mustReduce(t, snapshot, Event{Kind: EventExtractionProgress, Fraction: 0.6})
	if snapshot.Phase != PhaseDownloading || snapshot.Progress.Stage != ProgressExtracting || snapshot.Progress.Fraction != 0.6 {
		t.Fatalf("extraction = %#v, want downloading extraction at 60%%", snapshot)
	}
	snapshot = mustReduce(t, snapshot, Event{Kind: EventReadyToInstall})
	if snapshot.Phase != PhaseAvailable || !snapshot.ReadyToInstall || snapshot.CanCancel || snapshot.Update == nil {
		t.Fatalf("ready = %#v, want retained update awaiting install confirmation", snapshot)
	}
	snapshot = mustReduce(t, snapshot, Event{Kind: EventInstallStarted})
	if snapshot.Phase != PhaseInstalling || snapshot.CanCancel {
		t.Fatalf("install = %#v, want non-cancellable installing", snapshot)
	}
	again := mustReduce(t, snapshot, Event{Kind: EventInstallStarted})
	if again != snapshot {
		t.Fatalf("duplicate install callback mutated snapshot: before=%#v after=%#v", snapshot, again)
	}
	snapshot = mustReduce(t, snapshot, Event{Kind: EventCycleFinished})
	if snapshot.Phase != PhaseIdle || snapshot.Update != nil || snapshot.Progress != (Progress{}) {
		t.Fatalf("cycle finished = %#v, want clean idle", snapshot)
	}
}

func TestReduceTypedFailureAndRecovery(t *testing.T) {
	t.Parallel()

	snapshot := Snapshot{Phase: PhaseChecking}
	fault := &Fault{Code: FaultInvalidSignature, Message: "archive signature rejected"}
	snapshot = mustReduce(t, snapshot, Event{Kind: EventFailed, Fault: fault})
	if snapshot.Phase != PhaseError || snapshot.Fault == nil || snapshot.Fault.Code != FaultInvalidSignature {
		t.Fatalf("failed = %#v, want typed signature fault", snapshot)
	}
	again := mustReduce(t, snapshot, Event{Kind: EventFailed, Fault: fault})
	if again != snapshot {
		t.Fatalf("duplicate failure mutated snapshot: before=%#v after=%#v", snapshot, again)
	}
	late := mustReduce(t, snapshot, Event{Kind: EventFailed, Fault: &Fault{Code: FaultDownload, Message: "late wrapper"}})
	if late != snapshot {
		t.Fatalf("late failure overwrote first terminal fault: before=%#v after=%#v", snapshot, late)
	}

	snapshot = mustReduce(t, snapshot, Event{Kind: EventCheckStarted})
	if snapshot.Phase != PhaseChecking || snapshot.Fault != nil || snapshot.Update != nil {
		t.Fatalf("retry = %#v, want checking with transient state cleared", snapshot)
	}
}

func TestReduceRejectsInvalidOrMalformedEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		before   Snapshot
		event    Event
		wantKind error
	}{
		{name: "found outside check", before: Snapshot{Phase: PhaseIdle}, event: Event{Kind: EventUpdateFound, Update: &Update{Version: "1", Architecture: "arm64"}}, wantKind: ErrInvalidTransition},
		{name: "missing update", before: Snapshot{Phase: PhaseChecking}, event: Event{Kind: EventUpdateFound}, wantKind: ErrInvalidEvent},
		{name: "wrong architecture", before: Snapshot{Phase: PhaseChecking}, event: Event{Kind: EventUpdateFound, Update: &Update{Version: "1", Architecture: "x86_64"}}, wantKind: ErrInvalidEvent},
		{name: "fraction below zero", before: downloadingSnapshot(), event: Event{Kind: EventExtractionProgress, Fraction: -0.1}, wantKind: ErrInvalidEvent},
		{name: "failure without fault", before: Snapshot{Phase: PhaseChecking}, event: Event{Kind: EventFailed}, wantKind: ErrInvalidEvent},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			after, err := Reduce(test.before, test.event)
			if !errors.Is(err, test.wantKind) {
				t.Fatalf("Reduce error = %v, want %v", err, test.wantKind)
			}
			if after != test.before {
				t.Fatalf("invalid event mutated snapshot: before=%#v after=%#v", test.before, after)
			}
		})
	}
}

func TestReduceCapsFractionWhenExpectedLengthIsTooSmall(t *testing.T) {
	t.Parallel()

	snapshot := downloadingSnapshot()
	after := mustReduce(t, snapshot, Event{Kind: EventDownloadProgress, Received: 2, Total: 1})
	if after.Progress.Received != 2 || after.Progress.Total != 1 || after.Progress.Fraction != 1 {
		t.Fatalf("progress=%#v, want actual bytes with capped fraction", after.Progress)
	}
}

func TestReduceCloseIsIdempotentAndIgnoresLaterCallbacks(t *testing.T) {
	t.Parallel()

	snapshot := downloadingSnapshot()
	snapshot = mustReduce(t, snapshot, Event{Kind: EventClosed})
	if !snapshot.Closed || snapshot.Phase != PhaseIdle || snapshot.CanCancel || snapshot.Update != nil {
		t.Fatalf("closed = %#v, want closed idle", snapshot)
	}
	again := mustReduce(t, snapshot, Event{Kind: EventClosed})
	if again != snapshot {
		t.Fatalf("second close mutated snapshot: before=%#v after=%#v", snapshot, again)
	}
	after, err := Reduce(snapshot, Event{Kind: EventFailed, Fault: &Fault{Code: FaultNative, Message: "late"}})
	if err != nil || after != snapshot {
		t.Fatalf("late callback after close = (%#v, %v), want unchanged", after, err)
	}
}

func mustReduce(t *testing.T, before Snapshot, event Event) Snapshot {
	t.Helper()
	after, err := Reduce(before, event)
	if err != nil {
		t.Fatalf("Reduce(%s, %s): %v", before.Phase, event.Kind, err)
	}
	return after
}

func availableSnapshot() Snapshot {
	return Snapshot{Phase: PhaseAvailable, Update: &Update{Version: "42", DisplayVersion: "0.2.0", ContentLength: 128, Architecture: "arm64"}}
}

func downloadingSnapshot() Snapshot {
	snapshot := availableSnapshot()
	snapshot.Phase = PhaseDownloading
	snapshot.CanCancel = true
	return snapshot
}
