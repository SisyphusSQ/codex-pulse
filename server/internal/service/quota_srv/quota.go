package quota_srv

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	quota_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/quota_vo"
	quota_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/quota_repo"
	access_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
)

type Quota struct {
	repository *quota_repo.Quota
	now        func() time.Time
}

func NewQuota(repository *quota_repo.Quota) *Quota {
	return &Quota{repository: repository, now: time.Now}
}

func ownerKey(account *string, client, scope string) string {
	if account != nil {
		return *account
	}
	return reportingv1.Key("unassigned", client, scope)
}
func windowKey(row reporting_do.QuotaObservation) string {
	minutes := "unknown"
	if row.WindowMinutes != nil {
		minutes = strconv.FormatInt(*row.WindowMinutes, 10)
	}
	return reportingv1.Key(row.Provider, ownerKey(row.AccountKey, row.ClientID, row.LocalScope), row.LimitID, row.WindowKind, minutes)
}
func (s *Quota) Current(ctx context.Context, principal access_dto.Principal, q quota_dto.Query) (out quota_vo.Response, err error) {
	if err = access_srv.RequireAdmin(principal); err != nil {
		return
	}
	out = quota_vo.Response{EvaluatedAtMS: s.now().UnixMilli(), RuleVersion: "center-quota-v1/" + store.DefaultQuotaArbitrationRule().Version, Accounts: []quota_vo.Account{}, Windows: []quota_vo.Window{}, Credits: []quota_vo.Credits{}, Coverage: "observed_only"}
	err = s.repository.Snapshot(ctx, func(ctx context.Context) error {
		accounts, err := s.repository.Accounts(ctx, q)
		if err != nil {
			return err
		}
		for _, a := range accounts {
			out.Accounts = append(out.Accounts, quota_vo.Account{Key: a.ID, Provider: a.Provider, RawID: a.AccountID, Email: a.Email, Plan: a.Plan, CollectedAtMS: a.CollectedAtMS})
		}
		clients, err := s.repository.Clients(ctx)
		if err != nil {
			return err
		}
		names := map[string]string{}
		for _, c := range clients {
			names[c.ID] = c.Name
		}
		windows, err := s.repository.Windows(ctx, q)
		if err != nil {
			return err
		}
		for _, scope := range windows {
			key := scopeKey(scope)
			if q.WindowKey != "" && q.WindowKey != key {
				continue
			}
			if scope.Provider == "codex" && !store.CodexQuotaLimitActive(scope.LimitID) {
				continue
			}
			headers, err := s.repository.Headers(ctx, q, scope, out.EvaluatedAtMS, store.DefaultQuotaArbitrationRule().MaxClockSkewMS)
			if err != nil {
				return err
			}
			if len(headers) == 0 {
				continue
			}
			header, err := buildWindow(key, headers, names, out.EvaluatedAtMS)
			if err != nil {
				return err
			}
			resets := retainedResets(header)
			if q.View == "summary" {
				header = retainedWindow(header)
				header.Observations = []quota_vo.Observation{}
				header.Cycles = []quota_vo.Cycle{}
				out.Windows = append(out.Windows, header)
				continue
			}
			rows, err := s.repository.WindowObservations(ctx, q, scope, resets)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				continue
			}
			window, err := buildWindow(key, rows, names, out.EvaluatedAtMS)
			if err != nil {
				return err
			}
			window = retainedWindow(window)
			window.ObservationCount = int64(len(window.Observations))
			if q.View == "evidence" {
				page, limit := q.Page, q.Limit
				if page == 0 {
					page = 1
				}
				if limit == 0 {
					limit = 20
				}
				start := min((page-1)*limit, len(window.Observations))
				end := min(start+limit, len(window.Observations))
				if q.Direction != "asc" {
					slices.Reverse(window.Observations)
				}
				window.Observations = window.Observations[start:end]
				window.ObservationPage = page
				window.ObservationLimit = limit
				for i := range window.Cycles {
					window.Cycles[i].ObservationIDs = []string{}
				}
			}
			out.Windows = append(out.Windows, window)
		}
		slices.SortFunc(out.Windows, func(a, b quota_vo.Window) int { return strings.Compare(a.Key, b.Key) })
		credits, err := s.repository.Credits(ctx, q, out.EvaluatedAtMS+store.DefaultQuotaArbitrationRule().MaxClockSkewMS)
		if err != nil {
			return err
		}
		out.Credits, err = buildCredits(credits, out.EvaluatedAtMS)
		return err
	})
	return
}

func quotaSource(source string) store.QuotaSource {
	if source == "legacy_wham" {
		return store.QuotaSourceWham
	}
	return store.QuotaSource(source)
}
func nativeObservation(row reporting_do.QuotaObservation, scope string) store.QuotaObservation {
	o := store.QuotaObservation{ObservationID: row.ID, AccountScope: scope, Source: quotaSource(row.Source), LimitID: new(row.LimitID), WindowKind: store.QuotaWindowKind(row.WindowKind), Validity: store.QuotaValidity(row.Validity), FirstObservedAtMS: row.ObservedAtMS, LastObservedAtMS: row.ObservedAtMS, SampleCount: 1}
	if row.UsedPercent != nil {
		o.UsedPercent = *row.UsedPercent
	}
	if row.ResetsAtMS != nil {
		o.ResetsAtMS = *row.ResetsAtMS
	}
	if row.WindowMinutes != nil {
		o.WindowMinutes = *row.WindowMinutes
	}
	if row.UsedPercent == nil || row.ResetsAtMS == nil || row.WindowMinutes == nil {
		o.Validity = store.QuotaValiditySuspicious
	}
	return o
}

func buildWindow(key string, rows []reporting_do.QuotaObservation, names map[string]string, now int64) (quota_vo.Window, error) {
	first := rows[0]
	out := quota_vo.Window{Key: key, Provider: first.Provider, AccountKey: first.AccountKey, IdentityState: "unassigned", LimitID: first.LimitID, WindowKind: first.WindowKind, WindowMinutes: first.WindowMinutes, Current: quota_vo.Current{Freshness: "never_loaded", Reason: "unavailable"}, Cycles: []quota_vo.Cycle{}, Observations: []quota_vo.Observation{}, Coverage: "observed_only"}
	if first.AccountKey != nil {
		out.IdentityState = "confirmed"
	}
	normal, linked := []store.QuotaObservation{}, []store.QuotaObservation{}
	byID := map[string]reporting_do.QuotaObservation{}
	for _, row := range rows {
		byID[row.ID] = row
		if row.HistoryOrigin == "linked_history" || row.HistoryOrigin == "legacy_unassigned" {
			linked = append(linked, nativeObservation(row, "linked:"+key))
		} else {
			normal = append(normal, nativeObservation(row, key))
		}
	}
	evidence := map[string]store.QuotaArbitrationEvidence{}
	if len(normal) > 0 {
		facts, err := store.ComputeQuotaWindow(normal, now, store.DefaultQuotaArbitrationRule())
		if err != nil {
			return out, err
		}
		for _, e := range facts.Evidence {
			evidence[e.ObservationID] = e
		}
		c := facts.Current
		out.Current = quota_vo.Current{UsedPercent: c.EffectiveUsedPercent, ResetsAtMS: c.ResetsAtMS, ObservedAtMS: c.LastSuccessAtMS, Freshness: string(c.FreshnessState), Conflict: c.ConflictState == store.QuotaConflictPresent, Reason: string(c.ExplanationCode), SelectedObservationID: c.ObservationID}
		if c.EffectiveUsedPercent != nil {
			out.Current.RemainingPercent = new(100 - *c.EffectiveUsedPercent)
		}
		if c.ResetsAtMS != nil && c.WindowMinutes != nil {
			out.Current.WindowStartAtMS = new(*c.ResetsAtMS - *c.WindowMinutes*60000)
			if c.ObservationID != nil {
				selected := byID[*c.ObservationID]
				out.Current.SelectedClientID = new(selected.ClientID)
				out.Current.Source = new(selected.Source)
				if selected.WindowStartAtMS != nil {
					out.Current.WindowStartAtMS = selected.WindowStartAtMS
				}
			}
		}
		// 同一时刻的设备事实互相矛盾时明确标记；不累加、平均或猜执行来源。
		if c.LastSuccessAtMS != nil && c.EffectiveUsedPercent != nil && c.ResetsAtMS != nil {
			for _, row := range rows {
				e := evidence[row.ID]
				if row.ObservedAtMS == *c.LastSuccessAtMS && e.WindowGeneration != nil && store.QuotaResetsEquivalentForWindow(quotaSource(row.Source), *c.WindowMinutes, *e.WindowGeneration, *c.ResetsAtMS) && row.UsedPercent != nil && *row.UsedPercent != *c.EffectiveUsedPercent && eligible(e) {
					out.Current.Conflict = true
					out.Current.Reason = "source_conflict"
				}
			}
		}
	}
	if len(linked) > 0 {
		facts, err := store.ComputeQuotaWindow(linked, now, store.DefaultQuotaArbitrationRule())
		if err != nil {
			return out, err
		}
		for _, e := range facts.Evidence {
			evidence[e.ObservationID] = e
		}
	}
	if out.IdentityState == "unassigned" {
		out.Current.Freshness = "unassigned"
		out.Current.Reason = "binding_unavailable"
	}
	if out.Current.Freshness == "fresh" && !out.Current.Conflict && out.Current.ResetsAtMS != nil && *out.Current.ResetsAtMS > now {
		out.Current.ResetRemainingMS = new(*out.Current.ResetsAtMS - now)
	}
	if out.Current.ObservedAtMS != nil && out.Current.ResetsAtMS != nil && *out.Current.ResetsAtMS > *out.Current.ObservedAtMS {
		out.Current.SnapshotResetRemainingMS = new(*out.Current.ResetsAtMS - *out.Current.ObservedAtMS)
	}
	for _, row := range rows {
		e := evidence[row.ID]
		o := quota_vo.Observation{ID: row.ID, ClientID: row.ClientID, ClientName: names[row.ClientID], ObservedAtMS: row.ObservedAtMS, ReceivedAtMS: row.ReceivedAtMS, UsedPercent: row.UsedPercent, ResetsAtMS: row.ResetsAtMS, WindowStartAtMS: row.WindowStartAtMS, Source: row.Source, Validity: row.Validity, HistoryOrigin: row.HistoryOrigin, Disposition: string(e.Disposition)}
		if e.Reason != nil {
			o.Reason = new(string(*e.Reason))
		}
		if eligible(e) {
			o.CanonicalResetAtMS = e.WindowGeneration
		}
		out.Observations = append(out.Observations, o)
	}
	buildCycles(&out)
	return out, nil
}
func eligible(e store.QuotaArbitrationEvidence) bool {
	return e.Disposition == store.QuotaEvidenceSelected || e.Disposition == store.QuotaEvidenceEligible || e.Disposition == store.QuotaEvidenceSuperseded
}

// buildCycles 仅保存本次投影的聚类状态；无本地 generation 或持久周期计数。
func buildCycles(window *quota_vo.Window) {
	if window.WindowMinutes == nil {
		return
	}
	resets := []int64{}
	sources := map[int64]store.QuotaSource{}
	for _, o := range window.Observations {
		if o.CanonicalResetAtMS != nil {
			resets = append(resets, *o.CanonicalResetAtMS)
			source := quotaSource(o.Source)
			if _, found := sources[*o.CanonicalResetAtMS]; !found || source == store.QuotaSourceAppServer || source == store.QuotaSourceWham {
				sources[*o.CanonicalResetAtMS] = source
			}
		}
	}
	slices.Sort(resets)
	resets = slices.Compact(resets)
	aliases := map[int64]int64{}
	anchor, canonical := int64(-1), int64(-1)
	pending := []int64{}
	flush := func() {
		for _, reset := range pending {
			aliases[reset] = canonical
		}
		pending = nil
	}
	for _, reset := range resets {
		source := sources[reset]
		if sources[anchor] == store.QuotaSourceAppServer || sources[anchor] == store.QuotaSourceWham {
			source = sources[anchor]
		}
		if anchor < 0 || !store.QuotaResetsEquivalentForWindow(source, *window.WindowMinutes, anchor, reset) {
			flush()
			anchor = reset
		}
		canonical = reset
		pending = append(pending, reset)
	}
	flush()
	cycles := map[int64]*quota_vo.Cycle{}
	for i := range window.Observations {
		o := &window.Observations[i]
		if o.CanonicalResetAtMS == nil {
			continue
		}
		reset := aliases[*o.CanonicalResetAtMS]
		id := reportingv1.Key("center-cycle", window.Key, strconv.FormatInt(reset, 10))
		o.CanonicalResetAtMS = new(reset)
		o.CycleID = new(id)
		cycle := cycles[reset]
		if cycle == nil {
			cycle = &quota_vo.Cycle{ID: id, StartAtMS: reset - *window.WindowMinutes*60000, ResetsAtMS: reset, ObservationIDs: []string{}}
			cycles[reset] = cycle
		}
		cycle.ObservationIDs = append(cycle.ObservationIDs, o.ID)
		cycle.LinkedHistory = cycle.LinkedHistory || o.HistoryOrigin == "linked_history" || o.HistoryOrigin == "legacy_unassigned"
	}
	keys := []int64{}
	for reset := range cycles {
		keys = append(keys, reset)
	}
	slices.Sort(keys)
	for _, reset := range keys {
		window.Cycles = append(window.Cycles, *cycles[reset])
	}
}
