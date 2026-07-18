package updater

import (
	"errors"
	"fmt"
	"net/url"
)

type Phase string

const (
	PhaseIdle        Phase = "idle"
	PhaseChecking    Phase = "checking"
	PhaseAvailable   Phase = "available"
	PhaseDownloading Phase = "downloading"
	PhaseInstalling  Phase = "installing"
	PhaseError       Phase = "error"
)

type ProgressStage string

const (
	ProgressDownloading ProgressStage = "downloading"
	ProgressExtracting  ProgressStage = "extracting"
)

type FaultCode string

const (
	FaultUnavailable      FaultCode = "unavailable"
	FaultConfiguration    FaultCode = "configuration"
	FaultCheck            FaultCode = "check"
	FaultDownload         FaultCode = "download"
	FaultInvalidSignature FaultCode = "invalid_signature"
	FaultInstall          FaultCode = "install"
	FaultNative           FaultCode = "native"
)

type SignatureStatus string

const (
	SignatureSkipped   SignatureStatus = "skipped"
	SignatureSucceeded SignatureStatus = "succeeded"
	SignatureFailed    SignatureStatus = "failed"
)

type Update struct {
	Version             string
	DisplayVersion      string
	ContentLength       uint64
	Architecture        string
	ReleaseNotes        string
	FeedSignatureStatus SignatureStatus
	InformationOnly     bool
	InformationURL      string
}

type Progress struct {
	Stage    ProgressStage
	Received uint64
	Total    uint64
	Fraction float64
}

type Fault struct {
	Code    FaultCode
	Message string
}

type Snapshot struct {
	Phase          Phase
	Update         *Update
	Progress       Progress
	Fault          *Fault
	CanCancel      bool
	ReadyToInstall bool
	Closed         bool
}

type EventKind string

const (
	EventCheckStarted         EventKind = "check_started"
	EventUpdateFound          EventKind = "update_found"
	EventResumableUpdateFound EventKind = "resumable_update_found"
	EventReleaseNotes         EventKind = "release_notes"
	EventNoUpdate             EventKind = "no_update"
	EventCheckCancelled       EventKind = "check_cancelled"
	EventUpdateDismissed      EventKind = "update_dismissed"
	EventDownloadStarted      EventKind = "download_started"
	EventDownloadProgress     EventKind = "download_progress"
	EventExtractionProgress   EventKind = "extraction_progress"
	EventDownloadCancelled    EventKind = "download_cancelled"
	EventReadyToInstall       EventKind = "ready_to_install"
	EventInstallStarted       EventKind = "install_started"
	EventCycleFinished        EventKind = "cycle_finished"
	EventFailed               EventKind = "failed"
	EventClosed               EventKind = "closed"
)

type Event struct {
	Kind     EventKind
	Update   *Update
	Received uint64
	Total    uint64
	Fraction float64
	Fault    *Fault
}

var (
	ErrInvalidEvent      = errors.New("invalid updater event")
	ErrInvalidTransition = errors.New("invalid updater transition")
)

func Reduce(before Snapshot, event Event) (Snapshot, error) {
	if before.Closed {
		return before, nil
	}
	if event.Kind == EventClosed {
		return Snapshot{Phase: PhaseIdle, Closed: true}, nil
	}

	after := before
	switch event.Kind {
	case EventCheckStarted:
		if before.Phase != PhaseIdle && before.Phase != PhaseError && before.Phase != PhaseAvailable {
			return before, transitionError(before.Phase, event.Kind)
		}
		after = Snapshot{Phase: PhaseChecking, CanCancel: true}
	case EventUpdateFound:
		if before.Phase != PhaseChecking {
			return before, transitionError(before.Phase, event.Kind)
		}
		if !validUpdate(event.Update) {
			return before, eventError(event.Kind)
		}
		update := *event.Update
		after = Snapshot{Phase: PhaseAvailable, Update: &update}
	case EventResumableUpdateFound:
		if before.Phase != PhaseChecking {
			return before, transitionError(before.Phase, event.Kind)
		}
		if !validUpdate(event.Update) || event.Update.InformationOnly {
			return before, eventError(event.Kind)
		}
		update := *event.Update
		after = Snapshot{Phase: PhaseAvailable, Update: &update, ReadyToInstall: true}
	case EventReleaseNotes:
		if event.Update == nil {
			return before, eventError(event.Kind)
		}
		if before.Update == nil {
			return before, nil
		}
		update := *before.Update
		update.ReleaseNotes = event.Update.ReleaseNotes
		after.Update = &update
	case EventNoUpdate:
		if before.Phase != PhaseChecking {
			return before, transitionError(before.Phase, event.Kind)
		}
		after = Snapshot{Phase: PhaseIdle}
	case EventCheckCancelled:
		if before.Phase != PhaseChecking {
			return before, transitionError(before.Phase, event.Kind)
		}
		after = Snapshot{Phase: PhaseIdle}
	case EventUpdateDismissed:
		if before.Phase == PhaseIdle {
			return before, nil
		}
		if before.Phase != PhaseAvailable || before.Update == nil || before.ReadyToInstall {
			return before, transitionError(before.Phase, event.Kind)
		}
		after = Snapshot{Phase: PhaseIdle}
	case EventDownloadStarted:
		if before.Phase != PhaseAvailable || before.Update == nil {
			return before, transitionError(before.Phase, event.Kind)
		}
		after.Phase = PhaseDownloading
		after.Progress = Progress{Stage: ProgressDownloading}
		after.CanCancel = true
		after.Fault = nil
	case EventDownloadProgress:
		if before.Phase != PhaseDownloading {
			return before, transitionError(before.Phase, event.Kind)
		}
		after.Progress = Progress{Stage: ProgressDownloading, Received: event.Received, Total: event.Total}
		if event.Total > 0 {
			after.Progress.Fraction = float64(event.Received) / float64(event.Total)
			if after.Progress.Fraction > 1 {
				after.Progress.Fraction = 1
			}
		}
	case EventExtractionProgress:
		if before.Phase != PhaseDownloading {
			return before, transitionError(before.Phase, event.Kind)
		}
		if event.Fraction < 0 || event.Fraction > 1 {
			return before, eventError(event.Kind)
		}
		after.Progress = Progress{Stage: ProgressExtracting, Fraction: event.Fraction}
	case EventDownloadCancelled:
		if before.Phase != PhaseDownloading {
			return before, transitionError(before.Phase, event.Kind)
		}
		after.Phase = PhaseAvailable
		after.Progress = Progress{}
		after.CanCancel = false
	case EventReadyToInstall:
		if before.Phase != PhaseDownloading || before.Update == nil {
			return before, transitionError(before.Phase, event.Kind)
		}
		after.Phase = PhaseAvailable
		after.Progress = Progress{}
		after.CanCancel = false
		after.ReadyToInstall = true
	case EventInstallStarted:
		if before.Phase == PhaseInstalling {
			return before, nil
		}
		if before.Phase != PhaseAvailable || !before.ReadyToInstall {
			return before, transitionError(before.Phase, event.Kind)
		}
		after.Phase = PhaseInstalling
		after.Progress = Progress{}
		after.CanCancel = false
		after.ReadyToInstall = false
	case EventCycleFinished:
		if before.Phase == PhaseIdle {
			return before, nil
		}
		after = Snapshot{Phase: PhaseIdle}
	case EventFailed:
		if event.Fault == nil || event.Fault.Code == "" || event.Fault.Message == "" {
			return before, eventError(event.Kind)
		}
		if before.Phase == PhaseError && before.Fault != nil {
			return before, nil
		}
		fault := *event.Fault
		after = Snapshot{Phase: PhaseError, Fault: &fault}
	default:
		return before, eventError(event.Kind)
	}
	return after, nil
}

func validUpdate(update *Update) bool {
	if update == nil || update.Version == "" || update.Architecture != "arm64" {
		return false
	}
	if !update.InformationOnly {
		return true
	}
	parsed, err := url.Parse(update.InformationURL)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func eventError(kind EventKind) error {
	return fmt.Errorf("%w: %s", ErrInvalidEvent, kind)
}

func transitionError(phase Phase, kind EventKind) error {
	return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, phase, kind)
}
