package quota_srv

import (
	"encoding/json/v2"
	"errors"
	"slices"
	"strconv"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	quota_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/quota_vo"
)

func buildCredits(rows []reporting_do.ResetCredits, now int64) ([]quota_vo.Credits, error) {
	latest := map[string]reporting_do.ResetCredits{}
	last := map[string]reporting_do.ResetCredits{}
	samples := map[string]*int64{}
	conflicts := map[string]bool{}
	rule := store.DefaultQuotaArbitrationRule()
	for _, row := range rows {
		key := reportingv1.Key(row.Provider, ownerKey(row.AccountKey, row.ClientID, row.LocalScope))
		sampleKey := reportingv1.Key(key, strconv.FormatInt(row.ObservedAtMS, 10))
		previous, seen := samples[sampleKey]
		if seen && ((previous == nil) != (row.Inventory == nil) || previous != nil && row.Inventory != nil && *previous != *row.Inventory) {
			conflicts[sampleKey] = true
		}
		samples[sampleKey] = row.Inventory
		attempt, exists := last[key]
		if !exists || row.ObservedAtMS > attempt.ObservedAtMS || row.ObservedAtMS == attempt.ObservedAtMS && row.ID > attempt.ID {
			last[key] = row
		}
		if row.Inventory == nil || (row.Status != "accepted" && row.Status != "fresh" && row.Status != "stale") || row.ObservedAtMS > now+rule.MaxClockSkewMS {
			continue
		}
		old, exists := latest[key]
		if !exists || row.ObservedAtMS > old.ObservedAtMS || row.ObservedAtMS == old.ObservedAtMS && row.ID > old.ID {
			latest[key] = row
		}
	}
	keys := []string{}
	for key := range last {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	out := []quota_vo.Credits{}
	for _, key := range keys {
		row, known := latest[key]
		if !known {
			row = last[key]
		}
		view := quota_vo.Credits{Key: key, Provider: row.Provider, AccountKey: row.AccountKey, ClientID: row.ClientID, ObservedAtMS: row.ObservedAtMS, ObservedInventory: row.Inventory, DetailsStatus: row.DetailsStatus, Freshness: "fresh", NextResetAtMS: row.NextResetAtMS, ExpirySchedule: []quota_vo.CreditExpiry{}}
		if err := json.Unmarshal([]byte(row.ExpirySchedule), &view.ExpirySchedule); err != nil {
			return nil, err
		}
		if view.ExpirySchedule == nil {
			view.ExpirySchedule = []quota_vo.CreditExpiry{}
		}
		if !known {
			view.Freshness = "unknown"
		}
		if last[key].ObservedAtMS > row.ObservedAtMS {
			view.Freshness = "stale"
		}
		if row.Inventory == nil || row.Status == "unknown" || row.Status == "unavailable" {
			view.Freshness = "unknown"
		}
		if row.Status == "stale" || now-row.ObservedAtMS > rule.FreshForMS {
			view.Freshness = "stale"
		}
		if row.ObservedAtMS > now+rule.MaxClockSkewMS {
			view.Freshness = "suspicious"
		}
		if row.AccountKey == nil {
			view.Freshness = "unassigned"
		}
		view.Conflict = conflicts[reportingv1.Key(key, strconv.FormatInt(row.ObservedAtMS, 10))]
		if row.DetailsStatus == "complete" && row.Inventory != nil {
			available, total := int64(0), int64(0)
			for _, expiry := range view.ExpirySchedule {
				if expiry.Count <= 0 {
					return nil, errors.New("stored credit schedule is invalid")
				}
				total += expiry.Count
				if expiry.ExpiresAtMS == nil || *expiry.ExpiresAtMS > now {
					available += expiry.Count
					if expiry.ExpiresAtMS != nil && (view.NextExpiresAtMS == nil || *expiry.ExpiresAtMS < *view.NextExpiresAtMS) {
						view.NextExpiresAtMS = expiry.ExpiresAtMS
					}
				}
			}
			if total != *row.Inventory {
				return nil, errors.New("stored credit inventory does not reconcile")
			}
			if view.Freshness == "fresh" && !view.Conflict {
				view.AvailableInventory = &available
			}
		} else {
			view.NextExpiresAtMS = row.NextExpiresAtMS
			if view.NextExpiresAtMS != nil && *view.NextExpiresAtMS <= now {
				view.NextExpiresAtMS = nil
			}
		}
		out = append(out, view)
	}
	return out, nil
}
