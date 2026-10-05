package quota_srv

import (
	"context"
	"math/big"

	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	quota_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/quota_vo"
)

func (s *Quota) withAccountTokens(ctx context.Context, q quota_dto.Query, window *quota_vo.Window, now int64) error {
	if window.Provider != "codex" || window.AccountKey == nil || window.LimitID != "codex" || window.WindowMinutes == nil || *window.WindowMinutes != 10080 || window.Current.WindowStartAtMS == nil || window.Current.ResetsAtMS == nil || *window.Current.WindowStartAtMS > now || *window.Current.ResetsAtMS <= now {
		return nil
	}
	rows, available, err := s.repository.AccountTokenFacts(ctx, q, *window.AccountKey, *window.Current.WindowStartAtMS, *window.Current.ResetsAtMS, now)
	if err != nil || !available {
		return err
	}
	total := new(big.Int)
	for _, row := range rows {
		total.Add(total, big.NewInt(row.TotalTokens))
	}
	window.RecordedTokens = new(total.String())
	return nil
}
