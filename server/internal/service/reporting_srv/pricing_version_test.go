package reporting_srv

import (
	"testing"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/reporting_dto"
)

func TestTerraMixedSessionCatalogVersionUpgrade(t *testing.T) {
	for _, mode := range []string{"codex_model_sum", "event_cost"} {
		t.Run(mode, func(t *testing.T) {
			old := reportingv1.SessionSnapshot{Provider: "codex", Complete: true, Revision: 1}
			sol := reportingv1.Contribution{ID: "sol", Model: new("gpt-6.1-sol"), CostStatus: "known", PricingMode: mode, PricingVersion: new("openai-api-2026-09-29")}
			terra := reportingv1.Contribution{ID: "terra", Model: new("gpt-6.1-terra"), CostStatus: "unpriced", PricingMode: mode}
			if mode == "codex_model_sum" {
				sol.Rates = &reportingv1.Rates{InputMicroUSD: new(int64(2_000_000)), CachedMicroUSD: new(int64(100_000)), OutputMicroUSD: new(int64(10_000_000))}
			} else {
				sol.CostMicroUSD = new(int64(10_000))
			}
			old.Contributions = []reportingv1.Contribution{sol, terra}
			upgraded := old
			upgraded.Revision = 2
			upgraded.Contributions = append([]reportingv1.Contribution(nil), old.Contributions...)
			upgraded.Contributions[0].PricingVersion = new("openai-api-2026-10-26")
			upgraded.Contributions[1].PricingVersion = new("openai-api-2026-10-26")
			upgraded.Contributions[1].CostStatus = "known"
			if mode == "codex_model_sum" {
				upgraded.Contributions[1].Rates = &reportingv1.Rates{InputMicroUSD: new(int64(1_000_000)), CachedMicroUSD: new(int64(50_000)), OutputMicroUSD: new(int64(5_000_000))}
			} else {
				upgraded.Contributions[1].CostMicroUSD = new(int64(5_000))
			}
			previous := reporting_dto.SourceSnapshot{ID: "old-device", Snapshot: old}
			incoming := reporting_dto.SourceSnapshot{ID: "upgraded-device", Snapshot: upgraded}
			decision := DecideSnapshot([]reporting_dto.SourceSnapshot{previous, incoming}, &previous)
			if decision.Conflict || decision.Source.ID != incoming.ID {
				t.Fatal("unchanged Sol rate blocked Terra enrichment", decision)
			}
			if *previous.Snapshot.Contributions[0].PricingVersion != "openai-api-2026-09-29" || *decision.Source.Snapshot.Contributions[0].PricingVersion != "openai-api-2026-10-26" {
				t.Fatal("original pricing evidence changed")
			}
			// A real price/model difference must retain the conflict; no lowest/highest-price arbitration.
			for _, change := range []string{"price", "model"} {
				different := upgraded
				different.Contributions = append([]reportingv1.Contribution(nil), upgraded.Contributions...)
				if change == "model" {
					different.Contributions[0].Model = new("gpt-6-sol")
				} else if mode == "codex_model_sum" {
					rates := *sol.Rates
					rates.OutputMicroUSD = new(int64(11_000_000))
					different.Contributions[0].Rates = &rates
				} else {
					different.Contributions[0].CostMicroUSD = new(int64(11_000))
				}
				incoming.Snapshot = different
				if !DecideSnapshot([]reporting_dto.SourceSnapshot{previous, incoming}, &previous).Conflict {
					t.Fatal("real pricing conflict hidden", change)
				}
			}
		})
	}
}
