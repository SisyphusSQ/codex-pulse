package quota_srv

import (
	"context"
	"encoding/json/v2"
	"slices"
	"strings"
	"time"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	quota_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/quota_vo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
)

func scopeKey(w quota_dto.WindowScope) string {
	return windowKey(reporting_do.QuotaObservation{Provider: w.Provider, AccountKey: w.AccountKey, ClientID: w.ClientID, LocalScope: w.LocalScope, LimitID: w.LimitID, WindowKind: w.WindowKind, WindowMinutes: w.WindowMinutes})
}

func retainedCycles(w quota_vo.Window) map[string]bool {
	keep := map[string]bool{}
	if w.Current.SelectedObservationID != nil {
		for _, o := range w.Observations {
			if o.ID == *w.Current.SelectedObservationID && o.CycleID != nil {
				keep[*o.CycleID] = true
			}
		}
	}
	for i := len(w.Cycles) - 1; i >= 0 && len(keep) < 4; i-- {
		keep[w.Cycles[i].ID] = true
	}
	return keep
}

func retainedResets(w quota_vo.Window) []int64 {
	keep := retainedCycles(w)
	resets := []int64{}
	for _, o := range w.Observations {
		if o.CycleID != nil && keep[*o.CycleID] && o.ResetsAtMS != nil {
			resets = append(resets, *o.ResetsAtMS)
		}
	}
	slices.Sort(resets)
	return slices.Compact(resets)
}

func retainedWindow(w quota_vo.Window) quota_vo.Window {
	keep := retainedCycles(w)
	w.Cycles = slices.DeleteFunc(w.Cycles, func(c quota_vo.Cycle) bool { return !keep[c.ID] })
	w.Observations = slices.DeleteFunc(w.Observations, func(o quota_vo.Observation) bool { return o.CycleID != nil && !keep[*o.CycleID] })
	return w
}

func (s *Quota) Accounts(ctx context.Context, p access_dto.Principal, q quota_dto.Query) (out quota_vo.Response, err error) {
	if err = access_srv.RequireAdmin(p); err != nil {
		return
	}
	out = quota_vo.Response{EvaluatedAtMS: s.now().UnixMilli(), Accounts: []quota_vo.Account{}, Windows: []quota_vo.Window{}, Credits: []quota_vo.Credits{}, Coverage: "observed_only"}
	rows, err := s.repository.Accounts(ctx, q)
	if err != nil {
		return out, err
	}
	for _, a := range rows {
		out.Accounts = append(out.Accounts, quota_vo.Account{Key: a.ID, Provider: a.Provider, RawID: a.AccountID, Email: a.Email, Plan: a.Plan, CollectedAtMS: a.CollectedAtMS})
	}
	return out, nil
}

// Compact 保留四个已观测周期及最后有效值，仅压缩已结束周期中的同状态区间。
func (s *Quota) Compact(ctx context.Context) (retired int, err error) {
	windows, err := s.repository.Windows(ctx, quota_dto.Query{RawHistory: true})
	if err != nil {
		return 0, err
	}
	for _, scope := range windows {
		rows, err := s.repository.AllWindowObservations(ctx, scope)
		if err != nil {
			return retired, err
		}
		if len(rows) == 0 {
			continue
		}
		w, err := buildWindow(scopeKey(scope), rows, map[string]string{}, s.now().UnixMilli())
		if err != nil {
			return retired, err
		}
		cycles := retainedCycles(w)
		currentCycle := ""
		for _, o := range w.Observations {
			if w.Current.SelectedObservationID != nil && o.ID == *w.Current.SelectedObservationID && o.CycleID != nil {
				currentCycle = *o.CycleID
			}
		}
		byID := map[string]quota_vo.Observation{}
		for _, o := range w.Observations {
			byID[o.ID] = o
		}
		remove := map[string]bool{}
		groups := map[string][]reporting_do.QuotaObservation{}
		for _, row := range rows {
			o := byID[row.ID]
			if o.CycleID != nil && !cycles[*o.CycleID] {
				remove[row.ID] = true
				continue
			}
			if o.CycleID == nil || *o.CycleID == currentCycle || o.CanonicalResetAtMS == nil || *o.CanonicalResetAtMS > s.now().UnixMilli() || row.Validity != "accepted" {
				continue
			}
			groups[strings.Join([]string{row.ClientID, row.LocalScope, row.Source, row.HistoryOrigin, *o.CycleID}, "\n")] = append(groups[strings.Join([]string{row.ClientID, row.LocalScope, row.Source, row.HistoryOrigin, *o.CycleID}, "\n")], row)
		}
		for _, group := range groups {
			start := 0
			previous := ""
			flush := func(end int) {
				for i := start + 1; i < end-1; i++ {
					remove[group[i].ID] = true
				}
			}
			for i, row := range group {
				row.ID = ""
				row.ObservationID = ""
				row.ObservedAtMS = 0
				row.ReceivedAtMS = 0
				body, e := json.Marshal(row)
				if e != nil {
					return retired, e
				}
				state := string(body)
				if i > 0 && state != previous {
					flush(i)
					start = i
				}
				previous = state
			}
			flush(len(group))
		}
		if w.Current.SelectedObservationID != nil {
			delete(remove, *w.Current.SelectedObservationID)
		}
		batch := []reporting_do.RetiredObservation{}
		for _, row := range rows {
			if remove[row.ID] {
				digest, e := reporting_do.ObservationDigest(row)
				if e != nil {
					return retired, e
				}
				batch = append(batch, reporting_do.RetiredObservation{ID: row.ID, Digest: digest, RetiredAtMS: time.Now().UnixMilli()})
			}
		}
		for start := 0; start < len(batch); start += 250 {
			part := batch[start:min(start+250, len(batch))]
			if err = s.repository.Transaction(ctx, func(ctx context.Context) error { return s.repository.Retire(ctx, part) }); err != nil {
				return retired, err
			}
			retired += len(part)
		}
	}
	return retired, nil
}

// displayPoints 只返回真实采样，保留区间端点与极值；预测仍用完整当前周期证据。
func displayPoints(points []quota_vo.PacePoint) []quota_vo.PacePoint {
	if len(points) <= 512 {
		return points
	}
	out := make([]quota_vo.PacePoint, 0, 512)
	for start := 0; start < len(points); {
		end := min(start+max(1, (len(points)+127)/128), len(points))
		low, high := start, start
		for i := start; i < end; i++ {
			if points[i].UsedPercent < points[low].UsedPercent {
				low = i
			}
			if points[i].UsedPercent > points[high].UsedPercent {
				high = i
			}
		}
		indices := []int{start, low, high, end - 1}
		slices.Sort(indices)
		for _, i := range slices.Compact(indices) {
			out = append(out, points[i])
		}
		start = end
	}
	return out
}
