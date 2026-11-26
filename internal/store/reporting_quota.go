package store

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strconv"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
)

// ReportingQuotaPage 不含认证响应、request/source file/credit ID 或本地代际。
type ReportingQuotaPage struct {
	Batch       reportingv1.Batch
	Next        string
	Done        bool
	LinkedScope *string
}
type reportingQuotaCursor struct {
	Stage      string
	At         int64
	Key        string
	Generation int64
	Scope      string `json:",omitempty"`
	Window     string `json:",omitempty"`
}

func quotaReportingCursor(after string) (out reportingQuotaCursor, err error) {
	out.Stage = "quota"
	if after == "" {
		return
	}
	if len(after) > 2048 {
		return out, ErrReportingSource
	}
	body, err := base64.RawURLEncoding.DecodeString(after)
	if err != nil {
		return out, ErrReportingSource
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		return out, ErrReportingSource
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return out, ErrReportingSource
	}
	if out.Stage != "quota" && out.Stage != "credits" || out.At < 0 || out.Generation < 0 {
		return out, ErrReportingSource
	}
	return
}
func encodeQuotaCursor(cursor reportingQuotaCursor) string {
	body, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(body)
}
func (r *Repository) ReportingQuotaPartition(ctx context.Context) (string, error) {
	var revision int64
	err := r.database.ViewSnapshot(ctx, func(_ context.Context, db *gorm.DB) error {
		return db.Table("codex_quota_history_association_generations").Select("COALESCE(MAX(revision),0)").Scan(&revision).Error
	})
	return strconv.FormatInt(revision, 10), err
}
func (r *Repository) ReportingQuotaPage(ctx context.Context, provider, after string, start int64) (ReportingQuotaPage, error) {
	return r.reportingQuotaPage(ctx, provider, after, start, false)
}

// ReportingCurrentQuotaPage 独立提取每个 scope/limit/window 的最新观测，避免历史游标阻塞。
func (r *Repository) ReportingCurrentQuotaPage(ctx context.Context, provider string, start int64) (ReportingQuotaPage, error) {
	return r.reportingQuotaPage(ctx, provider, "", start, true)
}
func (r *Repository) reportingQuotaPage(ctx context.Context, provider, after string, start int64, current bool) (page ReportingQuotaPage, err error) {
	cursor, err := quotaReportingCursor(after)
	if err != nil {
		return page, err
	}
	err = r.database.ViewSnapshot(ctx, func(ctx context.Context, db *gorm.DB) error {
		switch provider {
		case "codex":
			association, found, err := loadLegacyQuotaHistoryAssociation(ctx, db)
			if err != nil {
				return err
			}
			if found {
				page.LinkedScope = new(association.AccountScope)
			}
			if cursor.Stage == "quota" {
				var rows []quotaObservationModel
				query := db.Where("last_observed_at_ms >= ?", start)
				if current {
					query = query.Where("observation_id IN (SELECT observation_id FROM (SELECT observation_id, ROW_NUMBER() OVER (PARTITION BY account_scope, limit_id, window_kind, validity, source ORDER BY last_observed_at_ms DESC, observation_id DESC) AS rank FROM quota_observations WHERE last_observed_at_ms >= ?) WHERE rank = 1)", start)
				}
				if after != "" {
					query = query.Where("last_observed_at_ms > ? OR (last_observed_at_ms = ? AND observation_id > ?)", cursor.At, cursor.At, cursor.Key)
				}
				if err := query.Order("last_observed_at_ms,observation_id").Limit(100).Find(&rows).Error; err != nil {
					return err
				}
				for _, row := range rows {
					observation, err := quotaObservationFromModel(row)
					if err != nil {
						return err
					}
					for _, at := range quotaEndpoints(observation.FirstObservedAtMS, observation.LastObservedAtMS, start) {
						origin := "pending_association"
						if observation.AccountScope == QuotaAccountScopeDefault {
							origin = "legacy_unassigned"
						}
						source := string(observation.Source)
						if observation.Source == QuotaSourceWham {
							source = "legacy_wham"
						}
						limit := valueOrUnknown(observation.LimitID)
						page.Batch.Quotas = append(page.Batch.Quotas, reportingv1.QuotaObservation{Provider: "codex", ID: reportingv1.Key("quota", observation.ObservationID, strconv.FormatInt(at, 10)), LocalScope: observation.AccountScope, LimitID: limit, WindowKind: string(observation.WindowKind), WindowMinutes: new(observation.WindowMinutes), ResetsAtMS: new(observation.ResetsAtMS), ObservedAtMS: at, UsedPercent: new(observation.UsedPercent), Validity: string(observation.Validity), Source: source, HistoryOrigin: origin})
					}
					cursor = reportingQuotaCursor{Stage: "quota", At: row.LastObservedAtMS, Key: row.ObservationID}
				}
				if len(rows) > 0 && !current {
					page.Next = encodeQuotaCursor(cursor)
					return nil
				}
				cursor = reportingQuotaCursor{Stage: "credits"}
			}
			var rows []resetCreditsSnapshotModel
			query := db.Where("observed_at_ms >= ?", start)
			if current {
				query = query.Where("snapshot_id IN (SELECT snapshot_id FROM (SELECT snapshot_id, ROW_NUMBER() OVER (PARTITION BY account_scope ORDER BY observed_at_ms DESC, snapshot_id DESC) AS rank FROM reset_credit_snapshots WHERE observed_at_ms >= ?) WHERE rank=1)", start)
			}
			if cursor.Key != "" {
				query = query.Where("observed_at_ms > ? OR (observed_at_ms = ? AND snapshot_id > ?)", cursor.At, cursor.At, cursor.Key)
			}
			if err := query.Order("observed_at_ms,snapshot_id").Limit(50).Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				var details []resetCreditModel
				if err := db.Where("snapshot_id = ?", row.SnapshotID).Order("credit_id_hash").Limit(101).Find(&details).Error; err != nil {
					return err
				}
				if len(details) > 100 {
					return ErrReportingBudget
				}
				snapshot, err := resetCreditsSnapshotFromModels(row, details)
				if err != nil {
					return err
				}
				if err := validateResetCreditsSnapshot(snapshot); err != nil {
					return err
				}
				attempt, found, err := sourceAttemptByID(ctx, db, row.RequestID)
				if err != nil {
					return err
				}
				if !found || attempt.Outcome != SourceAttemptSucceeded || snapshot.ObservedAtMS < attempt.StartedAtMS || snapshot.ObservedAtMS > attempt.FinishedAtMS || attempt.SourceInstanceID != ResetCreditsSourceInstanceAppServer(snapshot.AccountScope) && attempt.SourceInstanceID != ResetCreditsSourceInstanceWhamDefault {
					return ErrReportingSource
				}
				summary := ResetCreditsSummary{}
				populateResetCreditsSummary(&summary, snapshot, row.ObservedAtMS)
				fact := reportingv1.ResetCredits{Provider: "codex", ID: reportingv1.Key("credits", row.SnapshotID), LocalScope: row.AccountScope, ObservedAtMS: row.ObservedAtMS, Inventory: summary.AvailableCount, Status: "accepted", DetailsStatus: string(snapshot.DetailsStatus), NextExpiresAtMS: summary.NextExpiresAtMS}
				counts := map[int64]int64{}
				for _, c := range snapshot.Credits {
					if c.Status != ResetCreditAvailable || c.ExpiresAtMS != nil && *c.ExpiresAtMS <= row.ObservedAtMS {
						continue
					}
					expiry := int64(-1)
					if c.ExpiresAtMS != nil {
						expiry = *c.ExpiresAtMS
					}
					counts[expiry]++
				}
				keys := make([]int64, 0, len(counts))
				for expiry := range counts {
					keys = append(keys, expiry)
				}
				sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
				for _, expiry := range keys {
					item := reportingv1.CreditExpiry{Count: counts[expiry]}
					if expiry >= 0 {
						item.ExpiresAtMS = new(expiry)
					}
					fact.ExpirySchedule = append(fact.ExpirySchedule, item)
				}
				page.Batch.Credits = append(page.Batch.Credits, fact)
				cursor = reportingQuotaCursor{Stage: "credits", At: row.ObservedAtMS, Key: row.SnapshotID}
			}
			if len(rows) > 0 && !current {
				page.Next = encodeQuotaCursor(cursor)
				return nil
			}
			page.Done = true
			return nil
		case "cursor", "grok":
			if cursor.Stage != "quota" {
				return ErrReportingSource
			}
			table := "cursor_dashboard_quota_observations"
			if provider == "grok" {
				table = "grok_billing_quota_observations"
			}
			// 两个 Provider 的持久观测表共用明确列，不依赖各自当前账期快照。
			var rows []cursorDashboardQuotaObservationModel
			query := db.Table(table).Where("observed_at_ms >= ?", start)
			if current {
				query = query.Where("(limit_id, observed_at_ms, generation) IN (SELECT limit_id, observed_at_ms, generation FROM (SELECT limit_id, observed_at_ms, generation, ROW_NUMBER() OVER (PARTITION BY limit_id ORDER BY observed_at_ms DESC, generation DESC) AS rank FROM "+table+" WHERE observed_at_ms >= ?) WHERE rank = 1)", start)
			}
			if after != "" {
				query = query.Where("observed_at_ms > ? OR (observed_at_ms = ? AND generation > ?) OR (observed_at_ms = ? AND generation = ? AND limit_id > ?)", cursor.At, cursor.At, cursor.Generation, cursor.At, cursor.Generation, cursor.Key)
			}
			if err := query.Order("observed_at_ms,generation,limit_id").Limit(100).Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) == 0 {
				page.Done = true
				if provider == "grok" {
					// 旧库只有当前账期时也导出真实快照；已有历史采用相同事实键去重。
					rows, err = grokReportingCurrent(db, start)
					if err != nil {
						return err
					}
				}
			}
			for _, row := range rows {
				kind := "primary"
				source := "cursor_dashboard"
				if row.LimitID == "cursor.other_models" {
					kind = "secondary"
				}
				if row.LimitID == "cursor.grok_bot" {
					kind = "additional:grok_bot"
				}
				if provider == "grok" {
					source = "grok_billing"
				}
				minutes := (row.CycleEndAtMS - row.CycleStartAtMS) / 60000
				if minutes <= 0 {
					return ErrReportingSource
				}
				// generation 仅参与本机 keyset，不发往网络，也不作为跨机器周期身份。
				id := reportingv1.Key("quota", provider, row.LimitID, strconv.FormatInt(row.ObservedAtMS, 10), strconv.FormatInt(row.CycleStartAtMS, 10), strconv.FormatInt(row.CycleEndAtMS, 10), strconv.FormatFloat(row.UsedPercent, 'f', -1, 64))
				page.Batch.Quotas = append(page.Batch.Quotas, reportingv1.QuotaObservation{Provider: provider, ID: id, LocalScope: "default", LimitID: row.LimitID, WindowKind: kind, WindowMinutes: &minutes, WindowStartAtMS: new(row.CycleStartAtMS), ResetsAtMS: new(row.CycleEndAtMS), ObservedAtMS: row.ObservedAtMS, UsedPercent: new(row.UsedPercent), Validity: "accepted", Source: source, HistoryOrigin: "pending_association"})
				cursor = reportingQuotaCursor{Stage: "quota", At: row.ObservedAtMS, Generation: row.Generation, Key: row.LimitID}
			}
			if current {
				page.Done = true
			}
			if len(rows) > 0 && !page.Done {
				page.Next = encodeQuotaCursor(cursor)
			}
			return nil
		case "dsh":
			page.Done = true
			return nil
		case "agy":
			if cursor.Stage != "quota" {
				return ErrReportingSource
			}
			return exportReportingAGYQuota(db, cursor, after, start, current, &page)
		default:
			return ErrReportingSource
		}
	})
	return
}

// ReportingAGYAccounts 返回本地已持久化的非敏感 AGY 账号摘要（64位 hex account_scope 与标准化套餐，不含邮箱原文）。
func (r *Repository) ReportingAGYAccounts(ctx context.Context) ([]reportingv1.Account, error) {
	if r == nil || r.database == nil {
		return nil, ErrInvalidRepository
	}
	var out []reportingv1.Account
	err := r.database.ViewSnapshot(ctx, func(_ context.Context, db *gorm.DB) error {
		type accEntry struct {
			plan        *string
			collectedAt int64
		}
		entries := map[string]*accEntry{}
		var snaps []agyQuotaSnapshotModel
		if err := db.Order("account_scope, updated_at_ms DESC").Find(&snaps).Error; err != nil {
			return err
		}
		for _, snap := range snaps {
			if !validHexDigest(snap.AccountScope) {
				continue
			}
			e := entries[snap.AccountScope]
			if e == nil {
				e = &accEntry{plan: snap.PlanType, collectedAt: snap.DataAsOfMS}
				entries[snap.AccountScope] = e
			}
			if snap.DataAsOfMS > e.collectedAt {
				e.collectedAt = snap.DataAsOfMS
				if snap.PlanType != nil && *snap.PlanType != "" {
					e.plan = snap.PlanType
				}
			} else if e.plan == nil && snap.PlanType != nil && *snap.PlanType != "" {
				e.plan = snap.PlanType
			}
		}
		var obsScopes []struct {
			AccountScope string
			MaxObserved  int64
		}
		if err := db.Table("agy_quota_observations").Select("account_scope, MAX(observed_at_ms) AS max_observed").Group("account_scope").Scan(&obsScopes).Error; err != nil {
			return err
		}
		for _, row := range obsScopes {
			if !validHexDigest(row.AccountScope) {
				continue
			}
			if _, exists := entries[row.AccountScope]; !exists {
				entries[row.AccountScope] = &accEntry{collectedAt: row.MaxObserved}
			}
		}
		scopes := make([]string, 0, len(entries))
		for scope := range entries {
			scopes = append(scopes, scope)
		}
		sort.Strings(scopes)
		out = make([]reportingv1.Account, 0, len(scopes))
		for _, scope := range scopes {
			e := entries[scope]
			out = append(out, reportingv1.Account{
				Provider:      "agy",
				ID:            scope,
				Email:         nil,
				Plan:          e.plan,
				CollectedAtMS: e.collectedAt,
			})
		}
		return nil
	})
	return out, err
}

func exportReportingAGYQuota(db *gorm.DB, cursor reportingQuotaCursor, after string, start int64, current bool, page *ReportingQuotaPage) error {
	var snapshots []agyQuotaSnapshotModel
	if err := db.Order("account_scope, limit_id, window_kind").Find(&snapshots).Error; err != nil {
		return err
	}
	windowMinutes := make(map[string]int64, len(snapshots))
	for _, snap := range snapshots {
		windowMinutes[snap.AccountScope+"\x00"+snap.LimitID+"\x00"+snap.WindowKind] = snap.WindowMinutes
	}
	var rows []agyQuotaObservationModel
	query := db.Where("observed_at_ms >= ?", start)
	if current {
		query = query.Where(`(account_scope, limit_id, window_kind, observed_at_ms) IN (
			SELECT account_scope, limit_id, window_kind, observed_at_ms FROM (
				SELECT account_scope, limit_id, window_kind, observed_at_ms,
					ROW_NUMBER() OVER (PARTITION BY account_scope, limit_id, window_kind ORDER BY observed_at_ms DESC) AS rank
				FROM agy_quota_observations WHERE observed_at_ms >= ?
			) WHERE rank = 1
		)`, start)
	}
	if after != "" {
		query = query.Where(
			"observed_at_ms > ? OR (observed_at_ms = ? AND account_scope > ?) OR (observed_at_ms = ? AND account_scope = ? AND limit_id > ?) OR (observed_at_ms = ? AND account_scope = ? AND limit_id = ? AND window_kind > ?)",
			cursor.At,
			cursor.At, cursor.Scope,
			cursor.At, cursor.Scope, cursor.Key,
			cursor.At, cursor.Scope, cursor.Key, cursor.Window,
		)
	}
	if err := query.Order("observed_at_ms, account_scope, limit_id, window_kind").Limit(100).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		page.Done = true
		if after == "" {
			for _, snap := range snapshots {
				if snap.DataAsOfMS < start || snap.UsedPercent == nil {
					continue
				}
				rows = append(rows, agyQuotaObservationModel{
					AccountScope: snap.AccountScope,
					LimitID:      snap.LimitID,
					WindowKind:   snap.WindowKind,
					ObservedAtMS: snap.DataAsOfMS,
					UsedPercent:  *snap.UsedPercent,
					ResetsAtMS:   snap.ResetsAtMS,
				})
			}
		}
	}
	for _, row := range rows {
		winMins := windowMinutes[row.AccountScope+"\x00"+row.LimitID+"\x00"+row.WindowKind]
		if winMins <= 0 {
			if row.WindowKind == "secondary" {
				winMins = 10080
			} else {
				winMins = 300
			}
		}
		resetKey := "none"
		if row.ResetsAtMS != nil {
			resetKey = strconv.FormatInt(*row.ResetsAtMS, 10)
		}
		id := reportingv1.Key(
			"quota", "agy",
			row.AccountScope, row.LimitID, row.WindowKind,
			strconv.FormatInt(row.ObservedAtMS, 10),
			resetKey,
			strconv.FormatInt(row.UsedPercent, 10),
		)
		obs := reportingv1.QuotaObservation{
			Provider:      "agy",
			ID:            id,
			LocalScope:    row.AccountScope,
			LimitID:       row.LimitID,
			WindowKind:    row.WindowKind,
			WindowMinutes: new(winMins),
			ResetsAtMS:    row.ResetsAtMS,
			ObservedAtMS:  row.ObservedAtMS,
			UsedPercent:   new(float64(row.UsedPercent)),
			Validity:      "unknown",
			Source:        "agy_ls_rpc",
			HistoryOrigin: "pending_association",
		}
		if row.ResetsAtMS != nil {
			obs.Validity = "accepted"
			if winStart := *row.ResetsAtMS - winMins*60000; winStart >= 0 && winStart < *row.ResetsAtMS {
				obs.WindowStartAtMS = new(winStart)
			}
		}
		page.Batch.Quotas = append(page.Batch.Quotas, obs)
		cursor = reportingQuotaCursor{
			Stage:  "quota",
			At:     row.ObservedAtMS,
			Scope:  row.AccountScope,
			Key:    row.LimitID,
			Window: row.WindowKind,
		}
	}
	if current {
		page.Done = true
	}
	if len(rows) > 0 && !page.Done {
		page.Next = encodeQuotaCursor(cursor)
	}
	return nil
}

func grokReportingCurrent(db *gorm.DB, start int64) ([]cursorDashboardQuotaObservationModel, error) {
	var current grokBillingSnapshotModel
	err := db.Where("provider = ?", "grok").Take(&current).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if current.CollectedAtMS < start {
		return nil, nil
	}
	row := cursorDashboardQuotaObservationModel{Generation: current.Generation, LimitID: "grok.included_credits", UsedPercent: current.UsedPercent, CycleStartAtMS: current.PeriodStartMS, CycleEndAtMS: current.PeriodEndMS, ObservedAtMS: current.CollectedAtMS}
	rows := []cursorDashboardQuotaObservationModel{row}
	if current.OnDemandUsed != nil && current.OnDemandCap != nil && *current.OnDemandCap > 0 {
		percent := *current.OnDemandUsed / *current.OnDemandCap * 100
		if percent >= 0 && percent <= 100 {
			row.LimitID, row.UsedPercent = "grok.on_demand", percent
			rows = append(rows, row)
		}
	}
	return rows, nil
}
func quotaEndpoints(first, last, start int64) []int64 {
	out := []int64{}
	if first >= start {
		out = append(out, first)
	}
	if last >= start && last != first {
		out = append(out, last)
	}
	return out
}
func valueOrUnknown(value *string) string {
	if value == nil || *value == "" {
		return "unknown"
	}
	return *value
}
