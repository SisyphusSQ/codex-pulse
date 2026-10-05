package quota_repo

import (
	"context"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func (r *Quota) AccountTokenFacts(ctx context.Context, q quota_dto.Query, account string, start, end, now int64) (rows []reporting_do.AccountTokenFact, available bool, err error) {
	db := r.engine.DB(ctx)
	periods := db.Model(&reporting_do.AccountTokenPeriod{}).Where("account_key = ? AND provider = 'codex' AND collected_at_ms >= ? AND collected_at_ms <= ? AND window_start_at_ms <= ? AND resets_at_ms > ?", account, start, now+5*60*1000, now, now)
	if q.ClientID != "" {
		periods = periods.Where("client_id = ?", q.ClientID)
	}
	var count int64
	if err = periods.Count(&count).Error; err != nil || count == 0 {
		return
	}
	available = true
	facts := db.Where("account_key = ? AND provider = 'codex' AND observed_at_ms >= ? AND observed_at_ms < ? AND observed_at_ms <= ?", account, start, end, now)
	if q.ClientID != "" {
		facts = facts.Where("EXISTS (SELECT 1 FROM pulse_account_token_sources s WHERE s.fact_id = pulse_account_token_facts.id AND s.client_id = ?)", q.ClientID)
	}
	err = facts.Select("total_tokens").Limit(200001).Find(&rows).Error
	if len(rows) > 200000 {
		return nil, false, utils.ErrRequestBudget
	}
	return
}
