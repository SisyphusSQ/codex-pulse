// Package creditbalance defines official billing-credit observations, independently of earned resets.
package creditbalance

import (
	"encoding/json"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/SisyphusSQ/codex-pulse/internal/jsonshape"
)

const DayMS int64 = 86_400_000

var decimal = regexp.MustCompile(`^(0|[1-9][0-9]{0,29})(\.[0-9]{1,18})?$`)

// Amount accepts bounded exact decimals; it never rounds a provider balance.
func Amount(value string) (string, bool) {
	if !decimal.MatchString(value) {
		return "", false
	}
	if strings.Contains(value, ".") {
		value = strings.TrimRight(strings.TrimRight(value, "0"), ".")
	}
	return value, true
}

type Snapshot struct {
	LimitID      string  `json:"limitId"`
	State        string  `json:"state"`
	Balance      *string `json:"balance,omitempty"`
	HasCredits   *bool   `json:"hasCredits,omitempty"`
	Unlimited    *bool   `json:"unlimited,omitempty"`
	ObservedAtMS int64   `json:"observedAtMs"`
}

type Point struct {
	ObservedAtMS int64   `json:"observedAtMs"`
	Balance      *string `json:"balance,omitempty"`
	Unlimited    bool    `json:"unlimited"`
}

type View struct {
	Snapshot
	Freshness        string  `json:"freshness"`
	CheckedAtMS      int64   `json:"checkedAtMs"`
	ConsumptionState string  `json:"consumptionState"`
	History          []Point `json:"history"`
}

// Decode keeps malformed optional credit fields from invalidating quota windows.
func Decode(raw json.RawMessage, limitID string) Snapshot {
	out := Snapshot{LimitID: limitID, State: "unavailable"}
	if len(raw) == 0 || string(raw) == "null" {
		return out
	}
	var wire struct {
		Balance    *string `json:"balance"`
		HasCredits *bool   `json:"hasCredits"`
		Unlimited  *bool   `json:"unlimited"`
	}
	if jsonshape.ValidateDocument(raw) != nil || json.Unmarshal(raw, &wire) != nil || wire.HasCredits == nil || wire.Unlimited == nil {
		out.State = "invalid"
		return out
	}
	if wire.Balance != nil {
		amount, ok := Amount(*wire.Balance)
		if !ok {
			out.State = "invalid"
			return out
		}
		wire.Balance = &amount
	}
	out.State = "known"
	out.Balance = wire.Balance
	out.HasCredits = wire.HasCredits
	out.Unlimited = wire.Unlimited
	return out
}

func (s Snapshot) Valid() bool {
	if len(s.LimitID) == 0 || len(s.LimitID) > 512 || !utf8.ValidString(s.LimitID) || strings.TrimSpace(s.LimitID) != s.LimitID || s.ObservedAtMS < 0 || s.ObservedAtMS > 9_007_199_254_740_991 {
		return false
	}
	switch s.State {
	case "known":
		if s.HasCredits == nil || s.Unlimited == nil {
			return false
		}
		if s.Balance != nil {
			amount, ok := Amount(*s.Balance)
			return ok && amount == *s.Balance
		}
		return true
	case "unavailable", "invalid":
		return s.Balance == nil && s.HasCredits == nil && s.Unlimited == nil
	default:
		return false
	}
}

// Views selects the latest successful balance per bucket; daily points are UTC observations, not debits.
func Views(samples []Snapshot, now int64, lastAttemptAtMS int64, sourceFailed bool) []View {
	groups := map[string][]Snapshot{}
	for _, s := range samples {
		if s.Valid() && s.ObservedAtMS <= now {
			groups[s.LimitID] = append(groups[s.LimitID], s)
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]View, 0, len(keys))
	for _, key := range keys {
		rows := groups[key]
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].ObservedAtMS < rows[j].ObservedAtMS })
		latest := rows[len(rows)-1]
		selected := latest
		found := false
		daily := map[int64]Snapshot{}
		for _, row := range rows {
			if row.State != "known" {
				continue
			}
			selected = row
			found = true
			if row.ObservedAtMS >= now-30*DayMS {
				daily[row.ObservedAtMS/DayMS] = row
			}
		}
		view := View{Snapshot: selected, CheckedAtMS: max(lastAttemptAtMS, latest.ObservedAtMS), Freshness: "unknown", ConsumptionState: "source_unavailable", History: []Point{}}
		if found {
			view.Freshness = "fresh"
			if (sourceFailed && lastAttemptAtMS > selected.ObservedAtMS) || latest.State != "known" || now-selected.ObservedAtMS > 10*60_000 {
				view.Freshness = "stale"
			}
			for _, row := range rows {
				if row.State == "known" && row.ObservedAtMS == selected.ObservedAtMS && !reflect.DeepEqual(row, selected) {
					view.Freshness = "conflict"
					view.Balance = nil
					view.Unlimited = nil
					break
				}
			}
		}
		days := make([]int64, 0, len(daily))
		for day := range daily {
			days = append(days, day)
		}
		sort.Slice(days, func(i, j int) bool { return days[i] < days[j] })
		for _, day := range days {
			row := daily[day]
			point := Point{ObservedAtMS: row.ObservedAtMS, Balance: row.Balance, Unlimited: *row.Unlimited}
			for _, other := range rows {
				if other.State == "known" && other.ObservedAtMS == row.ObservedAtMS && !reflect.DeepEqual(other, row) {
					point.Balance = nil
					point.Unlimited = false
					break
				}
			}
			view.History = append(view.History, point)
		}
		out = append(out, view)
	}
	return out
}
