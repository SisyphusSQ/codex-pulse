// Package onboarding coordinates the content-free Codex Home selection and
// the user's explicit privacy confirmation. It never starts indexing.
package onboarding

import (
	"context"
	"errors"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
)

var (
	ErrInvalidConfiguration = errors.New("invalid onboarding configuration")
	ErrCandidateNotFound    = errors.New("onboarding candidate not found")
	ErrCandidateUnavailable = errors.New("onboarding candidate unavailable")
	ErrCandidateChanged     = errors.New("onboarding candidate changed")
	ErrPersistenceFailed    = errors.New("onboarding persistence failed")
)

type CandidateSource string

const (
	CandidateSourceEnvironment CandidateSource = "environment"
	CandidateSourceDefault     CandidateSource = "default"
	CandidateSourceSelected    CandidateSource = "selected"
)

type CandidateStatus string

const (
	CandidateStatusReady       CandidateStatus = "ready"
	CandidateStatusUnavailable CandidateStatus = "unavailable"
	CandidateStatusUnsafe      CandidateStatus = "unsafe"
)

type CandidateReason string

const (
	CandidateReasonNone              CandidateReason = "none"
	CandidateReasonMissing           CandidateReason = "missing"
	CandidateReasonPermission        CandidateReason = "permission"
	CandidateReasonUnsafeSymlink     CandidateReason = "unsafe_symlink"
	CandidateReasonUnsupportedEntry  CandidateReason = "unsupported_entry"
	CandidateReasonChanged           CandidateReason = "changed"
	CandidateReasonInvalidPath       CandidateReason = "invalid_path"
	CandidateReasonIO                CandidateReason = "io"
	CandidateReasonDurabilityUnknown CandidateReason = "durability_unknown"
)

type Phase string

const (
	PhaseAwaitingConfirmation Phase = "awaiting_confirmation"
	PhaseNeedsSelection       Phase = "needs_selection"
	PhaseCanceled             Phase = "canceled"
	PhaseConfirmed            Phase = "confirmed"
	PhaseRetryableError       Phase = "retryable_error"
	PhaseSourceChanged        Phase = "source_changed"
)

type Candidate struct {
	ID        string
	Source    CandidateSource
	Path      string
	Status    CandidateStatus
	Reason    CandidateReason
	Retryable bool
	Metadata  logs.HomeMetadata
}

type Confirmation struct {
	CandidateID         string
	OnlineQuotaEnabled  bool
	ResetCreditsEnabled bool
}

type PrivacyNotice struct {
	TitleZH             string
	BodyZH              string
	TrackerDatabasePath string
	ReadsSessionFiles   bool
	StoresContent       bool
	OnlineTokenInMemory bool
}

type State struct {
	Phase      Phase
	Reason     CandidateReason
	Candidates []Candidate
	Confirmed  *preferences.OnboardingSnapshot
	Privacy    PrivacyNotice
}

type HomeProbe interface {
	Probe(ctx context.Context, path string) (logs.HomeMetadata, error)
}

type Store interface {
	Load(ctx context.Context) (preferences.OnboardingSnapshot, error)
	Confirm(ctx context.Context, next preferences.OnboardingSnapshot) error
}
