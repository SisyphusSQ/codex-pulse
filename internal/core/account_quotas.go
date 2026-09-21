package core

import (
	"context"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/accountquota"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
)

type CodexAccountQuotas interface {
	ListCodexAccountQuotas(context.Context, int64, string) (accountquota.Snapshot, error)
	ClearCodexAccountQuotaHistory(context.Context) (accountquota.ClearReceipt, error)
}

type CodexAccountQuotaWindowView struct {
	WindowKind        string   `json:"windowKind"`
	LimitID           string   `json:"limitId"`
	UsedPercent       *float64 `json:"usedPercent,omitempty"`
	RemainingPercent  *float64 `json:"remainingPercent,omitempty"`
	WindowMinutes     *int64   `json:"windowMinutes,omitempty"`
	ResetsAtMS        *int64   `json:"resetsAtMs,omitempty"`
	LastCollectedAtMS *int64   `json:"lastCollectedAtMs,omitempty"`
	Freshness         string   `json:"freshness"`
	Conflict          string   `json:"conflict"`
}

type CodexAccountQuotaView struct {
	Account           codexSubscriptionAccountWire  `json:"account"`
	Windows           []CodexAccountQuotaWindowView `json:"windows"`
	LastCollectedAtMS *int64                        `json:"lastCollectedAtMs,omitempty"`
}

type CodexAccountQuotasView struct {
	Meta          basequery.ResponseMeta  `json:"meta"`
	Version       string                  `json:"version"`
	EvaluatedAtMS int64                   `json:"evaluatedAtMs"`
	TimeZone      string                  `json:"timeZone"`
	Accounts      []CodexAccountQuotaView `json:"accounts"`
}

func (service *Service) ListCodexAccountQuotas(
	ctx context.Context,
	evaluatedAtMS int64,
	timeZone string,
) (CodexAccountQuotasView, error) {
	if service == nil || service.codexAccountQuotas == nil {
		return CodexAccountQuotasView{}, newServiceFailure(ErrService)
	}
	if err := validateSubscriptionEvaluation(evaluatedAtMS, timeZone); err != nil {
		return CodexAccountQuotasView{}, newServiceFailure(err)
	}
	return serviceQueryCall(service, func() (CodexAccountQuotasView, error) {
		snapshot, err := service.codexAccountQuotas.ListCodexAccountQuotas(ctx, evaluatedAtMS, timeZone)
		if err != nil {
			return CodexAccountQuotasView{}, mapCodexSubscriptionError(err)
		}
		meta, err := basequery.NewResponseMeta(basequery.ResponseComplete, nil, nil)
		if err != nil {
			return CodexAccountQuotasView{}, err
		}
		accounts := make([]CodexAccountQuotaView, 0, len(snapshot.Accounts))
		for _, account := range snapshot.Accounts {
			wire, err := encodeCodexSubscriptionAccount(account.Subscription)
			if err != nil {
				return CodexAccountQuotasView{}, err
			}
			windows := make([]CodexAccountQuotaWindowView, 0, len(account.Windows))
			for _, window := range account.Windows {
				windows = append(windows, CodexAccountQuotaWindowView{
					WindowKind: string(window.WindowKind), LimitID: window.LimitID,
					UsedPercent: window.UsedPercent, RemainingPercent: window.RemainingPercent,
					WindowMinutes: window.WindowMinutes, ResetsAtMS: window.ResetsAtMS,
					LastCollectedAtMS: window.LastCollectedAtMS,
					Freshness:         string(window.Freshness), Conflict: string(window.Conflict),
				})
			}
			accounts = append(accounts, CodexAccountQuotaView{
				Account: *wire, Windows: windows, LastCollectedAtMS: account.LastCollectedAtMS,
			})
		}
		return CodexAccountQuotasView{
			Meta: meta, Version: snapshot.Version, EvaluatedAtMS: snapshot.EvaluatedAtMS,
			TimeZone: snapshot.TimeZone, Accounts: accounts,
		}, nil
	})
}

func (service *Service) ClearCodexAccountQuotaHistory(
	ctx context.Context,
) (accountquota.ClearReceipt, error) {
	if service == nil || service.codexAccountQuotas == nil {
		return accountquota.ClearReceipt{}, newServiceFailure(ErrService)
	}
	return serviceCall(func() (accountquota.ClearReceipt, error) {
		return service.codexAccountQuotas.ClearCodexAccountQuotaHistory(ctx)
	})
}
