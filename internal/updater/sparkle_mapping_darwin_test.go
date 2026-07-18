//go:build darwin && cgo

package updater

import (
	"testing"
	"unicode/utf8"
)

func TestNativeUpdateFoundMapping(t *testing.T) {
	t.Parallel()

	event := nativeEvent(nativeEventUpdateFound, "42", "0.2.0", "安全更新", "", 128, 1, 0, false, 0, 0, 0, 0, "")
	if event.Kind != EventUpdateFound || event.Update == nil {
		t.Fatalf("event=%#v, want update found", event)
	}
	if event.Update.Version != "42" || event.Update.DisplayVersion != "0.2.0" || event.Update.ContentLength != 128 {
		t.Fatalf("update=%#v, want native metadata", event.Update)
	}
	if event.Update.ReleaseNotes != "安全更新" {
		t.Fatalf("release notes=%q, want bounded plain text", event.Update.ReleaseNotes)
	}
	if event.Update.Architecture != "arm64" || event.Update.FeedSignatureStatus != SignatureSucceeded {
		t.Fatalf("update=%#v, want arm64 and succeeded feed signature", event.Update)
	}
}

func TestNativeProgressAndLifecycleMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind int
		want EventKind
	}{
		{name: "no update", kind: nativeEventNoUpdate, want: EventNoUpdate},
		{name: "check cancel", kind: nativeEventCheckCancelled, want: EventCheckCancelled},
		{name: "download start", kind: nativeEventDownloadStarted, want: EventDownloadStarted},
		{name: "ready", kind: nativeEventReadyToInstall, want: EventReadyToInstall},
		{name: "install", kind: nativeEventInstallStarted, want: EventInstallStarted},
		{name: "cancel", kind: nativeEventDownloadCancelled, want: EventDownloadCancelled},
		{name: "finished", kind: nativeEventCycleFinished, want: EventCycleFinished},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := nativeEvent(test.kind, "", "", "", "", 0, 0, 0, false, 12, 48, 0.25, 0, "")
			if event.Kind != test.want {
				t.Fatalf("kind=%q, want %q", event.Kind, test.want)
			}
		})
	}

	download := nativeEvent(nativeEventDownloadProgress, "", "", "", "", 0, 0, 0, false, 12, 48, 0, 0, "")
	if download.Received != 12 || download.Total != 48 {
		t.Fatalf("download=%#v, want 12/48", download)
	}
	extraction := nativeEvent(nativeEventExtractionProgress, "", "", "", "", 0, 0, 0, false, 0, 0, 0.25, 0, "")
	if extraction.Fraction != 0.25 {
		t.Fatalf("extraction=%#v, want 0.25", extraction)
	}
}

func TestSparkleErrorMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code int64
		want FaultCode
	}{
		{code: 3, want: FaultConfiguration},
		{code: 1000, want: FaultCheck},
		{code: 2001, want: FaultDownload},
		{code: 3001, want: FaultInvalidSignature},
		{code: 3002, want: FaultInvalidSignature},
		{code: 4005, want: FaultInstall},
		{code: 4007, want: FaultInstall},
		{code: 9999, want: FaultNative},
	}
	for _, test := range tests {
		event := nativeEvent(nativeEventFailed, "", "", "", "", 0, 0, 0, false, 0, 0, 0, test.code, "boom")
		if event.Fault == nil || event.Fault.Code != test.want || event.Fault.Message != "boom" {
			t.Fatalf("code %d event=%#v, want %q boom", test.code, event, test.want)
		}
	}
}

func TestSparkleAsyncInstallFailureMapping(t *testing.T) {
	event := nativeEvent(nativeEventInstallFailed, "", "", "", "", 0, 0, 0, false, 0, 0, 0, 3, "reply missing")
	if event.Kind != EventFailed || event.Fault == nil || event.Fault.Code != FaultInstall || event.Fault.Message != "reply missing" {
		t.Fatalf("event=%#v, want typed install failure", event)
	}
}

func TestBoundedReleaseNotesPreservesUTF8(t *testing.T) {
	value := string(make([]byte, maximumReleaseNotesBytes-1)) + "安全"
	got := boundedReleaseNotes(value)
	if len(got) > maximumReleaseNotesBytes || !utf8.ValidString(got) {
		t.Fatalf("bounded release notes len=%d valid=%t", len(got), utf8.ValidString(got))
	}
}

func TestNativeReleaseNotesRefreshMapping(t *testing.T) {
	event := nativeEvent(nativeEventReleaseNotes, "", "", "外链安全更新", "", 0, 0, 0, false, 0, 0, 0, 0, "")
	if event.Kind != EventReleaseNotes || event.Update == nil || event.Update.ReleaseNotes != "外链安全更新" {
		t.Fatalf("event=%#v, want release notes refresh", event)
	}
}

func TestNativeResumableAndInformationOnlyMapping(t *testing.T) {
	resumable := nativeEvent(nativeEventResumableUpdateFound, "42", "0.2.0", "", "", 128, 1, 2, false, 0, 0, 0, 0, "")
	if resumable.Kind != EventResumableUpdateFound || resumable.Update == nil || resumable.Update.InformationOnly {
		t.Fatalf("unexpected resumable event: %#v", resumable)
	}
	information := nativeEvent(nativeEventUpdateFound, "43", "0.3.0", "fallback", "https://example.com/update", 0, 1, 0, true, 0, 0, 0, 0, "")
	if information.Kind != EventUpdateFound || information.Update == nil || !information.Update.InformationOnly || information.Update.InformationURL != "https://example.com/update" {
		t.Fatalf("unexpected information-only event: %#v", information)
	}
}
