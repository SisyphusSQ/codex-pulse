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
func (b Batch) Validate() error {
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
		if !provider(snapshot.Provider) || !identifier(snapshot.HomeID, 128) || !identifier(snapshot.SessionID, 255) || snapshot.Revision <= 0 || !timestamp(snapshot.CollectedAtMS) || !text(snapshot.Title, 512) || !identifier(snapshot.ProjectID, 255) || !text(snapshot.ProjectName, 255) || !optionalTimestamp(snapshot.CreatedAtMS) || !optionalTimestamp(snapshot.LastActiveAtMS) || len(snapshot.Contributions) > MaxContributions {
			return ErrInvalid
		}
		key := Key(snapshot.Provider, snapshot.HomeID, snapshot.SessionID)
		if seen[key] {
			return ErrInvalid
		}
		seen[key] = true
		if snapshot.Deleted && len(snapshot.Contributions) != 0 {
			return ErrInvalid
		}
		ids := make(map[string]bool)
		for _, c := range snapshot.Contributions {
			if len(c.ID) != 64 || !optionalTimestamp(c.ObservedAtMS) || !optionalText(c.Model, 128) || !counter(c.InputTokens) || !counter(c.CachedTokens) || !counter(c.OutputTokens) || !counter(c.ReasoningTokens) || !counter(c.TotalTokens) || !counter(c.CostMicroUSD) || !counter(c.ReportedChargeMicroUSD) || !optionalText(c.PricingVersion, 128) || !slices.Contains([]string{"known", "partial", "unpriced"}, c.CostStatus) {
				return ErrInvalid
			}
			if _, err := hex.DecodeString(c.ID); err != nil || ids[c.ID] {
				return ErrInvalid
			}
			ids[c.ID] = true
			if snapshot.Provider == "codex" && c.InputTokens != nil && c.CachedTokens != nil && *c.CachedTokens > *c.InputTokens {
				return ErrInvalid
			}
			if c.CostStatus == "known" && (c.CostMicroUSD == nil || c.PricingVersion == nil) {
				return ErrInvalid
			}
		}
	}
	for _, account := range b.Accounts {
		if !provider(account.Provider) || !identifier(account.ID, 255) || !optionalText(account.Email, 255) || !optionalText(account.Plan, 128) || !timestamp(account.CollectedAtMS) {
			return ErrInvalid
		}
	}
	for _, binding := range b.Bindings {
		if !provider(binding.Provider) || !identifier(binding.LocalScope, 128) || !identifier(binding.AccountID, 255) || !timestamp(binding.ConfirmedAtMS) {
			return ErrInvalid
		}
	}
	for _, q := range b.Quotas {
		if !provider(q.Provider) || !identifier(q.ID, 128) || !optionalID(q.AccountID, 255) || !identifier(q.LocalScope, 128) || !identifier(q.LimitID, 128) || !identifier(q.WindowKind, 64) || !timestamp(q.ObservedAtMS) || !optionalTimestamp(q.ResetsAtMS) || !slices.Contains([]string{"accepted", "suspicious", "rejected", "unknown"}, q.Validity) || !slices.Contains([]string{"app_server", "local_jsonl", "legacy_wham", "cursor_dashboard", "grok_billing"}, q.Source) || !slices.Contains([]string{"confirmed", "pending_association", "legacy_unassigned", "linked_history"}, q.HistoryOrigin) {
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
	for _, c := range b.Credits {
		if !provider(c.Provider) || !identifier(c.ID, 128) || !optionalID(c.AccountID, 255) || !identifier(c.LocalScope, 128) || !timestamp(c.ObservedAtMS) || !counter(c.Inventory) || !optionalTimestamp(c.NextResetAtMS) || !slices.Contains([]string{"fresh", "stale", "unknown", "unavailable", "accepted"}, c.Status) {
			return ErrInvalid
		}
	}
	for _, s := range b.Status {
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
		Provider, Session                       string
		Observed                                *int64
		Input, Cached, Output, Reasoning, Total *int64
		Ordinal                                 int64
	}{provider, sessionID, c.ObservedAtMS, c.InputTokens, c.CachedTokens, c.OutputTokens, c.ReasoningTokens, c.TotalTokens, ordinal}
	encoded, _ := json.Marshal(identity)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
