package store

import (
	"encoding/json"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
)

func exportReportingAGYUsage(db *gorm.DB, id string, s *reportingv1.SessionSnapshot) error {
	var session agySessionModel
	if err := db.Where("external_session_id = ?", id).Take(&session).Error; err != nil {
		return err
	}
	if session.Throughput != nil {
		if err := json.Unmarshal([]byte(*session.Throughput), &s.Throughput); err != nil {
			return err
		}
	}
	var rows []agyUsageModel
	if err := db.Where("external_session_id = ?", id).Order("occurred_at_ms,event_id").Limit(reportingv1.MaxSnapshotFacts + 1).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) > reportingv1.MaxSnapshotFacts {
		return ErrReportingBudget
	}
	var input, cached int64
	cacheKnown := s.Complete
	for _, r := range rows {
		c := reportingv1.Contribution{
			ObservedAtMS: &r.OccurredAtMS,
			Model:        r.ModelKey,
			OutputTokens: &r.OutputTokens,
			CostStatus:   "unpriced",
			PricingMode:  "event_cost",
		}
		if r.CacheReadKnown {
			c.CachedTokens = &r.CacheReadTokens
		}
		if r.ReasoningKnown {
			c.ReasoningTokens = &r.ReasoningTokens
		}
		if r.TotalKnown {
			c.TotalTokens = &r.TotalTokens
		}
		if r.CacheReadKnown {
			full, err := reportingTotal(r.UncachedInputTokens, r.CacheReadTokens)
			if err != nil {
				return err
			}
			c.InputTokens = &full
			input, err = checkedAdd(input, full)
			if err != nil {
				return err
			}
			cached, err = checkedAdd(cached, r.CacheReadTokens)
			if err != nil {
				return err
			}
			at := r.OccurredAtMS
			if r.StartedAtMS != nil && *r.StartedAtMS > 0 {
				at = *r.StartedAtMS
			}
			if r.ModelKey != nil {
				if rate, ok := pricing.AGYRateAt(*r.ModelKey, at); ok {
					if cost, ok := pricing.EstimateAGYCost(rate, r.UncachedInputTokens, r.CacheReadTokens, r.OutputTokens); ok {
						c.CostMicroUSD = &cost
						version := rate.PricingVersion()
						c.PricingVersion = &version
						c.CostStatus = "known"
					}
				}
			}
		} else {
			cacheKnown = false
		}
		s.Contributions = append(s.Contributions, c)
	}
	s.CacheUsage = &reportingv1.CacheUsageCapsule{Version: 1, Basis: "lifetime_cached_input", Reason: "unavailable"}
	if cacheKnown {
		s.CacheUsage.Reason = ""
		s.CacheUsage.InputTokens = &input
		s.CacheUsage.CachedInputTokens = &cached
	}
	return nil
}
