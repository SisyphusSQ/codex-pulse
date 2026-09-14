package appserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/SisyphusSQ/codex-pulse/internal/runtimeclock"
)

const (
	maxAccountIDBytes           = 256
	maxRateLimitIdentityBytes   = 512
	maxRateLimitWindowMinutes   = 525600
	accountRateLimitsReadMethod = "account/rateLimits/read"
)

var (
	ErrAccountIdentityUnavailable   = errors.New("App Server account identity unavailable")
	ErrRateLimitsSchemaIncompatible = errors.New("App Server rate limits schema incompatible")
)

type SensitiveAccountID []byte

type RateLimitWindow struct {
	UsedPercent        int32
	WindowDurationMins *int64
	ResetsAtSeconds    *int64
}

type RateLimitSnapshot struct {
	LimitID             *string
	LimitName           *string
	PlanType            *string
	Primary             *RateLimitWindow
	Secondary           *RateLimitWindow
	SpendControlReached *bool
}

type RateLimitResetCredit struct {
	ID               string
	Status           string
	ResetType        string
	GrantedAtSeconds int64
	ExpiresAtSeconds *int64
}

type RateLimitResetCreditsSummary struct {
	AvailableCount int64
	Credits        []RateLimitResetCredit
}

type AccountRateLimitsSnapshot struct {
	AccountID             SensitiveAccountID
	RateLimits            RateLimitSnapshot
	RateLimitsByLimitID   map[string]RateLimitSnapshot
	RateLimitResetCredits *RateLimitResetCreditsSummary
	OrdinaryUsageAllowed  *bool
}

type accountRateLimitsReadParams struct {
	ExcludeResetCreditDetails bool `json:"excludeResetCreditDetails"`
}

type rateLimitWindowWire struct {
	UsedPercent        *int32 `json:"usedPercent"`
	WindowDurationMins *int64 `json:"windowDurationMins"`
	ResetsAt           *int64 `json:"resetsAt"`
}

type rateLimitSnapshotWire struct {
	LimitID             *string              `json:"limitId"`
	LimitName           *string              `json:"limitName"`
	PlanType            *string              `json:"planType"`
	Primary             *rateLimitWindowWire `json:"primary"`
	Secondary           *rateLimitWindowWire `json:"secondary"`
	SpendControlReached *bool                `json:"spendControlReached"`
}

type rateLimitResetCreditWire struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	ResetType string `json:"resetType"`
	GrantedAt *int64 `json:"grantedAt"`
	ExpiresAt *int64 `json:"expiresAt"`
}

type rateLimitResetCreditsWire struct {
	AvailableCount *int64                      `json:"availableCount"`
	Credits        *[]rateLimitResetCreditWire `json:"credits"`
}

type accountRateLimitsReadResult struct {
	AccountID             *string                           `json:"accountId"`
	RateLimits            *rateLimitSnapshotWire            `json:"rateLimits"`
	RateLimitsByLimitID   *map[string]rateLimitSnapshotWire `json:"rateLimitsByLimitId"`
	RateLimitResetCredits *rateLimitResetCreditsWire        `json:"rateLimitResetCredits"`
	OrdinaryUsageAllowed  *bool                             `json:"ordinaryUsageAllowed"`
}

func ReadLocalAccountRateLimits(
	ctx context.Context,
	confirmedHome ConfirmedHome,
	options ProcessOptions,
	excludeResetCreditDetails bool,
) (AccountRateLimitsSnapshot, error) {
	binding, err := openConfirmedHomeBinding(ctx, confirmedHome)
	if err != nil {
		return AccountRateLimitsSnapshot{}, err
	}
	defer func() { _ = binding.close() }()
	beforeStart := options.BeforeStart
	options.BeforeStart = func(startContext context.Context) error {
		if beforeStart != nil {
			if err := beforeStart(startContext); err != nil {
				return err
			}
		}
		return binding.validate(startContext)
	}
	options.homeBinding = binding
	snapshot, err := withInitializedLocalRPC(
		ctx,
		confirmedHome.Path,
		options,
		func(ctx context.Context, rpc *jsonLineRPC, _ string) (AccountRateLimitsSnapshot, error) {
			return readAccountRateLimits(ctx, rpc, excludeResetCreditDetails)
		},
	)
	if err != nil {
		return AccountRateLimitsSnapshot{}, err
	}
	if err := binding.validate(ctx); err != nil {
		return AccountRateLimitsSnapshot{}, err
	}
	return snapshot, nil
}

func readAccountRateLimits(
	ctx context.Context,
	rpc RPC,
	excludeResetCreditDetails bool,
) (AccountRateLimitsSnapshot, error) {
	if ctx == nil || rpc == nil {
		return AccountRateLimitsSnapshot{}, errors.New("invalid rate limits reader")
	}
	var result json.RawMessage
	if err := rpc.Call(
		ctx,
		accountRateLimitsReadMethod,
		accountRateLimitsReadParams{ExcludeResetCreditDetails: excludeResetCreditDetails},
		&result,
	); err != nil {
		return AccountRateLimitsSnapshot{}, err
	}
	return NormalizeAccountRateLimits(result)
}

func NormalizeAccountRateLimits(raw []byte) (AccountRateLimitsSnapshot, error) {
	if len(bytes.TrimSpace(raw)) == 0 || !json.Valid(raw) {
		return AccountRateLimitsSnapshot{}, ErrRateLimitsSchemaIncompatible
	}
	var wire accountRateLimitsReadResult
	if err := json.Unmarshal(raw, &wire); err != nil || wire.RateLimits == nil {
		return AccountRateLimitsSnapshot{}, ErrRateLimitsSchemaIncompatible
	}
	accountID, err := normalizeSensitiveAccountID(wire.AccountID)
	if err != nil {
		return AccountRateLimitsSnapshot{}, err
	}
	rateLimits, err := normalizeRateLimitSnapshot(*wire.RateLimits, "")
	if err != nil {
		return AccountRateLimitsSnapshot{}, err
	}
	snapshot := AccountRateLimitsSnapshot{
		AccountID:            accountID,
		RateLimits:           rateLimits,
		OrdinaryUsageAllowed: cloneOptionalBool(wire.OrdinaryUsageAllowed),
	}
	if wire.RateLimitsByLimitID != nil {
		snapshot.RateLimitsByLimitID = make(map[string]RateLimitSnapshot, len(*wire.RateLimitsByLimitID))
		for key, bucket := range *wire.RateLimitsByLimitID {
			if !validRateLimitIdentity(key) {
				return AccountRateLimitsSnapshot{}, ErrRateLimitsSchemaIncompatible
			}
			normalized, normalizeErr := normalizeRateLimitSnapshot(bucket, key)
			if normalizeErr != nil {
				return AccountRateLimitsSnapshot{}, normalizeErr
			}
			snapshot.RateLimitsByLimitID[key] = normalized
		}
	}
	credits, err := normalizeRateLimitResetCredits(wire.RateLimitResetCredits)
	if err != nil {
		return AccountRateLimitsSnapshot{}, err
	}
	snapshot.RateLimitResetCredits = credits
	return snapshot, nil
}

func normalizeSensitiveAccountID(raw *string) (SensitiveAccountID, error) {
	if raw == nil || *raw == "" || len(*raw) > maxAccountIDBytes || !utf8.ValidString(*raw) {
		return nil, ErrAccountIdentityUnavailable
	}
	if strings.TrimSpace(*raw) != *raw {
		return nil, ErrAccountIdentityUnavailable
	}
	return SensitiveAccountID(append([]byte(nil), *raw...)), nil
}

func normalizeRateLimitSnapshot(wire rateLimitSnapshotWire, expectedLimitID string) (RateLimitSnapshot, error) {
	limitID, ok := normalizeOptionalIdentity(wire.LimitID)
	if !ok {
		return RateLimitSnapshot{}, ErrRateLimitsSchemaIncompatible
	}
	if expectedLimitID != "" && limitID != nil && *limitID != expectedLimitID {
		return RateLimitSnapshot{}, ErrRateLimitsSchemaIncompatible
	}
	limitName, ok := normalizeOptionalIdentity(wire.LimitName)
	if !ok {
		return RateLimitSnapshot{}, ErrRateLimitsSchemaIncompatible
	}
	planType, ok := normalizedOptionalAccountField(wire.PlanType, maxAccountPlanBytes)
	if !ok {
		return RateLimitSnapshot{}, ErrRateLimitsSchemaIncompatible
	}
	primary, err := normalizeRateLimitWindow(wire.Primary)
	if err != nil {
		return RateLimitSnapshot{}, err
	}
	secondary, err := normalizeRateLimitWindow(wire.Secondary)
	if err != nil {
		return RateLimitSnapshot{}, err
	}
	return RateLimitSnapshot{
		LimitID:             limitID,
		LimitName:           limitName,
		PlanType:            planType,
		Primary:             primary,
		Secondary:           secondary,
		SpendControlReached: cloneOptionalBool(wire.SpendControlReached),
	}, nil
}

func normalizeRateLimitWindow(wire *rateLimitWindowWire) (*RateLimitWindow, error) {
	if wire == nil {
		return nil, nil
	}
	if wire.UsedPercent == nil || *wire.UsedPercent < 0 || *wire.UsedPercent > 100 {
		return nil, ErrRateLimitsSchemaIncompatible
	}
	if wire.WindowDurationMins != nil &&
		(*wire.WindowDurationMins <= 0 || *wire.WindowDurationMins > maxRateLimitWindowMinutes) {
		return nil, ErrRateLimitsSchemaIncompatible
	}
	if wire.ResetsAt != nil && !validStoreUnixSeconds(*wire.ResetsAt) {
		return nil, ErrRateLimitsSchemaIncompatible
	}
	return &RateLimitWindow{
		UsedPercent:        *wire.UsedPercent,
		WindowDurationMins: cloneOptionalInt64(wire.WindowDurationMins),
		ResetsAtSeconds:    cloneOptionalInt64(wire.ResetsAt),
	}, nil
}

func normalizeRateLimitResetCredits(wire *rateLimitResetCreditsWire) (*RateLimitResetCreditsSummary, error) {
	if wire == nil {
		return nil, nil
	}
	if wire.AvailableCount == nil || *wire.AvailableCount < 0 {
		return nil, ErrRateLimitsSchemaIncompatible
	}
	summary := &RateLimitResetCreditsSummary{AvailableCount: *wire.AvailableCount}
	if wire.Credits == nil {
		return summary, nil
	}
	summary.Credits = make([]RateLimitResetCredit, 0, len(*wire.Credits))
	for _, credit := range *wire.Credits {
		normalized, err := normalizeRateLimitResetCredit(credit)
		if err != nil {
			return nil, err
		}
		summary.Credits = append(summary.Credits, normalized)
	}
	return summary, nil
}

func normalizeRateLimitResetCredit(wire rateLimitResetCreditWire) (RateLimitResetCredit, error) {
	if wire.ID == "" || len(wire.ID) > maxAccountIDBytes || !utf8.ValidString(wire.ID) ||
		strings.TrimSpace(wire.ID) != wire.ID || wire.GrantedAt == nil || !validStoreUnixSeconds(*wire.GrantedAt) {
		return RateLimitResetCredit{}, ErrRateLimitsSchemaIncompatible
	}
	if wire.ExpiresAt != nil && !validStoreUnixSeconds(*wire.ExpiresAt) {
		return RateLimitResetCredit{}, ErrRateLimitsSchemaIncompatible
	}
	return RateLimitResetCredit{
		ID:               wire.ID,
		Status:           normalizeResetCreditStatus(wire.Status),
		ResetType:        normalizeResetCreditType(wire.ResetType),
		GrantedAtSeconds: *wire.GrantedAt,
		ExpiresAtSeconds: cloneOptionalInt64(wire.ExpiresAt),
	}, nil
}

func normalizeResetCreditStatus(value string) string {
	switch value {
	case "available", "redeeming", "redeemed", "unknown":
		return value
	default:
		return "unknown"
	}
}

func normalizeResetCreditType(value string) string {
	switch value {
	case "codexRateLimits", "unknown":
		return value
	default:
		return "unknown"
	}
}

func normalizeOptionalIdentity(value *string) (*string, bool) {
	if value == nil {
		return nil, true
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil, true
	}
	if !validRateLimitIdentity(normalized) {
		return nil, false
	}
	return &normalized, true
}

func validRateLimitIdentity(value string) bool {
	return value != "" && len(value) <= maxRateLimitIdentityBytes && utf8.ValidString(value) &&
		strings.TrimSpace(value) == value
}

func validStoreUnixSeconds(value int64) bool {
	return value >= 0 && value <= runtimeclock.MaxTimestampMS/1000
}

func cloneOptionalBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneOptionalInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
