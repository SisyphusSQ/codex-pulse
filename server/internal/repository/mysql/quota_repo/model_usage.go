package quota_repo

import (
	"context"

	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	reporting_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

// StreamAccountModelFacts 以账号事实为准，发布的会话贡献只补充模型和 Token 分项。
func (r *Quota) StreamAccountModelFacts(ctx context.Context, q quota_dto.Query, account string, start, end, now int64, visit func(quota_dto.AccountModelFact)) (bool, error) {
	db := r.engine.DB(ctx)
	var available bool
	if err := db.Raw("SELECT EXISTS (?)", r.accountTokenPeriods(ctx, q, account, start, now)).Row().Scan(&available); err != nil || !available {
		return false, err
	}

	facts := r.accountTokenFacts(ctx, q, account, start, end, now).Select("id, observed_at_ms, total_tokens")
	usage := reporting_repo.PublishedUsage(r.engine.DB(ctx)).
		Select("u.contribution_id, u.observed_at_ms, u.total_tokens, u.model, u.input_tokens, u.cached_tokens, u.output_tokens, u.reasoning_tokens").
		Joins("JOIN pulse_sessions s ON s.id = u.session_key AND s.provider = 'codex' AND s.deleted = 0").
		Where("u.observed_at_ms >= ? AND u.observed_at_ms < ? AND u.observed_at_ms <= ?", start, end, now)
	rows, err := db.Table("(?) AS facts", facts).
		Joins("LEFT JOIN (?) AS matched_usage ON matched_usage.observed_at_ms = facts.observed_at_ms AND matched_usage.contribution_id = facts.id AND matched_usage.total_tokens = facts.total_tokens", usage).
		Select("facts.total_tokens, matched_usage.contribution_id IS NOT NULL, matched_usage.model, matched_usage.input_tokens, matched_usage.cached_tokens, matched_usage.output_tokens, matched_usage.reasoning_tokens").Rows()
	if err != nil {
		return false, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > accountTokenFactLimit {
			return false, utils.ErrRequestBudget
		}
		var row quota_dto.AccountModelFact
		if err := rows.Scan(&row.TotalTokens, &row.Matched, &row.Model, &row.InputTokens, &row.CachedTokens, &row.OutputTokens, &row.ReasoningTokens); err != nil {
			return false, err
		}
		visit(row)
	}
	return true, rows.Err()
}
