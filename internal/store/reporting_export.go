package store

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
)

var ErrReportingSource = errors.New("reporting source is not ready")
var ErrReportingBudget = errors.New("reporting session exceeds fact budget")

// ReportingPage 只包含白名单事实。游标是当前来源的 Session ID，删除扫描只在
// Authority=true 时进行；未就绪的索引不能作为删除历史的证据。
type ReportingPage struct {
	Sessions  []reportingv1.SessionSnapshot
	Next      string
	Authority bool
}
type ReportingSource struct {
	HomeID, Path, DeviceID, Partition string
	Inode                             int64
	StartAtMS                         int64
}

// ReportingPage 在一致的只读事务内导出当前活动代，不读取源文件或认证文件。
func (r *Repository) ReportingPage(ctx context.Context, provider string, source ReportingSource, after string) (page ReportingPage, err error) {
	if r == nil || r.database == nil {
		return page, ErrInvalidRepository
	}
	err = r.database.ViewSnapshot(ctx, func(ctx context.Context, db *gorm.DB) error {
		switch provider {
		case "codex":
			return exportReportingCodex(db, source, after, &page)
		case "cursor", "grok":
			return exportReportingAgent(db, provider, source, after, &page)
		default:
			return ErrReportingSource
		}
	})
	return
}

func assignReportingIDs(s *reportingv1.SessionSnapshot) {
	ordinals := make(map[string]int64)
	for i := range s.Contributions {
		c := &s.Contributions[i]
		base := reportingv1.ContributionID(s.Provider, s.SessionID, *c, 0)
		c.ID = reportingv1.ContributionID(s.Provider, s.SessionID, *c, ordinals[base])
		ordinals[base]++
	}
}

func exportReportingCodex(db *gorm.DB, source ReportingSource, after string, page *ReportingPage) error {
	var state struct {
		HomePath, HomeDeviceID string
		HomeInode              int64
		MetadataReadyAtMS      *int64
		TokenScanState         string
		UpdatedAtMS            int64
	}
	if err := db.Table("light_index_state").Where("state_id = 1").Take(&state).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return exportReportingStrict(db, source, after, page)
		}
		return err
	}
	if state.HomePath != source.Path || state.HomeDeviceID != source.DeviceID || state.HomeInode != source.Inode || state.MetadataReadyAtMS == nil {
		return ErrReportingSource
	}
	page.Authority = state.TokenScanState == "complete"
	var sessions []struct {
		SessionID                string
		ThreadName               *string
		CWD                      string `gorm:"column:cwd"`
		CreatedAtMS, UpdatedAtMS int64
		ActiveTokenGeneration    int64
		RecencyAtMS              *int64
		ScanState                string
	}
	if err := db.Table("light_sessions").Where("session_id > ?", after).Order("session_id").Limit(16).Find(&sessions).Error; err != nil {
		return err
	}
	catalogs, err := loadLightPricingCatalogs(db, reportingv1.MaxTimestampMS)
	if err != nil {
		return err
	}
	for _, row := range sessions {
		s := reportingv1.SessionSnapshot{Provider: "codex", HomeID: source.HomeID, SessionID: row.SessionID, SessionKind: "session", SourceKind: "light_index", CreatedAtMS: &row.CreatedAtMS, LastActiveAtMS: row.RecencyAtMS, ProjectID: reportingv1.Key("project", row.CWD), ProjectName: filepath.Base(row.CWD), CollectedAtMS: state.UpdatedAtMS, Complete: row.ScanState == "complete", Contributions: []reportingv1.Contribution{}}
		if row.CWD == "" {
			s.ProjectID = "unknown"
			s.ProjectName = "未归属项目"
		} else if s.ProjectName == string(filepath.Separator) {
			s.ProjectName = "根目录"
		}
		if row.ThreadName != nil {
			s.Title = *row.ThreadName
		}
		var scan struct {
			Complete                                                      bool
			InputTokens, CachedInputTokens, OutputTokens, ReasoningTokens int64
			UpdatedAtMS                                                   int64
		}
		s.CacheUsage = &reportingv1.CacheUsageCapsule{Version: 1, Basis: "lifetime_cached_input", Reason: "rollup_missing"}
		if row.ActiveTokenGeneration > 0 {
			if err := db.Table("light_token_scans").Select("complete,input_tokens,cached_input_tokens,output_tokens,reasoning_tokens,updated_at_ms").Where("session_id = ? AND generation = ? AND state = 'active'", row.SessionID, row.ActiveTokenGeneration).Take(&scan).Error; err != nil {
				return err
			}
			s.Complete = s.Complete && scan.Complete
			s.CacheUsage.Reason = ""
			s.CacheUsage.InputTokens, s.CacheUsage.CachedInputTokens = new(scan.InputTokens), new(scan.CachedInputTokens)
			if scan.InputTokens < 0 || scan.CachedInputTokens < 0 {
				s.CacheUsage.InputTokens, s.CacheUsage.CachedInputTokens, s.CacheUsage.Reason = nil, nil, "unavailable"
			}
			s.CollectedAtMS = max(s.CollectedAtMS, scan.UpdatedAtMS)
			var timed []struct {
				ObservedAtMS                                                  int64
				ModelKey                                                      *string
				ModelSource                                                   string
				InputTokens, CachedInputTokens, OutputTokens, ReasoningTokens int64
			}
			if err := db.Table("light_token_timed").Select("observed_at_ms,model_key,model_source,input_tokens,cached_input_tokens,output_tokens,reasoning_tokens").Where("session_id = ? AND generation = ?", row.SessionID, row.ActiveTokenGeneration).Order("source_offset").Limit(reportingv1.MaxContributions + 1).Find(&timed).Error; err != nil {
				return err
			}
			if len(timed) > reportingv1.MaxContributions {
				return ErrReportingBudget
			}
			var sums [4]int64
			for _, t := range timed {
				for i, v := range []int64{t.InputTokens, t.CachedInputTokens, t.OutputTokens, t.ReasoningTokens} {
					sums[i], err = checkedAdd(sums[i], v)
					if err != nil {
						return err
					}
				}
				total, err := reportingTotal(t.InputTokens, t.OutputTokens, t.ReasoningTokens)
				if err != nil {
					return err
				}
				c := reportingv1.Contribution{ObservedAtMS: &t.ObservedAtMS, InputTokens: &t.InputTokens, CachedTokens: &t.CachedInputTokens, OutputTokens: &t.OutputTokens, ReasoningTokens: &t.ReasoningTokens, TotalTokens: &total, CostStatus: "unpriced", PricingMode: "codex_model_sum"}
				dimension := lightModelDimension(t.ModelKey, t.ModelSource)
				c.Model = dimension.identity
				catalog := effectiveLightPricingCatalog(catalogs, t.ObservedAtMS)
				if catalog != nil {
					c.PricingVersion = &catalog.version.PricingVersion
					if dimension.identity != nil {
						if model, ok := catalog.models[*dimension.identity]; ok {
							c.Rates = &reportingv1.Rates{InputMicroUSD: model.InputMicrosPerMillion, CachedMicroUSD: model.CachedInputMicrosPerMillion, OutputMicroUSD: model.OutputMicrosPerMillion}
							c.CostStatus = "known"
						}
					}
				}
				// 初次全历史身份序号必须先生成，再应用补传起点，避免重复事实键漂移。
				s.Contributions = append(s.Contributions, c)
			}
			remaining := []int64{scan.InputTokens - sums[0], scan.CachedInputTokens - sums[1], scan.OutputTokens - sums[2], scan.ReasoningTokens - sums[3]}
			nonzero := false
			for _, v := range remaining {
				if v < 0 {
					return ErrReportingSource
				}
				nonzero = nonzero || v > 0
			}
			if nonzero {
				total, err := reportingTotal(remaining[0], remaining[2], remaining[3])
				if err != nil {
					return err
				}
				s.Contributions = append(s.Contributions, reportingv1.Contribution{InputTokens: &remaining[0], CachedTokens: &remaining[1], OutputTokens: &remaining[2], ReasoningTokens: &remaining[3], TotalTokens: &total, CostStatus: "unpriced"})
				s.Complete = false
			}
		} else {
			s.Complete = false
		}
		if row.ActiveTokenGeneration > 0 {
			if err := exportReportingInvocations(db, "codex", row.SessionID, row.ActiveTokenGeneration, source.StartAtMS, &s); err != nil {
				return err
			}
		}
		assignReportingIDs(&s)
		if err := exportReportingThroughput(db, source, &s); err != nil {
			return err
		}
		s.Contributions = reportingRange(s.Contributions, source.StartAtMS)
		if source.StartAtMS > 0 {
			s.CacheUsage.InputTokens, s.CacheUsage.CachedInputTokens, s.CacheUsage.Reason = nil, nil, "history_filtered"
		}
		if len(s.Contributions) > reportingv1.MaxContributions {
			return ErrReportingBudget
		}
		page.Sessions = append(page.Sessions, s)
		page.Next = row.SessionID
	}
	return nil
}
func reportingTotal(values ...int64) (total int64, err error) {
	for _, v := range values {
		total, err = checkedAdd(total, v)
		if err != nil {
			return
		}
	}
	return
}
func reportingRange(facts []reportingv1.Contribution, start int64) []reportingv1.Contribution {
	if start == 0 {
		return facts
	}
	out := make([]reportingv1.Contribution, 0, len(facts))
	for _, c := range facts {
		if c.ObservedAtMS != nil && *c.ObservedAtMS >= start {
			out = append(out, c)
		}
	}
	return out
}

func exportReportingStrict(db *gorm.DB, source ReportingSource, after string, page *ReportingPage) error {
	// 旧深索引没有一致 Home fence，不把旧库猜归当前 Home；待轻量 metadata 确认后导出。
	var count int64
	if err := db.Table("sessions").Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrReportingSource
	}
	return nil
}

func exportReportingAgent(db *gorm.DB, provider string, source ReportingSource, after string, page *ReportingPage) error {
	var snapshot struct{ CollectedAtMS int64 }
	if err := db.Table("agent_provider_snapshots").Where("provider = ?", provider).Take(&snapshot).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	page.Authority = true
	var adverse, totalSources int64
	if err := db.Table("agent_provider_sources").Where("provider = ?", provider).Count(&totalSources).Error; err != nil {
		return err
	}
	if err := db.Table("agent_provider_sources").Where("provider = ? AND (state <> 'available' OR coverage_state <> 'exact')", provider).Count(&adverse).Error; err != nil {
		return err
	}
	page.Authority = totalSources > 0 && adverse == 0
	var dashboard int64
	if provider == "cursor" {
		var snap struct{ WindowStartMS, CollectedAtMS int64 }
		result := db.Table("cursor_dashboard_snapshots").Take(&snap)
		if result.Error == nil {
			dashboard = 1
			snapshot.CollectedAtMS = snap.CollectedAtMS
			if source.Partition != "dashboard-"+strconv.FormatInt(snap.WindowStartMS, 10) {
				return ErrReportingSource
			}
		} else if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return result.Error
		} else if source.Partition != "local" {
			return ErrReportingSource
		}
	}
	table := provider + "_sessions" // provider 在入口枚举中固定。
	selectSQL := "SELECT external_session_id AS id FROM " + table
	if dashboard > 0 {
		selectSQL += " UNION SELECT COALESCE(external_session_id,'unassigned-' || event_fingerprint) AS id FROM cursor_dashboard_usage_events"
	}
	var ids []string
	if err := db.Raw("SELECT id FROM ("+selectSQL+") WHERE id > ? ORDER BY id LIMIT 16", after).Scan(&ids).Error; err != nil {
		return err
	}
	for _, id := range ids {
		var meta struct {
			DisplayTitle, ProjectKey, ProjectDisplayName, CoverageState string
			CreatedAtMS, LastActivityAtMS                               int64
			LineageConflict                                             bool
		}
		result := db.Table(table).Select("display_title,project_key,project_display_name,coverage_state,created_at_ms,last_activity_at_ms,lineage_conflict").Where("external_session_id = ?", id).Take(&meta)
		if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return result.Error
		}
		s := reportingv1.SessionSnapshot{Provider: provider, HomeID: source.HomeID, SessionID: id, SessionKind: "session", SourceKind: provider + "_local", Title: meta.DisplayTitle, ProjectID: meta.ProjectKey, ProjectName: meta.ProjectDisplayName, CollectedAtMS: snapshot.CollectedAtMS, Complete: meta.CoverageState == "exact" && !meta.LineageConflict, Contributions: []reportingv1.Contribution{}}
		if result.Error == nil {
			s.CreatedAtMS = &meta.CreatedAtMS
			s.LastActiveAtMS = &meta.LastActivityAtMS
		}
		if s.ProjectID == "" {
			s.ProjectID = "unknown"
			s.ProjectName = "未归属项目"
		}
		if dashboard > 0 {
			s.SourceKind = "cursor_dashboard"
			if err := exportReportingCursorDashboard(db, id, &s); err != nil {
				return err
			}
		} else {
			if err := exportReportingAgentUsage(db, provider, id, &s); err != nil {
				return err
			}
		}
		if len(s.Contributions) > reportingv1.MaxContributions {
			return ErrReportingBudget
		}
		if err := exportReportingInvocations(db, provider, id, 0, source.StartAtMS, &s); err != nil {
			return err
		}
		assignReportingIDs(&s)
		s.Contributions = reportingRange(s.Contributions, source.StartAtMS)
		page.Sessions = append(page.Sessions, s)
		page.Next = id
	}
	return nil
}

func exportReportingAgentUsage(db *gorm.DB, provider, id string, s *reportingv1.SessionSnapshot) error {
	var rows []struct {
		OccurredAtMS                                                                                   int64
		ModelKey                                                                                       *string
		InputTokens, OutputTokens, CachedReadTokens, CacheCreationTokens, ReasoningTokens, TotalTokens int64
		ReportedCostMicros                                                                             *int64
	}
	query := db.Table(provider+"_usage_events").Where("external_session_id = ?", id).Order("occurred_at_ms,event_id").Limit(reportingv1.MaxContributions + 1)
	if err := query.Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) > reportingv1.MaxContributions {
		return ErrReportingBudget
	}
	for _, r := range rows {
		c := reportingv1.Contribution{ObservedAtMS: &r.OccurredAtMS, Model: r.ModelKey, InputTokens: &r.InputTokens, OutputTokens: &r.OutputTokens, CostStatus: "unpriced", PricingMode: "event_cost"}
		if provider == "grok" {
			c.CachedTokens = &r.CachedReadTokens
			c.CacheWriteTokens = &r.CacheCreationTokens
			c.ReasoningTokens = &r.ReasoningTokens
			c.TotalTokens = &r.TotalTokens
			c.ReportedChargeMicroUSD = r.ReportedCostMicros
			if r.ModelKey != nil {
				if cost, ok := pricing.EstimateGrokUsageCostAt(*r.ModelKey, r.OccurredAtMS, r.InputTokens, r.CachedReadTokens, r.CacheCreationTokens, r.OutputTokens); ok {
					c.CostMicroUSD = &cost
					version := pricing.GrokPricingVersion
					c.PricingVersion = &version
					c.CostStatus = "known"
				}
			}
		} else {
			total, err := reportingTotal(r.InputTokens, r.OutputTokens)
			if err != nil {
				return err
			}
			c.TotalTokens = &total
			s.Complete = false
		}
		s.Contributions = append(s.Contributions, c)
	}
	return nil
}

func exportReportingCursorDashboard(db *gorm.DB, id string, s *reportingv1.SessionSnapshot) error {
	var rows []cursorDashboardUsageModel
	query := db.Where("external_session_id = ?", id)
	if len(id) > 11 && id[:11] == "unassigned-" {
		query = db.Where("external_session_id IS NULL AND event_fingerprint = ?", id[11:])
		s.SessionKind = "unassigned_usage"
		s.Title = "未关联会话的用量"
		s.Complete = false
	}
	if err := query.Order("occurred_at_ms,event_fingerprint").Limit(reportingv1.MaxContributions + 1).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) > reportingv1.MaxContributions {
		return ErrReportingBudget
	}
	for _, r := range rows {
		tokens := []int64{r.InputTokens, r.CacheReadTokens, r.CacheWriteTokens, r.OutputTokens, r.ReportedChargeMicros}
		for i, v := range tokens {
			var err error
			tokens[i], err = checkedMultiply(v, r.OccurrenceCount)
			if err != nil {
				return err
			}
		}
		total, err := reportingTotal(tokens[0], tokens[1], tokens[2], tokens[3])
		if err != nil {
			return err
		}
		c := reportingv1.Contribution{ObservedAtMS: &r.OccurredAtMS, Model: r.ModelKey, InputTokens: &tokens[0], CachedTokens: &tokens[1], CacheWriteTokens: &tokens[2], OutputTokens: &tokens[3], TotalTokens: &total, ReportedChargeMicroUSD: &tokens[4], CostStatus: "unpriced", PricingMode: "cursor_range_sum"}
		if !r.TokenBased {
			c.InputTokens = nil
			c.CachedTokens = nil
			c.CacheWriteTokens = nil
			c.OutputTokens = nil
			c.TotalTokens = nil
		}
		if r.TokenBased && r.ModelKey != nil {
			if rate, ok := pricing.CursorRateForUsage(*r.ModelKey, r.OccurredAtMS, r.InputTokens, r.CacheWriteTokens, r.CacheReadTokens); ok && (r.CacheWriteTokens == 0 || rate.CacheWriteMicros != nil) {
				c.Rates = &reportingv1.Rates{InputMicroUSD: &rate.InputMicros, CachedMicroUSD: &rate.CacheReadMicros, CacheWriteMicroUSD: rate.CacheWriteMicros, OutputMicroUSD: &rate.OutputMicros}
				version := pricing.CursorPricingVersion
				c.PricingVersion = &version
				c.CostStatus = "known"
			}
		}
		s.Contributions = append(s.Contributions, c)
	}
	return nil
}
func checkedMultiply(a, b int64) (int64, error) {
	if a < 0 || b < 0 || b > 0 && a > (1<<63-1)/b {
		return 0, ErrReportingBudget
	}
	return a * b, nil
}

// ReportingPartition separates Cursor billing cycles because its local dashboard table
// is a replacement of the active cycle. An old cycle remains historical evidence.
func (r *Repository) ReportingPartition(ctx context.Context, provider string) (key string, err error) {
	key = "local"
	if provider != "cursor" {
		return key, nil
	}
	err = r.database.ViewSnapshot(ctx, func(_ context.Context, db *gorm.DB) error {
		var snapshot struct{ WindowStartMS int64 }
		result := db.Table("cursor_dashboard_snapshots").Take(&snapshot)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		if result.Error != nil {
			return result.Error
		}
		key = "dashboard-" + strconv.FormatInt(snapshot.WindowStartMS, 10)
		return nil
	})
	return
}

func exportReportingInvocations(db *gorm.DB, provider, sessionID string, generation, start int64, s *reportingv1.SessionSnapshot) error {
	var rows []struct {
		ObservedAtMS        int64
		Kind, Name, Outcome string
		DurationMS          *int64
	}
	var query *gorm.DB
	if provider == "codex" {
		query = db.Table("light_invocation_events").Select("observed_at_ms,kind,name,outcome,duration_ms").Where("session_id = ? AND generation = ?", sessionID, generation).Order("source_offset,event_ordinal")
	} else {
		query = db.Table(provider+"_tool_events").Select("occurred_at_ms AS observed_at_ms,'tool' AS kind,tool_name AS name,outcome").Where("external_session_id = ?", sessionID).Order("occurred_at_ms,event_id")
	}
	if err := query.Limit(reportingv1.MaxContributions + 1).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) > reportingv1.MaxContributions {
		return ErrReportingBudget
	}
	ordinal := make(map[string]int64)
	for _, r := range rows {
		i := reportingv1.Invocation{ObservedAtMS: r.ObservedAtMS, Kind: r.Kind, Name: r.Name, Outcome: r.Outcome, DurationMS: r.DurationMS}
		key := reportingv1.InvocationID(provider, sessionID, i, 0)
		i.ID = reportingv1.InvocationID(provider, sessionID, i, ordinal[key])
		ordinal[key]++
		if i.ObservedAtMS >= start {
			s.Invocations = append(s.Invocations, i)
		}
	}
	if len(s.Contributions)+len(s.Invocations) > reportingv1.MaxContributions {
		return ErrReportingBudget
	}
	return nil
}

// ReportingStatus reads coverage independently of online/pairing state.
func (r *Repository) ReportingStatus(ctx context.Context, provider string, source ReportingSource) (status reportingv1.DeviceStatus, err error) {
	if provider != "codex" && provider != "cursor" && provider != "grok" {
		return status, ErrReportingSource
	}
	status = reportingv1.DeviceStatus{Provider: provider, Status: "source_unavailable"}
	err = r.database.ViewSnapshot(ctx, func(_ context.Context, db *gorm.DB) error {
		var coverage struct{ Start, End *int64 }
		if provider == "codex" {
			var state struct {
				HomePath, HomeDeviceID string
				HomeInode              int64
				UpdatedAtMS            int64
				TokenScanState         string
				MetadataReadyAtMS      *int64
			}
			result := db.Table("light_index_state").Take(&state)
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return nil
			}
			if result.Error != nil {
				return result.Error
			}
			if state.HomePath != source.Path || state.HomeDeviceID != source.DeviceID || state.HomeInode != source.Inode || state.MetadataReadyAtMS == nil {
				return nil
			}
			status.CollectedAtMS = &state.UpdatedAtMS
			status.Status = "partial"
			if state.TokenScanState == "complete" {
				status.Status = "ready"
			}
			if err := db.Table("light_token_timed AS t").Select("MIN(t.observed_at_ms) AS start,MAX(t.observed_at_ms) AS end").Joins("JOIN light_sessions s ON s.session_id=t.session_id AND s.active_token_generation=t.generation").Where("t.observed_at_ms >= ?", source.StartAtMS).Scan(&coverage).Error; err != nil {
				return err
			}
		} else {
			var snapshot struct{ CollectedAtMS int64 }
			result := db.Table("agent_provider_snapshots").Where("provider = ?", provider).Take(&snapshot)
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return nil
			}
			if result.Error != nil {
				return result.Error
			}
			status.CollectedAtMS = &snapshot.CollectedAtMS
			status.Status = "partial"
			var known, partial int64
			if err := db.Table("agent_provider_sources").Where("provider = ?", provider).Count(&known).Error; err != nil {
				return err
			}
			if err := db.Table("agent_provider_sources").Where("provider = ? AND (state <> 'available' OR coverage_state <> 'exact')", provider).Count(&partial).Error; err != nil {
				return err
			}
			if known > 0 && partial == 0 {
				status.Status = "ready"
			}
			table := provider + "_usage_events"
			if provider == "cursor" && source.Partition != "local" {
				table = "cursor_dashboard_usage_events"
				var dashboard struct{ CollectedAtMS int64 }
				if err := db.Table("cursor_dashboard_snapshots").Take(&dashboard).Error; err != nil {
					return err
				}
				status.CollectedAtMS = &dashboard.CollectedAtMS
			}
			if err := db.Table(table).Select("MIN(occurred_at_ms) AS start,MAX(occurred_at_ms) AS end").Where("occurred_at_ms >= ?", source.StartAtMS).Scan(&coverage).Error; err != nil {
				return err
			}
		}
		status.CoverageStartMS = coverage.Start
		status.CoverageEndMS = coverage.End
		return nil
	})
	return
}
