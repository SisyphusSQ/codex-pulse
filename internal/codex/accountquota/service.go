package accountquota

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/subscriptionaccounts"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

var ErrInvalidService = errors.New("codex account quota service is invalid")

type RecordsReader interface {
	CodexAccountQuotaRecords(context.Context, int64) (store.CodexAccountQuotaRecords, error)
}

type Service struct {
	reader RecordsReader
}

func NewService(reader RecordsReader) (*Service, error) {
	if reader == nil {
		return nil, ErrInvalidService
	}
	return &Service{reader: reader}, nil
}

func (service *Service) List(
	ctx context.Context,
	evaluatedAtMS int64,
	timeZone string,
) (Snapshot, error) {
	if service == nil || service.reader == nil {
		return Snapshot{}, ErrInvalidService
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	records, err := service.reader.CodexAccountQuotaRecords(ctx, evaluatedAtMS)
	if err != nil {
		return Snapshot{}, err
	}
	subscriptions, err := subscriptionaccounts.Project(records.Subscriptions, evaluatedAtMS, timeZone)
	if err != nil {
		return Snapshot{}, err
	}
	quotaByDetectedID := make(map[string][]store.QuotaCurrent, len(records.Accounts))
	for _, account := range records.Accounts {
		quotaByDetectedID[account.DetectedAccountID] = account.Windows
	}
	accounts := make([]Account, 0, len(records.Accounts))
	for _, subscription := range subscriptions.Accounts {
		if !subscription.Detected || subscription.DetectedAccountID == nil {
			continue
		}
		windows := projectWindows(quotaByDetectedID[*subscription.DetectedAccountID])
		if !subscription.Current && len(windows) == 0 {
			continue
		}
		accounts = append(accounts, Account{
			Subscription:      subscription,
			Windows:           windows,
			LastCollectedAtMS: latestCollectedAt(windows),
		})
	}
	return Snapshot{
		Version: ContractVersion, EvaluatedAtMS: evaluatedAtMS, TimeZone: timeZone, Accounts: accounts,
	}, nil
}

func projectWindows(currents []store.QuotaCurrent) []Window {
	windows := make([]Window, 0, len(currents))
	for _, current := range currents {
		var remaining *float64
		if current.EffectiveUsedPercent != nil {
			value := 100 - *current.EffectiveUsedPercent
			remaining = &value
		}
		windows = append(windows, Window{
			WindowKind: current.WindowKind, LimitID: current.LimitID,
			UsedPercent: current.EffectiveUsedPercent, RemainingPercent: remaining,
			WindowMinutes: current.WindowMinutes, ResetsAtMS: current.ResetsAtMS,
			LastCollectedAtMS: current.LastSuccessAtMS,
			Freshness:         current.FreshnessState, Conflict: current.ConflictState,
		})
	}
	slices.SortFunc(windows, func(left, right Window) int {
		if value := cmp.Compare(windowRank(left.WindowKind), windowRank(right.WindowKind)); value != 0 {
			return value
		}
		return cmp.Compare(left.LimitID, right.LimitID)
	})
	return windows
}

func latestCollectedAt(windows []Window) *int64 {
	var latest *int64
	for _, window := range windows {
		if window.LastCollectedAtMS == nil || latest != nil && *latest >= *window.LastCollectedAtMS {
			continue
		}
		latest = new(*window.LastCollectedAtMS)
	}
	return latest
}

func windowRank(kind store.QuotaWindowKind) int {
	switch kind {
	case store.QuotaWindowPrimary:
		return 0
	case store.QuotaWindowSecondary:
		return 1
	default:
		return 2
	}
}
