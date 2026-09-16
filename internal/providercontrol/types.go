package providercontrol

import (
	"context"
	"errors"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
)

const (
	ProviderControlVersion = "provider-control-v1"
)

type DiscoveryState string

const (
	DiscoveryUnchecked     DiscoveryState = "unchecked"
	DiscoveryAvailable     DiscoveryState = "available"
	DiscoveryMissing       DiscoveryState = "missing"
	DiscoveryInaccessible  DiscoveryState = "inaccessible"
	DiscoveryInvalid       DiscoveryState = "invalid"
)

type EffectiveState string

const (
	EffectiveEnabled     EffectiveState = "enabled"
	EffectiveDisabled    EffectiveState = "disabled"
	EffectiveUnavailable EffectiveState = "unavailable"
	EffectiveDisabling   EffectiveState = "disabling"
)

const (
	ReasonAvailable         = "available"
	ReasonNotFound          = "not_found"
	ReasonPermissionDenied  = "permission_denied"
	ReasonUnsafePath        = "unsafe_path"
	ReasonInvalidType       = "invalid_type"
	ReasonProbeFailed       = "probe_failed"
	ReasonDisabled          = "disabled"
	ReasonDisabling         = "disabling"
	ReasonUnchecked         = "unchecked"
	ReasonIntentEnabled     = "enabled"
)

var (
	ErrInvalidController = errors.New("provider controller is unavailable")
	ErrInvalidProvider   = errors.New("provider is invalid")
	ErrDisabled          = errors.New("provider is disabled")
	ErrUnavailable       = errors.New("provider is unavailable")
	ErrStaleGeneration   = errors.New("provider generation is stale")
	ErrSealed            = errors.New("provider controller is sealed")
)

var providerOrder = []string{agentprovider.Codex, agentprovider.Cursor, agentprovider.Grok}

type Snapshot struct {
	Provider   string
	Intent     preferences.ProviderIntent
	Discovery  DiscoveryState
	Effective  EffectiveState
	ReasonCode string
	Generation uint64
}

type TransitionResult struct {
	Applied           bool
	ReconcileRequired bool
	Snapshots         []Snapshot
}

type ProbeResult struct {
	State      DiscoveryState
	ReasonCode string
}

type StateReader interface {
	ProviderState(provider string) (Snapshot, error)
	EnabledProviders() []string
	Generation() uint64
}

type Operation interface {
	Context() context.Context
	Generation() uint64
	Provider() string
	BeginCommit() (finish func(), err error)
	Finish()
}

type Beginner interface {
	Begin(ctx context.Context, provider string) (Operation, error)
}

type PreferencesReader interface {
	LoadPreferences(context.Context) (preferences.Snapshot, error)
}

type ProbeSet struct {
	Codex  func(context.Context, *preferences.CodexHomePreferences) ProbeResult
	Cursor func(context.Context) ProbeResult
	Grok   func(context.Context) ProbeResult
}

func NormalizeProvider(value string) (string, error) {
	switch value {
	case agentprovider.Codex, agentprovider.Cursor, agentprovider.Grok:
		return value, nil
	default:
		return "", ErrInvalidProvider
	}
}

func EffectiveFor(intent preferences.ProviderIntent, discovery DiscoveryState, disabling bool) (EffectiveState, string) {
	if intent == preferences.ProviderIntentDisabled {
		if disabling {
			return EffectiveDisabling, ReasonDisabling
		}
		return EffectiveDisabled, ReasonDisabled
	}
	if discovery == DiscoveryAvailable {
		return EffectiveEnabled, ReasonAvailable
	}
	reason := string(discovery)
	if reason == "" || discovery == DiscoveryUnchecked {
		reason = ReasonUnchecked
	}
	return EffectiveUnavailable, reason
}

func QueryFailure(snapshot Snapshot) error {
	switch snapshot.Effective {
	case EffectiveEnabled:
		return nil
	case EffectiveDisabled, EffectiveDisabling:
		return ErrDisabled
	default:
		return ErrUnavailable
	}
}

func ProtoIntent(value preferences.ProviderIntent) string {
	switch value {
	case preferences.ProviderIntentAuto:
		return "PROVIDER_INTENT_AUTO"
	case preferences.ProviderIntentEnabled:
		return "PROVIDER_INTENT_ENABLED"
	case preferences.ProviderIntentDisabled:
		return "PROVIDER_INTENT_DISABLED"
	default:
		return "PROVIDER_INTENT_UNSPECIFIED"
	}
}

func ProtoDiscovery(value DiscoveryState) string {
	switch value {
	case DiscoveryUnchecked:
		return "PROVIDER_DISCOVERY_STATE_UNCHECKED"
	case DiscoveryAvailable:
		return "PROVIDER_DISCOVERY_STATE_AVAILABLE"
	case DiscoveryMissing:
		return "PROVIDER_DISCOVERY_STATE_MISSING"
	case DiscoveryInaccessible:
		return "PROVIDER_DISCOVERY_STATE_INACCESSIBLE"
	case DiscoveryInvalid:
		return "PROVIDER_DISCOVERY_STATE_INVALID"
	default:
		return "PROVIDER_DISCOVERY_STATE_UNSPECIFIED"
	}
}

func ProtoEffective(value EffectiveState) string {
	switch value {
	case EffectiveEnabled:
		return "PROVIDER_EFFECTIVE_STATE_ENABLED"
	case EffectiveDisabled:
		return "PROVIDER_EFFECTIVE_STATE_DISABLED"
	case EffectiveUnavailable:
		return "PROVIDER_EFFECTIVE_STATE_UNAVAILABLE"
	case EffectiveDisabling:
		return "PROVIDER_EFFECTIVE_STATE_DISABLING"
	default:
		return "PROVIDER_EFFECTIVE_STATE_UNSPECIFIED"
	}
}

func IntentFromProto(value string) (preferences.ProviderIntent, bool) {
	switch value {
	case "PROVIDER_INTENT_AUTO", string(preferences.ProviderIntentAuto):
		return preferences.ProviderIntentAuto, true
	case "PROVIDER_INTENT_ENABLED", string(preferences.ProviderIntentEnabled):
		return preferences.ProviderIntentEnabled, true
	case "PROVIDER_INTENT_DISABLED", string(preferences.ProviderIntentDisabled):
		return preferences.ProviderIntentDisabled, true
	default:
		return "", false
	}
}
