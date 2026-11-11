package quota_srv

import (
	"context"
	"sort"

	"github.com/SisyphusSQ/codex-pulse/internal/creditbalance"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	quota_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/quota_vo"
)

func (s *Quota) paidCredits(ctx context.Context, q quota_dto.Query, now int64) ([]quota_vo.PaidCredit, error) {
	rows, err := s.repository.PaidCredits(ctx, q, now)
	if err != nil {
		return nil, err
	}
	groups := map[string][]creditbalance.Snapshot{}
	for _, row := range rows {
		groups[row.AccountKey] = append(groups[row.AccountKey], creditbalance.Snapshot{LimitID: row.LimitID, State: row.State, Balance: row.Balance, HasCredits: row.HasCredits, Unlimited: row.Unlimited, ObservedAtMS: row.ObservedAtMS})
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := []quota_vo.PaidCredit{}
	for _, key := range keys {
		for _, view := range creditbalance.Views(groups[key], now, 0, false) {
			out = append(out, quota_vo.PaidCredit{AccountKey: key, Credit: view})
		}
	}
	return out, nil
}
