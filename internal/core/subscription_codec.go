package core

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptionaccounts"
	"github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptiontier"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type CodexSubscriptionAccountsView struct {
	Version                 string                               `json:"version"`
	EvaluatedAtMS           int64                                `json:"evaluatedAtMs"`
	TimeZone                string                               `json:"timeZone"`
	AutomaticDateCapability string                               `json:"automaticDateCapability"`
	Accounts                []codexSubscriptionAccountWire       `json:"accounts"`
	LinkCandidates          []codexSubscriptionLinkCandidateWire `json:"linkCandidates"`
}

type CodexSubscriptionMutationReceipt struct {
	Result string  `json:"result"`
	Reason *string `json:"reason,omitempty"`
}

type LegacyQuotaHistoryMutationReceipt struct {
	Result string  `json:"result"`
	Reason *string `json:"reason,omitempty"`
}

type codexSubscriptionAccountWire struct {
	AccountID                 string                  `json:"accountId"`
	DetectedAccountID         *string                 `json:"detectedAccountId,omitempty"`
	ManualEntryID             *string                 `json:"manualEntryId,omitempty"`
	Alias                     *string                 `json:"alias,omitempty"`
	DisplayEmail              *string                 `json:"displayEmail,omitempty"`
	DetectedEmail             *string                 `json:"detectedEmail,omitempty"`
	ManualEmail               *string                 `json:"manualEmail,omitempty"`
	Current                   bool                    `json:"current"`
	Detected                  bool                    `json:"detected"`
	HasManual                 bool                    `json:"hasManual"`
	Linked                    bool                    `json:"linked"`
	DetectedEmailObservedAtMS *int64                  `json:"detectedEmailObservedAtMs,omitempty"`
	AutomaticPlan             *string                 `json:"automaticPlan,omitempty"`
	AutomaticPlanState        string                  `json:"automaticPlanState"`
	AutomaticPlanSource       *string                 `json:"automaticPlanSource,omitempty"`
	AutomaticPlanObservedAtMS *int64                  `json:"automaticPlanObservedAtMs,omitempty"`
	ManualPlan                *string                 `json:"manualPlan,omitempty"`
	ResolvedPlan              *string                 `json:"resolvedPlan,omitempty"`
	ResolvedPlanSource        string                  `json:"resolvedPlanSource"`
	MembershipDate            *string                 `json:"membershipDate,omitempty"`
	DateKind                  *string                 `json:"dateKind,omitempty"`
	DateSource                string                  `json:"dateSource"`
	DateState                 string                  `json:"dateState"`
	DayDelta                  *int32                  `json:"dayDelta,omitempty"`
	DetectedRevision          *int64                  `json:"detectedRevision,omitempty"`
	ManualRevision            *int64                  `json:"manualRevision,omitempty"`
	LinkRevision              *int64                  `json:"linkRevision,omitempty"`
	LegacyQuotaHistory        *legacyQuotaHistoryWire `json:"legacyQuotaHistory,omitempty"`
}

type legacyQuotaHistoryWire struct {
	State               string `json:"state"`
	ObservationCount    int64  `json:"observationCount"`
	CycleCount          int64  `json:"cycleCount"`
	FirstObservedAtMS   *int64 `json:"firstObservedAtMs,omitempty"`
	LastObservedAtMS    *int64 `json:"lastObservedAtMs,omitempty"`
	AssociationRevision *int64 `json:"associationRevision,omitempty"`
}

type codexSubscriptionLinkCandidateWire struct {
	DetectedAccountID string `json:"detectedAccountId"`
	ManualEntryID     string `json:"manualEntryId"`
	Reason            string `json:"reason"`
}

func (snapshot AccountSnapshot) MarshalJSON() ([]byte, error) {
	encoded := struct {
		Account      *AccountIdentity              `json:"account,omitempty"`
		Binding      *store.CodexAccountBinding    `json:"binding,omitempty"`
		ProTier      *subscriptiontier.Snapshot    `json:"proTier,omitempty"`
		Subscription *codexSubscriptionAccountWire `json:"subscription,omitempty"`
	}{
		Account: snapshot.Account,
		Binding: snapshot.Binding,
		ProTier: snapshot.ProTier,
	}
	if snapshot.Subscription != nil {
		wire, err := encodeCodexSubscriptionAccount(*snapshot.Subscription)
		if err != nil {
			return nil, err
		}
		encoded.Subscription = wire
	}
	return json.Marshal(encoded)
}

func encodeCodexSubscriptionAccounts(snapshot subscriptionaccounts.Snapshot) (CodexSubscriptionAccountsView, error) {
	if snapshot.Version == "" {
		return CodexSubscriptionAccountsView{}, fmt.Errorf("%w: missing subscription accounts version", ErrProtoMapping)
	}
	capability, err := requiredSubscriptionEnum(
		"CODEX_SUBSCRIPTION_AUTOMATIC_DATE_CAPABILITY_",
		string(snapshot.AutomaticDateCapability),
	)
	if err != nil {
		return CodexSubscriptionAccountsView{}, err
	}
	accounts := make([]codexSubscriptionAccountWire, 0, len(snapshot.Accounts))
	for _, account := range snapshot.Accounts {
		wire, err := encodeCodexSubscriptionAccount(account)
		if err != nil {
			return CodexSubscriptionAccountsView{}, err
		}
		accounts = append(accounts, *wire)
	}
	candidates := make([]codexSubscriptionLinkCandidateWire, 0, len(snapshot.LinkCandidates))
	for _, candidate := range snapshot.LinkCandidates {
		if candidate.Reason != subscriptionaccounts.LinkCandidateSameEmail {
			return CodexSubscriptionAccountsView{}, fmt.Errorf("%w: unsupported link candidate reason", ErrProtoMapping)
		}
		candidates = append(candidates, codexSubscriptionLinkCandidateWire{
			DetectedAccountID: candidate.DetectedAccountID,
			ManualEntryID:     candidate.ManualEntryID,
			Reason:            candidate.Reason,
		})
	}
	return CodexSubscriptionAccountsView{
		Version:                 snapshot.Version,
		EvaluatedAtMS:           snapshot.EvaluatedAtMS,
		TimeZone:                snapshot.TimeZone,
		AutomaticDateCapability: capability,
		Accounts:                accounts,
		LinkCandidates:          candidates,
	}, nil
}

func encodeCodexSubscriptionAccount(account subscriptionaccounts.Account) (*codexSubscriptionAccountWire, error) {
	automaticState, err := requiredSubscriptionEnum(
		"CODEX_SUBSCRIPTION_AUTOMATIC_PLAN_STATE_",
		string(account.AutomaticPlanState),
	)
	if err != nil {
		return nil, err
	}
	resolvedSource, err := requiredSubscriptionEnum(
		"CODEX_SUBSCRIPTION_VALUE_SOURCE_",
		string(account.ResolvedPlanSource),
	)
	if err != nil {
		return nil, err
	}
	dateSource, err := requiredSubscriptionEnum(
		"CODEX_SUBSCRIPTION_VALUE_SOURCE_",
		string(account.DateSource),
	)
	if err != nil {
		return nil, err
	}
	dateState, err := requiredSubscriptionEnum(
		"CODEX_SUBSCRIPTION_DATE_STATE_",
		string(account.DateState),
	)
	if err != nil {
		return nil, err
	}
	automaticPlan, err := optionalSubscriptionPlan(account.AutomaticPlan)
	if err != nil {
		return nil, err
	}
	manualPlan, err := optionalSubscriptionPlan(account.ManualPlan)
	if err != nil {
		return nil, err
	}
	resolvedPlan, err := optionalSubscriptionPlan(account.ResolvedPlan)
	if err != nil {
		return nil, err
	}
	var automaticSource *string
	if account.AutomaticPlanSource != nil {
		encoded, err := requiredSubscriptionEnum(
			"CODEX_SUBSCRIPTION_AUTOMATIC_SOURCE_",
			string(*account.AutomaticPlanSource),
		)
		if err != nil {
			return nil, err
		}
		automaticSource = &encoded
	}
	var dateKind *string
	if account.DateKind != nil {
		encoded, err := requiredSubscriptionEnum(
			"CODEX_SUBSCRIPTION_DATE_KIND_",
			string(*account.DateKind),
		)
		if err != nil {
			return nil, err
		}
		dateKind = &encoded
	}
	legacyHistory, err := encodeLegacyQuotaHistory(account.LegacyQuotaHistory)
	if err != nil {
		return nil, err
	}
	return &codexSubscriptionAccountWire{
		AccountID:                 account.AccountID,
		DetectedAccountID:         account.DetectedAccountID,
		ManualEntryID:             account.ManualEntryID,
		Alias:                     account.Alias,
		DisplayEmail:              account.DisplayEmail,
		DetectedEmail:             account.DetectedEmail,
		ManualEmail:               account.ManualEmail,
		Current:                   account.Current,
		Detected:                  account.Detected,
		HasManual:                 account.HasManual,
		Linked:                    account.Linked,
		DetectedEmailObservedAtMS: account.DetectedEmailObservedAtMS,
		AutomaticPlan:             automaticPlan,
		AutomaticPlanState:        automaticState,
		AutomaticPlanSource:       automaticSource,
		AutomaticPlanObservedAtMS: account.AutomaticPlanObservedAtMS,
		ManualPlan:                manualPlan,
		ResolvedPlan:              resolvedPlan,
		ResolvedPlanSource:        resolvedSource,
		MembershipDate:            account.MembershipDate,
		DateKind:                  dateKind,
		DateSource:                dateSource,
		DateState:                 dateState,
		DayDelta:                  cloneWireInt32(account.DayDelta),
		DetectedRevision:          account.DetectedRevision,
		ManualRevision:            account.ManualRevision,
		LinkRevision:              account.LinkRevision,
		LegacyQuotaHistory:        legacyHistory,
	}, nil
}

func encodeLegacyQuotaHistory(
	status *subscriptionaccounts.LegacyQuotaHistory,
) (*legacyQuotaHistoryWire, error) {
	if status == nil {
		return nil, nil
	}
	if status.ObservationCount < 0 || status.CycleCount < 0 {
		return nil, fmt.Errorf("%w: invalid legacy quota history coverage", ErrProtoMapping)
	}
	state, err := requiredSubscriptionEnum(
		"CODEX_LEGACY_QUOTA_HISTORY_STATE_", string(status.State),
	)
	if err != nil {
		return nil, err
	}
	return &legacyQuotaHistoryWire{
		State: state, ObservationCount: status.ObservationCount, CycleCount: status.CycleCount,
		FirstObservedAtMS: status.FirstObservedAtMS, LastObservedAtMS: status.LastObservedAtMS,
		AssociationRevision: status.AssociationRevision,
	}, nil
}

func encodeCodexSubscriptionMutation(mutation CodexSubscriptionMutation) (CodexSubscriptionMutationReceipt, error) {
	result, err := encodeSubscriptionMutationResult(mutation.Result)
	if err != nil {
		return CodexSubscriptionMutationReceipt{}, err
	}
	reason, err := encodeSubscriptionMutationReason(mutation.Reason)
	if err != nil {
		return CodexSubscriptionMutationReceipt{}, err
	}
	return CodexSubscriptionMutationReceipt{Result: result, Reason: reason}, nil
}

func encodeLegacyQuotaHistoryMutation(
	mutation store.LegacyQuotaHistoryMutation,
) (LegacyQuotaHistoryMutationReceipt, error) {
	result, err := encodeSubscriptionMutationResult(string(mutation.Result))
	if err != nil {
		return LegacyQuotaHistoryMutationReceipt{}, err
	}
	if mutation.Reason != nil {
		switch *mutation.Reason {
		case store.LegacyQuotaHistoryReasonRevisionChanged,
			store.LegacyQuotaHistoryReasonAlreadyLinked,
			store.LegacyQuotaHistoryReasonCurrentAccountChanged,
			store.LegacyQuotaHistoryReasonLinkTargetChanged:
		default:
			return LegacyQuotaHistoryMutationReceipt{}, fmt.Errorf(
				"%w: unsupported legacy quota history mutation reason", ErrProtoMapping,
			)
		}
	}
	return LegacyQuotaHistoryMutationReceipt{Result: result, Reason: mutation.Reason}, nil
}

func encodeSubscriptionMutationResult(result string) (string, error) {
	switch subscriptionaccounts.MutationResult(result) {
	case subscriptionaccounts.MutationApplied, subscriptionaccounts.MutationNoop, subscriptionaccounts.MutationConflict:
		return requiredSubscriptionEnum("CODEX_SUBSCRIPTION_MUTATION_RESULT_", result)
	default:
		return "", fmt.Errorf("%w: unsupported subscription mutation result", ErrProtoMapping)
	}
}

func encodeSubscriptionMutationReason(reason *string) (*string, error) {
	if reason == nil || *reason == "" {
		return nil, nil
	}
	switch *reason {
	case subscriptionaccounts.ReasonRevisionChanged,
		subscriptionaccounts.ReasonAlreadyLinked,
		subscriptionaccounts.ReasonLinkTargetChanged,
		subscriptionaccounts.ReasonManualEmailRequiredBeforeUnlink,
		subscriptionaccounts.ReasonRequestIDReused,
		subscriptionaccounts.ReasonCurrentAccount:
		copied := *reason
		return &copied, nil
	default:
		return nil, fmt.Errorf("%w: unsupported subscription mutation reason", ErrProtoMapping)
	}
}

func optionalSubscriptionPlan(plan *subscriptionaccounts.Plan) (*string, error) {
	if plan == nil {
		return nil, nil
	}
	encoded, err := requiredSubscriptionEnum("CODEX_SUBSCRIPTION_PLAN_", string(*plan))
	if err != nil {
		return nil, err
	}
	return &encoded, nil
}

func requiredSubscriptionEnum(prefix, value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("%w: missing subscription enum", ErrProtoMapping)
	}
	allowed, ok := subscriptionEnumValues[prefix]
	if _, allowedValue := allowed[value]; !ok || !allowedValue {
		return "", fmt.Errorf("%w: unsupported subscription enum", ErrProtoMapping)
	}
	return prefix + strings.ToUpper(value), nil
}

var subscriptionEnumValues = map[string]map[string]struct{}{
	"CODEX_SUBSCRIPTION_PLAN_": enumValues(
		subscriptionaccounts.PlanFree, subscriptionaccounts.PlanGo, subscriptionaccounts.PlanPlus,
		subscriptionaccounts.PlanPro5X, subscriptionaccounts.PlanPro20X, subscriptionaccounts.PlanTeam,
		subscriptionaccounts.PlanBusiness, subscriptionaccounts.PlanEnterprise, subscriptionaccounts.PlanEdu,
	),
	"CODEX_SUBSCRIPTION_AUTOMATIC_PLAN_STATE_": enumValues(
		subscriptionaccounts.AutomaticPlanUnavailable, subscriptionaccounts.AutomaticPlanKnown,
		subscriptionaccounts.AutomaticPlanUnknown, subscriptionaccounts.AutomaticPlanConflict,
	),
	"CODEX_SUBSCRIPTION_VALUE_SOURCE_": enumValues(
		subscriptionaccounts.ValueSourceUnavailable, subscriptionaccounts.ValueSourceAutomatic,
		subscriptionaccounts.ValueSourceManual,
	),
	"CODEX_SUBSCRIPTION_DATE_KIND_": enumValues(
		subscriptionaccounts.DateKindNextRenewal, subscriptionaccounts.DateKindMembershipExpiry,
	),
	"CODEX_SUBSCRIPTION_DATE_STATE_": enumValues(
		subscriptionaccounts.DateStateUnavailable, subscriptionaccounts.DateStateFuture,
		subscriptionaccounts.DateStateToday, subscriptionaccounts.DateStateNeedsUpdate,
	),
	"CODEX_SUBSCRIPTION_AUTOMATIC_SOURCE_": enumValues(
		subscriptionaccounts.AutomaticSourceAccountSandwich,
	),
	"CODEX_SUBSCRIPTION_AUTOMATIC_DATE_CAPABILITY_": enumValues(
		subscriptionaccounts.AutomaticDateCapabilityManualOnly,
	),
	"CODEX_SUBSCRIPTION_MUTATION_RESULT_": enumValues(
		subscriptionaccounts.MutationApplied, subscriptionaccounts.MutationNoop,
		subscriptionaccounts.MutationConflict,
	),
	"CODEX_LEGACY_QUOTA_HISTORY_STATE_": enumValues(
		subscriptionaccounts.LegacyQuotaHistoryUnavailable,
		subscriptionaccounts.LegacyQuotaHistoryAvailable,
		subscriptionaccounts.LegacyQuotaHistoryLinked,
		subscriptionaccounts.LegacyQuotaHistoryLinkedElsewhere,
	),
}

func enumValues[Value ~string](values ...Value) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[string(value)] = struct{}{}
	}
	return result
}

func cloneWireInt32(value *int) *int32 {
	if value == nil {
		return nil
	}
	copied := int32(*value)
	return &copied
}
