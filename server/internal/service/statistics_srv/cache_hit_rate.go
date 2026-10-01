package statistics_srv

import (
	"reflect"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/cachehitrate"
	reporting_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/reporting_dto"
	statistics_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/statistics_vo"
)

func cacheHitRateView(chosen reportingv1.SessionSnapshot, owner, provider string, sources []reporting_dto.SourceSnapshot) *statistics_vo.CacheHitRateView {
	v := &statistics_vo.CacheHitRateView{Unit: "basis_points", Basis: "lifetime_cached_input", Status: "unavailable", Reason: "not_reported"}
	if provider != "codex" {
		v.Reason = "unsupported_provider"
		return v
	}
	c := chosen.CacheUsage
	if c == nil {
		return v
	}
	v.Reason = c.Reason
	v.InputTokens, v.CachedInputTokens = throughputDecimal(c.InputTokens), throughputDecimal(c.CachedInputTokens)
	if owner != "" {
		v.SourceClientID = new(owner)
	}
	if c.InputTokens == nil || c.CachedInputTokens == nil {
		return v
	}
	v.BasisPoints = throughputDecimal(cachehitrate.BasisPoints(*c.InputTokens, *c.CachedInputTokens))
	if v.BasisPoints == nil {
		v.Reason = "unavailable"
		if *c.InputTokens == 0 && *c.CachedInputTokens == 0 {
			v.Reason = "not_applicable"
		}
		return v
	}
	v.Status = "complete"
	if !chosen.Complete {
		v.Status = "partial"
	}
	for _, source := range sources {
		if chosen.Complete && source.Snapshot.Complete && source.Snapshot.CacheUsage != nil && sameThroughputFacts(chosen, source.Snapshot) && !reflect.DeepEqual(c, source.Snapshot.CacheUsage) {
			v.Conflict, v.Status, v.Reason = true, "partial", "source_conflict"
		}
	}
	return v
}
