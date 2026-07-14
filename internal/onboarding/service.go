package onboarding

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
)

type Config struct {
	Probe               HomeProbe
	Store               Store
	Getenv              func(string) string
	UserHomeDir         func() (string, error)
	DefaultHome         func(string) string
	TrackerDatabasePath string
	Clock               func() time.Time
}

type Service struct {
	probe               HomeProbe
	store               Store
	getenv              func(string) string
	userHomeDir         func() (string, error)
	defaultHome         func(string) string
	trackerDatabasePath string
	clock               func() time.Time

	mu                        sync.Mutex
	lastCandidates            []Candidate
	currentState              State
	hasPersistedConfiguration bool
}

func NewService(config Config) (*Service, error) {
	if config.Probe == nil || config.Store == nil ||
		!filepath.IsAbs(config.TrackerDatabasePath) ||
		filepath.Clean(config.TrackerDatabasePath) != config.TrackerDatabasePath {
		return nil, ErrInvalidConfiguration
	}
	if config.Getenv == nil {
		config.Getenv = os.Getenv
	}
	if config.UserHomeDir == nil {
		config.UserHomeDir = os.UserHomeDir
	}
	if config.DefaultHome == nil {
		config.DefaultHome = func(userHome string) string { return filepath.Join(userHome, ".codex") }
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	return &Service{
		probe: config.Probe, store: config.Store, getenv: config.Getenv,
		userHomeDir: config.UserHomeDir, defaultHome: config.DefaultHome,
		trackerDatabasePath: config.TrackerDatabasePath, clock: config.Clock,
	}, nil
}

func NewDefaultService(trackerDatabasePath string) (*Service, error) {
	preferencesPath, err := preferences.DefaultPath()
	if err != nil {
		return nil, err
	}
	store, err := preferences.NewFileStore(preferencesPath)
	if err != nil {
		return nil, err
	}
	return NewService(Config{
		Probe: logs.NewHomeProbe(), Store: store, TrackerDatabasePath: trackerDatabasePath,
	})
}

func (service *Service) Detect(ctx context.Context, selectedPath string) (State, error) {
	if service == nil {
		return State{}, ErrInvalidConfiguration
	}
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.hasPersistedConfiguration {
		return cloneState(service.currentState), nil
	}
	inputs := make([]candidateInput, 0, 3)
	if environmentHome := service.getenv("CODEX_HOME"); strings.TrimSpace(environmentHome) != "" {
		inputs = append(inputs, candidateInput{source: CandidateSourceEnvironment, path: environmentHome})
	}
	userHome, homeErr := service.userHomeDir()
	if homeErr != nil {
		inputs = append(inputs, candidateInput{source: CandidateSourceDefault, inputError: homeErr})
	} else {
		inputs = append(inputs, candidateInput{
			source: CandidateSourceDefault, path: service.defaultHome(userHome),
		})
	}
	if strings.TrimSpace(selectedPath) != "" {
		inputs = append(inputs, candidateInput{source: CandidateSourceSelected, path: selectedPath})
	}

	candidates := make([]Candidate, 0, len(inputs))
	seenInput := make(map[string]struct{}, len(inputs))
	seenCanonical := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		if err := ctx.Err(); err != nil {
			return State{}, err
		}
		if input.inputError != nil {
			candidates = append(candidates, Candidate{
				Source: input.source, Status: CandidateStatusUnavailable,
				Reason: CandidateReasonIO, Retryable: true,
			})
			continue
		}
		cleaned := filepath.Clean(input.path)
		if _, exists := seenInput[cleaned]; exists {
			continue
		}
		seenInput[cleaned] = struct{}{}
		candidate, probeErr := service.probeCandidate(ctx, input.source, cleaned)
		if probeErr != nil {
			return State{}, probeErr
		}
		if candidate.Status == CandidateStatusReady {
			if _, exists := seenCanonical[candidate.Path]; exists {
				continue
			}
			seenCanonical[candidate.Path] = struct{}{}
		}
		candidates = append(candidates, candidate)
	}
	state := State{
		Phase: PhaseNeedsSelection, Reason: CandidateReasonNone,
		Candidates: cloneCandidates(candidates), Privacy: service.privacyNotice(),
	}
	for _, candidate := range candidates {
		if candidate.Status == CandidateStatusReady {
			state.Phase = PhaseAwaitingConfirmation
			break
		}
	}
	service.lastCandidates = cloneCandidates(candidates)
	return service.recordState(state, false), nil
}

func (service *Service) Confirm(ctx context.Context, confirmation Confirmation) (State, error) {
	if service == nil {
		return State{}, ErrInvalidConfiguration
	}
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	candidate, found := findCandidate(service.lastCandidates, confirmation.CandidateID)
	if !found {
		return service.awaitingState(), ErrCandidateNotFound
	}
	if candidate.Status != CandidateStatusReady {
		return service.awaitingState(), ErrCandidateUnavailable
	}
	current, err := service.probe.Probe(ctx, candidate.Path)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return State{}, err
	}
	if err != nil || current.Path != candidate.Metadata.Path ||
		current.DeviceID != candidate.Metadata.DeviceID || current.Inode != candidate.Metadata.Inode {
		state := State{
			Phase: PhaseRetryableError, Reason: CandidateReasonChanged,
			Candidates: cloneCandidates(service.lastCandidates), Privacy: service.privacyNotice(),
		}
		return service.recordState(state, false), ErrCandidateChanged
	}
	next := preferences.OnboardingSnapshot{
		SchemaVersion:       preferences.CurrentSchemaVersion,
		OnboardingVersion:   preferences.CurrentOnboardingVersion,
		OnboardingCompleted: true,
		CodexHome: preferences.ConfirmedSource{
			Path: current.Path, DeviceID: current.DeviceID, Inode: current.Inode,
			ConfirmedAtMS: service.clock().UnixMilli(),
		},
		OnlineQuotaEnabled:  confirmation.OnlineQuotaEnabled,
		ResetCreditsEnabled: confirmation.ResetCreditsEnabled,
	}
	err = service.store.Confirm(ctx, next)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			persisted, readback := service.readbackAfterCommit(ctx, next)
			switch readback {
			case commitReadbackMatches:
				return service.recordState(service.confirmedState(persisted), true), nil
			case commitReadbackNotConfigured:
				return State{}, err
			case commitReadbackConflicts:
				state := service.awaitingState()
				return service.recordState(state, true), preferences.ErrAlreadyConfirmed
			default:
				state := State{
					Phase: PhaseRetryableError, Reason: CandidateReasonDurabilityUnknown,
					Candidates: cloneCandidates(service.lastCandidates), Privacy: service.privacyNotice(),
				}
				return service.recordState(state, true), preferences.ErrDurabilityUnknown
			}
		}
		if errors.Is(err, preferences.ErrDurabilityUnknown) {
			state := State{
				Phase: PhaseRetryableError, Reason: CandidateReasonDurabilityUnknown,
				Candidates: cloneCandidates(service.lastCandidates), Privacy: service.privacyNotice(),
			}
			if persisted, readback := service.readbackAfterCommit(ctx, next); readback == commitReadbackMatches {
				state.Confirmed = snapshotPointer(persisted)
			}
			return service.recordState(state, true), preferences.ErrDurabilityUnknown
		}
		if errors.Is(err, preferences.ErrAlreadyConfirmed) {
			state := service.awaitingState()
			return service.recordState(state, true), preferences.ErrAlreadyConfirmed
		}
		state := State{
			Phase: PhaseRetryableError, Reason: CandidateReasonIO,
			Candidates: cloneCandidates(service.lastCandidates), Privacy: service.privacyNotice(),
		}
		return service.recordState(state, false), ErrPersistenceFailed
	}
	persisted, readback := service.readbackAfterCommit(ctx, next)
	if readback != commitReadbackMatches {
		state := State{
			Phase: PhaseRetryableError, Reason: CandidateReasonDurabilityUnknown,
			Candidates: cloneCandidates(service.lastCandidates), Privacy: service.privacyNotice(),
		}
		return service.recordState(state, true), preferences.ErrDurabilityUnknown
	}
	return service.recordState(service.confirmedState(persisted), true), nil
}

func (service *Service) Cancel() State {
	if service == nil {
		return State{Phase: PhaseCanceled}
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.hasPersistedConfiguration {
		return cloneState(service.currentState)
	}
	service.lastCandidates = nil
	state := State{Phase: PhaseCanceled, Reason: CandidateReasonNone, Privacy: service.privacyNotice()}
	return service.recordState(state, false)
}

func (service *Service) Resume(ctx context.Context) (State, error) {
	if service == nil {
		return State{}, ErrInvalidConfiguration
	}
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	snapshot, err := service.store.Load(ctx)
	if errors.Is(err, preferences.ErrNotConfigured) {
		state := State{
			Phase: PhaseNeedsSelection, Reason: CandidateReasonNone, Privacy: service.privacyNotice(),
		}
		// A successful authoritative read is the only event allowed to clear a
		// conservative durability-unknown/configured latch. Transient failures
		// must keep the boundary closed until a later Resume can prove absence.
		service.hasPersistedConfiguration = false
		return service.recordState(state, false), nil
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return State{}, err
		}
		state := State{
			Phase: PhaseRetryableError, Reason: CandidateReasonIO, Privacy: service.privacyNotice(),
		}
		return service.recordState(state, false), ErrPersistenceFailed
	}
	current, probeErr := service.probe.Probe(ctx, snapshot.CodexHome.Path)
	if errors.Is(probeErr, context.Canceled) || errors.Is(probeErr, context.DeadlineExceeded) {
		return State{}, probeErr
	}
	if probeErr != nil || current.Path != snapshot.CodexHome.Path ||
		current.DeviceID != snapshot.CodexHome.DeviceID || current.Inode != snapshot.CodexHome.Inode {
		reason := CandidateReasonChanged
		if probeErr != nil {
			_, reason, _ = classifyProbeError(probeErr)
		}
		state := State{
			Phase: PhaseSourceChanged, Reason: reason, Privacy: service.privacyNotice(),
		}
		return service.recordState(state, true), nil
	}
	return service.recordState(service.confirmedState(snapshot), true), nil
}

type candidateInput struct {
	source     CandidateSource
	path       string
	inputError error
}

func (service *Service) probeCandidate(
	ctx context.Context,
	source CandidateSource,
	path string,
) (Candidate, error) {
	candidate := Candidate{Source: source, Path: path, Reason: CandidateReasonNone}
	if !filepath.IsAbs(path) {
		candidate.Status = CandidateStatusUnavailable
		candidate.Reason = CandidateReasonInvalidPath
		return candidate, nil
	}
	metadata, err := service.probe.Probe(ctx, path)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Candidate{}, err
		}
		candidate.Status, candidate.Reason, candidate.Retryable = classifyProbeError(err)
		return candidate, nil
	}
	if metadata.Path == "" || !filepath.IsAbs(metadata.Path) || metadata.DeviceID == "" || metadata.Inode <= 0 {
		candidate.Status = CandidateStatusUnavailable
		candidate.Reason = CandidateReasonIO
		candidate.Retryable = true
		return candidate, nil
	}
	candidate.Path = metadata.Path
	candidate.Status = CandidateStatusReady
	candidate.Metadata = metadata
	candidate.ID = candidateID(source, metadata)
	return candidate, nil
}

func classifyProbeError(err error) (CandidateStatus, CandidateReason, bool) {
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, logs.ErrInvalidHome):
		return CandidateStatusUnavailable, CandidateReasonMissing, false
	case errors.Is(err, fs.ErrPermission):
		return CandidateStatusUnavailable, CandidateReasonPermission, true
	case errors.Is(err, logs.ErrUnsafeHome), errors.Is(err, logs.ErrUnsafeSource):
		return CandidateStatusUnsafe, CandidateReasonUnsafeSymlink, false
	case errors.Is(err, logs.ErrUnsupportedFile):
		return CandidateStatusUnsafe, CandidateReasonUnsupportedEntry, false
	case errors.Is(err, logs.ErrHomeChanged), errors.Is(err, logs.ErrChangedDuringScan):
		return CandidateStatusUnavailable, CandidateReasonChanged, true
	default:
		return CandidateStatusUnavailable, CandidateReasonIO, true
	}
}

func candidateID(source CandidateSource, metadata logs.HomeMetadata) string {
	hasher := sha256.New()
	writeDigestString(hasher, string(source))
	writeDigestString(hasher, metadata.Path)
	writeDigestString(hasher, metadata.DeviceID)
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(metadata.Inode))
	_, _ = hasher.Write(encoded[:])
	return "codex-home:" + hex.EncodeToString(hasher.Sum(nil))
}

func writeDigestString(hasher hash.Hash, value string) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(len(value)))
	_, _ = hasher.Write(encoded[:])
	_, _ = hasher.Write([]byte(value))
}

func findCandidate(candidates []Candidate, id string) (Candidate, bool) {
	for _, candidate := range candidates {
		if candidate.ID != "" && candidate.ID == id {
			return candidate, true
		}
	}
	return Candidate{}, false
}

func cloneCandidates(candidates []Candidate) []Candidate {
	return append([]Candidate(nil), candidates...)
}

func cloneState(state State) State {
	state.Candidates = cloneCandidates(state.Candidates)
	if state.Confirmed != nil {
		state.Confirmed = snapshotPointer(*state.Confirmed)
	}
	return state
}

func snapshotPointer(snapshot preferences.OnboardingSnapshot) *preferences.OnboardingSnapshot {
	copy := snapshot
	return &copy
}

func matchesConfirmation(persisted, requested preferences.OnboardingSnapshot) bool {
	return persisted.SchemaVersion == requested.SchemaVersion &&
		persisted.OnboardingVersion == requested.OnboardingVersion &&
		persisted.OnboardingCompleted == requested.OnboardingCompleted &&
		persisted.CodexHome.Path == requested.CodexHome.Path &&
		persisted.CodexHome.DeviceID == requested.CodexHome.DeviceID &&
		persisted.CodexHome.Inode == requested.CodexHome.Inode &&
		persisted.OnlineQuotaEnabled == requested.OnlineQuotaEnabled &&
		persisted.ResetCreditsEnabled == requested.ResetCreditsEnabled
}

type commitReadback uint8

const (
	commitReadbackUnavailable commitReadback = iota
	commitReadbackNotConfigured
	commitReadbackConflicts
	commitReadbackMatches
)

func (service *Service) readbackAfterCommit(
	requestContext context.Context,
	requested preferences.OnboardingSnapshot,
) (preferences.OnboardingSnapshot, commitReadback) {
	readbackContext, cancel := context.WithTimeout(context.WithoutCancel(requestContext), 2*time.Second)
	defer cancel()
	persisted, err := service.store.Load(readbackContext)
	if errors.Is(err, preferences.ErrNotConfigured) {
		return preferences.OnboardingSnapshot{}, commitReadbackNotConfigured
	}
	if err != nil {
		return preferences.OnboardingSnapshot{}, commitReadbackUnavailable
	}
	if !matchesConfirmation(persisted, requested) {
		return persisted, commitReadbackConflicts
	}
	return persisted, commitReadbackMatches
}

func (service *Service) confirmedState(snapshot preferences.OnboardingSnapshot) State {
	return State{
		Phase: PhaseConfirmed, Reason: CandidateReasonNone,
		Candidates: cloneCandidates(service.lastCandidates),
		Confirmed:  snapshotPointer(snapshot), Privacy: service.privacyNotice(),
	}
}

func (service *Service) recordState(state State, persisted bool) State {
	state = cloneState(state)
	service.currentState = state
	if persisted {
		service.hasPersistedConfiguration = true
	}
	return cloneState(state)
}

func (service *Service) awaitingState() State {
	phase := PhaseNeedsSelection
	for _, candidate := range service.lastCandidates {
		if candidate.Status == CandidateStatusReady {
			phase = PhaseAwaitingConfirmation
			break
		}
	}
	return State{
		Phase: phase, Reason: CandidateReasonNone,
		Candidates: cloneCandidates(service.lastCandidates), Privacy: service.privacyNotice(),
	}
}

func (service *Service) privacyNotice() PrivacyNotice {
	return PrivacyNotice{
		TitleZH:             "隐私与数据边界",
		BodyZH:              "Codex Pulse 仅在你确认后只读本机 Codex session 和索引文件，并把 token、项目、模型、配额等结构化数据保存到本机 SQLite；不保存 prompt、response、reasoning、tool output 或 auth 内容。在线 quota 与 reset credits 可分别关闭；启用时 access token 仅驻内存，不写入数据库或日志。",
		TrackerDatabasePath: service.trackerDatabasePath,
		ReadsSessionFiles:   true, StoresContent: false, OnlineTokenInMemory: true,
	}
}
