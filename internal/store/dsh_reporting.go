package store

import (
	"encoding/json"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
)

func exportReportingDSHUsage(db *gorm.DB, id string, s *reportingv1.SessionSnapshot) error {
	var session dshSessionModel
	if err := db.Where("external_session_id = ?", id).Take(&session).Error; err != nil {
		return err
	}
	if session.Throughput != nil {
		if err := json.Unmarshal([]byte(*session.Throughput), &s.Throughput); err != nil {
			return err
		}
	}
	var rows []dshUsageModel
	if err := db.Where("external_session_id = ?", id).Order("occurred_at_ms,event_id").Limit(reportingv1.MaxSnapshotFacts + 1).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) > reportingv1.MaxSnapshotFacts {
		return ErrReportingBudget
	}
	var input, cached int64
	cacheKnown := s.Complete
	for _, r := range rows {
		c := reportingv1.Contribution{ObservedAtMS: &r.OccurredAtMS, Model: r.ModelKey, OutputTokens: &r.OutputTokens, CostStatus: "unpriced", PricingMode: "event_cost"}
		if r.CacheReadKnown {
			c.CachedTokens = &r.CachedReadTokens
		}
		if r.CacheWriteKnown {
			c.CacheWriteTokens = &r.CacheCreationTokens
		}
		if r.ReasoningKnown {
			c.ReasoningTokens = &r.ReasoningTokens
		}
		if r.TotalKnown {
			c.TotalTokens = &r.TotalTokens
		}
		if r.CacheReadKnown && r.CacheWriteKnown {
			full, err := reportingTotal(r.InputTokens, r.CachedReadTokens, r.CacheCreationTokens)
			if err != nil {
				return err
			}
			c.InputTokens = &full
			input, err = checkedAdd(input, full)
			if err != nil {
				return err
			}
			cached, err = checkedAdd(cached, r.CachedReadTokens)
			if err != nil {
				return err
			}
			at := r.OccurredAtMS
			if r.StartedAtMS != nil {
				at = *r.StartedAtMS
			}
			if r.ModelKey != nil && r.StartedAtMS != nil {
				if rate, ok := pricing.DSHRateAt(r.ModelProvider, *r.ModelKey, at); ok {
					if cost, ok := pricing.EstimateDSHCost(rate, r.InputTokens, r.CachedReadTokens, r.CacheCreationTokens, r.OutputTokens); ok {
						c.CostMicroUSD = &cost
						version := rate.Version + ":" + rate.Period
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
