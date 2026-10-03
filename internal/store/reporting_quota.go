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
func (r *Repository) ReportingQuotaPage(ctx context.Context, provider, after string, start int64) (page ReportingQuotaPage, err error) {
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
				if len(rows) > 0 {
					page.Next = encodeQuotaCursor(cursor)
					return nil
				}
				cursor = reportingQuotaCursor{Stage: "credits"}
			}
			var rows []resetCreditsSnapshotModel
			query := db.Where("observed_at_ms >= ?", start)
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
			if len(rows) > 0 {
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
			if len(rows) > 0 && !page.Done {
				page.Next = encodeQuotaCursor(cursor)
			}
			return nil
		default:
			return ErrReportingSource
		}
	})
	return
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
