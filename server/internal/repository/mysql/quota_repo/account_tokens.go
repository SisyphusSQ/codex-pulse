package quota_repo

import (
	"context"
	"math/big"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

const accountTokenFactLimit = 200000

func (r *Quota) AccountTokenTotal(ctx context.Context, q quota_dto.Query, account string, start, end, now int64) (string, bool, error) {
	db := r.engine.DB(ctx)
	periods := db.Model(&reporting_do.AccountTokenPeriod{}).Where("account_key = ? AND provider = 'codex' AND collected_at_ms >= ? AND collected_at_ms <= ? AND window_start_at_ms <= ? AND resets_at_ms > ?", account, start, now+5*60*1000, now, now)
	if q.ClientID != "" {
		periods = periods.Where("client_id = ?", q.ClientID)
	}
	periods = periods.Select("1")
	facts := db.Where("account_key = ? AND provider = 'codex' AND observed_at_ms >= ? AND observed_at_ms < ? AND observed_at_ms <= ?", account, start, end, now)
	if q.ClientID != "" {
		facts = facts.Where("EXISTS (SELECT 1 FROM pulse_account_token_sources s WHERE s.fact_id = pulse_account_token_facts.id AND s.client_id = ?)", q.ClientID)
	}
	// 限额内分别累加整数商与余数，两部分都能放入 int64，兼容 SQLite 的整数 SUM。
	const base = 1000000
	bounded := facts.Model(&reporting_do.AccountTokenFact{}).Select("total_tokens").Where("EXISTS (?)", periods).Limit(accountTokenFactLimit + 1)
	var available bool
	var count, high, low int64
	err := db.Table("(?) AS facts", bounded).
		Select("EXISTS (?), COUNT(*), COALESCE(SUM(CAST((total_tokens - total_tokens % ?) / ? AS SIGNED)), 0), COALESCE(SUM(total_tokens % ?), 0)", periods, base, base, base).
		Row().Scan(&available, &count, &high, &low)
	if err != nil || !available {
		return "", false, err
	}
	if count > accountTokenFactLimit {
		return "", false, utils.ErrRequestBudget
	}
	total := new(big.Int).Mul(big.NewInt(high), big.NewInt(base))
	return total.Add(total, big.NewInt(low)).String(), true, nil
}
