package reportingv1

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"
	"unicode/utf8"
)

var ErrVersion = errors.New("unsupported reporting version")
var ErrInvalid = errors.New("invalid reporting facts")

func provider(value string) bool { return value == "codex" || value == "cursor" || value == "grok" }
func text(value string, max int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= max && !strings.ContainsRune(value, 0)
}
func identifier(value string, max int) bool {
	return value != "" && text(value, max) && !strings.ContainsAny(value, "\r\n/\\")
}
func timestamp(value int64) bool               { return value >= 0 && value <= MaxTimestampMS }
func optionalTimestamp(value *int64) bool      { return value == nil || timestamp(*value) }
func counter(value *int64) bool                { return value == nil || *value >= 0 }
func optionalText(value *string, max int) bool { return value == nil || text(*value, max) }
func optionalID(value *string, max int) bool   { return value == nil || identifier(*value, max) }

// Validate 固定业务字段的预算与语义，传输解码另行拒绝未知及重复字段。
func (b Batch) Validate() error { return b.validate(MaxContributions) }

// ValidateSnapshot 校验收齐后的完整快照，片段仍受单批预算约束。
func ValidateSnapshot(s SessionSnapshot) error {
	if s.Chunk != nil {
		return ErrInvalid
	}
	return (Batch{Version: Version, ID: "00000000-0000-0000-0000-000000000000", Sessions: []SessionSnapshot{s}}).validate(MaxSnapshotFacts)
}
func (b Batch) validate(factBudget int) error {
	if b.Version != Version {
		return ErrVersion
	}
	if !identifier(b.ID, 36) || len(b.ID) != 36 || len(b.Sessions) > 32 || len(b.Accounts) > 32 || len(b.Bindings) > 32 || len(b.Quotas) > 1000 || len(b.Credits) > 100 || len(b.Status) > 3 {
		return ErrInvalid
	}
	if len(b.Sessions)+len(b.Accounts)+len(b.Bindings)+len(b.Quotas)+len(b.Credits)+len(b.Status) == 0 {
		return ErrInvalid
	}
	seen := make(map[string]bool)
	for _, snapshot := range b.Sessions {
		if !provider(snapshot.Provider) || !slices.Contains([]string{"", "light_index", "strict_index", "cursor_local", "cursor_dashboard", "grok_local"}, snapshot.SourceKind) || !slices.Contains([]string{"", "session", "unassigned_usage"}, snapshot.SessionKind) || !identifier(snapshot.HomeID, 128) || !identifier(snapshot.SessionID, 255) || snapshot.Revision <= 0 || !timestamp(snapshot.CollectedAtMS) || !timestamp(snapshot.HistoryStartAtMS) || !text(snapshot.Title, 512) || !identifier(snapshot.ProjectID, 255) || !text(snapshot.ProjectName, 255) || !optionalTimestamp(snapshot.CreatedAtMS) || !optionalTimestamp(snapshot.LastActiveAtMS) || len(snapshot.Contributions)+len(snapshot.Invocations) > factBudget {
			return ErrInvalid
		}
		if snapshot.Chunk != nil && !validChunk(snapshot) {
			return ErrInvalid
		}
		key := Key(snapshot.Provider, snapshot.HomeID, snapshot.SessionID)
		if seen[key] {
			return ErrInvalid
		}
		seen[key] = true
		if snapshot.Deleted && (len(snapshot.Contributions)+len(snapshot.Invocations) != 0) {
			return ErrInvalid
		}
		invocationIDs := make(map[string]bool)
		for _, i := range snapshot.Invocations {
			if len(i.ID) != 64 || !timestamp(i.ObservedAtMS) || !slices.Contains([]string{"tool", "skill"}, i.Kind) || !identifier(i.Name, 128) || !slices.Contains([]string{"unknown", "succeeded", "failed"}, i.Outcome) || !optionalTimestamp(i.DurationMS) || invocationIDs[i.ID] {
				return ErrInvalid
			}
			if _, err := hex.DecodeString(i.ID); err != nil {
				return ErrInvalid
			}
			invocationIDs[i.ID] = true
		}
		ids := make(map[string]bool)
		for _, c := range snapshot.Contributions {
			if len(c.ID) != 64 || !optionalTimestamp(c.ObservedAtMS) || !optionalText(c.Model, 128) || !counter(c.InputTokens) || !counter(c.CachedTokens) || !counter(c.CacheWriteTokens) || !counter(c.OutputTokens) || !counter(c.ReasoningTokens) || !counter(c.TotalTokens) || !counter(c.CostMicroUSD) || !counter(c.ReportedChargeMicroUSD) || !optionalText(c.PricingVersion, 128) || !slices.Contains([]string{"known", "partial", "unpriced"}, c.CostStatus) {
				return ErrInvalid
			}
			if _, err := hex.DecodeString(c.ID); err != nil || ids[c.ID] {
				return ErrInvalid
			}
			ids[c.ID] = true
			// 轻量索引的累计计数分别增量化，缓存 delta 可以大于同条 input delta。
			// 可分解性只在同模型/价格版本的范围汇总后判定。
			if !slices.Contains([]string{"", "codex_model_sum", "cursor_range_sum", "event_cost"}, c.PricingMode) {
				return ErrInvalid
			}
			if c.Rates != nil {
				for _, rate := range []*int64{c.Rates.InputMicroUSD, c.Rates.CachedMicroUSD, c.Rates.CacheWriteMicroUSD, c.Rates.OutputMicroUSD} {
					if !counter(rate) {
						return ErrInvalid
					}
				}
			}
			if c.CostStatus == "known" && (c.PricingVersion == nil || (c.CostMicroUSD == nil && c.Rates == nil)) {
				return ErrInvalid
			}
		}
		if snapshot.Throughput != nil && snapshot.Throughput.Version != 1 {
			return ErrVersion
		}
		if snapshot.Chunk == nil && !validThroughput(snapshot) {
			return ErrInvalid
		}
		if snapshot.CacheUsage != nil && snapshot.CacheUsage.Version != 1 {
			return ErrVersion
		}
		if snapshot.Chunk == nil && !validCacheUsage(snapshot) {
			return ErrInvalid
		}
	}
	accountsSeen := make(map[string]bool)
	for _, account := range b.Accounts {
		key := Key(account.Provider, account.ID)
		if accountsSeen[key] {
			return ErrInvalid
		}
		accountsSeen[key] = true
		if !provider(account.Provider) || !identifier(account.ID, 255) || !optionalText(account.Email, 255) || !optionalText(account.Plan, 128) || !timestamp(account.CollectedAtMS) {
			return ErrInvalid
		}
	}
	bindingsSeen := make(map[string]bool)
	for _, binding := range b.Bindings {
		key := Key(binding.Provider, binding.LocalScope)
		if bindingsSeen[key] {
			return ErrInvalid
		}
		bindingsSeen[key] = true
		if !provider(binding.Provider) || !identifier(binding.LocalScope, 128) || !identifier(binding.AccountID, 255) || !timestamp(binding.ConfirmedAtMS) {
			return ErrInvalid
		}
	}
	quotasSeen := make(map[string]bool)
	for _, q := range b.Quotas {
		key := Key(q.Provider, q.ID)
		if quotasSeen[key] {
			return ErrInvalid
		}
		quotasSeen[key] = true
		if !provider(q.Provider) || !identifier(q.ID, 128) || !optionalID(q.AccountID, 255) || !identifier(q.LocalScope, 128) || !identifier(q.LimitID, 128) || !identifier(q.WindowKind, 64) || !timestamp(q.ObservedAtMS) || !optionalTimestamp(q.ResetsAtMS) || !slices.Contains([]string{"accepted", "suspicious", "rejected", "unknown"}, q.Validity) || !slices.Contains([]string{"app_server", "local_jsonl", "legacy_wham", "cursor_dashboard", "grok_billing"}, q.Source) || !slices.Contains([]string{"confirmed", "pending_association", "legacy_unassigned", "linked_history"}, q.HistoryOrigin) {
			return ErrInvalid
		}
		if !optionalID(q.AssociationScope, 128) || !optionalTimestamp(q.WindowStartAtMS) {
			return ErrInvalid
		}
		if q.WindowStartAtMS != nil && q.ResetsAtMS != nil && *q.WindowStartAtMS >= *q.ResetsAtMS {
			return ErrInvalid
		}
		if q.HistoryOrigin == "linked_history" {
			if q.Provider != "codex" || q.LocalScope != "default" || q.AssociationScope == nil || *q.AssociationScope == "default" {
				return ErrInvalid
			}
		} else if q.AssociationScope != nil {
			return ErrInvalid
		}
		if q.WindowMinutes != nil && (*q.WindowMinutes <= 0 || *q.WindowMinutes > 5256000) {
			return ErrInvalid
		}
		if q.UsedPercent != nil && (math.IsNaN(*q.UsedPercent) || math.IsInf(*q.UsedPercent, 0) || *q.UsedPercent < 0 || *q.UsedPercent > 100) {
			return ErrInvalid
		}
		if q.Validity == "accepted" && (q.UsedPercent == nil || q.WindowMinutes == nil || q.ResetsAtMS == nil) {
			return ErrInvalid
		}
	}
	creditsSeen := make(map[string]bool)
	for _, c := range b.Credits {
		key := Key(c.Provider, c.ID)
		if creditsSeen[key] {
			return ErrInvalid
		}
		creditsSeen[key] = true
		if !slices.Contains([]string{"", "complete", "partial", "unavailable"}, c.DetailsStatus) || !optionalTimestamp(c.NextExpiresAtMS) || len(c.ExpirySchedule) > 100 {
			return ErrInvalid
		}
		expiryCount := int64(0)
		seenExpiry := map[int64]bool{}
		for _, e := range c.ExpirySchedule {
			if !optionalTimestamp(e.ExpiresAtMS) || e.Count <= 0 || e.Count > 1000000 {
				return ErrInvalid
			}
			key := int64(-1)
			if e.ExpiresAtMS != nil {
				key = *e.ExpiresAtMS
			}
			if seenExpiry[key] {
				return ErrInvalid
			}
			seenExpiry[key] = true
			expiryCount += e.Count
		}
		if c.DetailsStatus == "complete" && (c.Inventory == nil || expiryCount != *c.Inventory) {
			return ErrInvalid
		}
		if !provider(c.Provider) || !identifier(c.ID, 128) || !optionalID(c.AccountID, 255) || !identifier(c.LocalScope, 128) || !timestamp(c.ObservedAtMS) || !counter(c.Inventory) || !optionalTimestamp(c.NextResetAtMS) || !slices.Contains([]string{"fresh", "stale", "unknown", "unavailable", "accepted"}, c.Status) {
			return ErrInvalid
		}
	}
	statusSeen := make(map[string]bool)
	for _, s := range b.Status {
		if statusSeen[s.Provider] {
			return ErrInvalid
		}
		statusSeen[s.Provider] = true
		if !slices.Contains([]string{"", "ready", "partial", "source_budget_exceeded", "source_unavailable", "queue_full", "protocol_rejected", "storage_unavailable"}, s.SyncState) || !optionalTimestamp(s.SyncCheckedAtMS) || !slices.Contains([]string{"", "running", "completed"}, s.FullSyncState) {
			return ErrInvalid
		}
		if !provider(s.Provider) || !text(s.Version, 64) || !optionalTimestamp(s.CollectedAtMS) || !optionalTimestamp(s.CoverageStartMS) || !optionalTimestamp(s.CoverageEndMS) || s.PendingBatches < 0 || !slices.Contains([]string{"ready", "partial", "disabled", "reconnect_required", "queue_full", "source_unavailable"}, s.Status) {
			return ErrInvalid
		}
		if s.CoverageStartMS != nil && s.CoverageEndMS != nil && *s.CoverageStartMS > *s.CoverageEndMS {
			return ErrInvalid
		}
	}
	return nil
}

// Key 对长度编码的白名单标识求摘要，避免拼接碰撞，不包含路径或凭据。
func Key(parts ...string) string {
	encoded, _ := json.Marshal(parts)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// ContributionID 使用 Token 事实和相同事实的出现序号，价格/模型修订不改变身份。
// ordinal 是相同结构化事实的第几次出现，与文件 offset、设备和数据库 generation 无关。
func ContributionID(provider, sessionID string, c Contribution, ordinal int64) string {
	identity := struct {
		Provider, Session                                   string
		Observed                                            *int64
		Input, Cached, CacheWrite, Output, Reasoning, Total *int64
		Ordinal                                             int64
	}{provider, sessionID, c.ObservedAtMS, c.InputTokens, c.CachedTokens, c.CacheWriteTokens, c.OutputTokens, c.ReasoningTokens, c.TotalTokens, ordinal}
	encoded, _ := json.Marshal(identity)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func InvocationID(provider, session string, i Invocation, ordinal int64) string {
	encoded, _ := json.Marshal(struct {
		Provider, Session, Kind, Name string
		Observed, Ordinal             int64
	}{provider, session, i.Kind, i.Name, i.ObservedAtMS, ordinal})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
