package reportingv1

import (
	"encoding/json"
	"errors"
	"testing"
)

func validBatch() Batch {
	contribution := Contribution{ObservedAtMS: new(int64(1000)), InputTokens: new(int64(0)), TotalTokens: new(int64(0)), CostStatus: "unpriced"}
	contribution.ID = ContributionID("codex", "session", contribution, 0)
	return Batch{Version: Version, ID: "00000000-0000-0000-0000-000000000001", Sessions: []SessionSnapshot{{Provider: "codex", HomeID: "home", SessionID: "session", Revision: 1, CollectedAtMS: 2000, ProjectID: "project", Contributions: []Contribution{contribution}}}}
}

func TestStableContributionIdentitySeparatesRepeatedEventsAndIgnoresPriceMetadata(t *testing.T) {
	c := validBatch().Sessions[0].Contributions[0]
	before := ContributionID("codex", "session", c, 0)
	c.Model = new("gpt-model")
	c.CostMicroUSD = new(int64(10))
	c.PricingVersion = new("catalog-v2")
	if ContributionID("codex", "session", c, 0) != before {
		t.Fatal("price/model revision changed identity")
	}
	if ContributionID("codex", "session", c, 1) == before {
		t.Fatal("identical repeated events collided")
	}
	if ContributionID("cursor", "session", c, 0) == before {
		t.Fatal("provider identity collided")
	}
	if Key("ab", "c") == Key("a", "bc") {
		t.Fatal("concatenation ambiguity")
	}
}

func TestProtocolPreservesUnknownZeroAndLargeInteger(t *testing.T) {
	batch := validBatch()
	batch.Sessions[0].Contributions[0].TotalTokens = new(int64(9007199254740993))
	encoded, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Batch
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	c := decoded.Sessions[0].Contributions[0]
	if c.OutputTokens != nil || c.InputTokens == nil || *c.InputTokens != 0 || c.TotalTokens == nil || *c.TotalTokens != 9007199254740993 {
		t.Fatal("unknown, zero or integer precision lost")
	}
}

func TestRejectsInvalidFactsVersionsAndDuplicateContributions(t *testing.T) {
	if err := validBatch().Validate(); err != nil {
		t.Fatal(err)
	}
	batch := validBatch()
	batch.Version++
	if err := batch.Validate(); !errors.Is(err, ErrVersion) {
		t.Fatal("unknown protocol accepted")
	}
	batch = validBatch()
	batch.Sessions[0].Contributions = append(batch.Sessions[0].Contributions, batch.Sessions[0].Contributions[0])
	if err := batch.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatal("duplicate contribution accepted")
	}
	batch = validBatch()
	batch.Sessions[0].ProjectID = "/private/project"
	if err := batch.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatal("raw project path accepted")
	}
	batch = validBatch()
	batch.Sessions[0].Contributions[0].InputTokens = new(int64(-1))
	if err := batch.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatal("negative token accepted")
	}
}

func TestReportingMetadataDuplicatesAreRejected(t *testing.T) {
	batch := validBatch()
	batch.Accounts = []Account{{Provider: "codex", ID: "same", CollectedAtMS: 1}, {Provider: "codex", ID: "same", CollectedAtMS: 2}}
	if !errors.Is(batch.Validate(), ErrInvalid) {
		t.Fatal("duplicate accounts accepted")
	}
	batch = validBatch()
	batch.Status = []DeviceStatus{{Provider: "codex", Status: "ready"}, {Provider: "codex", Status: "partial"}}
	if !errors.Is(batch.Validate(), ErrInvalid) {
		t.Fatal("duplicate provider statuses accepted")
	}
}

func TestQuotaAssociationAndCreditsExpiryContract(t *testing.T) {
	q := QuotaObservation{Provider: "codex", ID: "legacy", LocalScope: "default", AssociationScope: new("scope-a"), AccountID: new("raw-a"), LimitID: "codex", WindowKind: "primary", WindowMinutes: new(int64(300)), ResetsAtMS: new(int64(18000001)), ObservedAtMS: 1000, UsedPercent: new(0.0), Validity: "accepted", Source: "legacy_wham", HistoryOrigin: "linked_history"}
	c := ResetCredits{Provider: "codex", ID: "credits", LocalScope: "scope-a", ObservedAtMS: 1000, Inventory: new(int64(2)), Status: "accepted", DetailsStatus: "complete", NextExpiresAtMS: new(int64(2000)), ExpirySchedule: []CreditExpiry{{Count: 1}, {ExpiresAtMS: new(int64(2000)), Count: 1}}}
	b := Batch{Version: Version, ID: "00000000-0000-0000-0000-000000000001", Quotas: []QuotaObservation{q}, Credits: []ResetCredits{c}}
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	b.Quotas[0].AssociationScope = nil
	if !errors.Is(b.Validate(), ErrInvalid) {
		t.Fatal("linked history without proof accepted")
	}
	b.Quotas[0] = q
	b.Quotas[0].HistoryOrigin = "confirmed"
	if !errors.Is(b.Validate(), ErrInvalid) {
		t.Fatal("ordinary fact carried legacy association")
	}
	b.Quotas[0] = q
	b.Credits[0].ExpirySchedule = append(b.Credits[0].ExpirySchedule, CreditExpiry{Count: 1})
	if !errors.Is(b.Validate(), ErrInvalid) {
		t.Fatal("duplicate expiry bucket accepted")
	}
	b.Credits[0] = c
	b.Credits[0].Inventory = new(int64(3))
	if !errors.Is(b.Validate(), ErrInvalid) {
		t.Fatal("complete inventory does not reconcile")
	}
}

func TestAGYProtocolAllowsLocalSessionMultiPoolQuotaAndFiveProviderStatuses(t *testing.T) {
	scope := Key("agy-account", "synthetic-account-digest")
	c := Contribution{
		ObservedAtMS:    new(int64(1784000000000)),
		Model:           new("gemini-4-carbon"),
		InputTokens:     new(int64(1500)),
		CachedTokens:    new(int64(500)),
		OutputTokens:    new(int64(250)),
		ReasoningTokens: new(int64(100)),
		TotalTokens:     new(int64(1750)),
		CostMicroUSD:    new(int64(4550)),
		PricingVersion:  new("agy-api-2026-11-25:phase1"),
		CostStatus:      "known",
		PricingMode:     "event_cost",
	}
	c.ID = ContributionID("agy", "agy-cascade-1", c, 0)
	b := Batch{
		Version: Version,
		ID:      "00000000-0000-0000-0000-000000000002",
		Sessions: []SessionSnapshot{{
			Provider:      "agy",
			HomeID:        "home-agy",
			SessionID:     "agy-cascade-1",
			Revision:      1,
			CollectedAtMS: 1784000005000,
			Title:         "AGY 会话",
			ProjectID:     Key("agy", "workspace"),
			ProjectName:   "workspace",
			SourceKind:    "agy_local",
			SessionKind:   "session",
			SessionRole:   "swarm_worker",
			Complete:      true,
			Contributions: []Contribution{c},
			CacheUsage: &CacheUsageCapsule{
				Version:           1,
				Basis:             "lifetime_cached_input",
				InputTokens:       new(int64(1500)),
				CachedInputTokens: new(int64(500)),
			},
			Throughput: &ThroughputCapsule{
				Version: 1,
				Basis:   "closed_turn_lifetime_output",
				Measures: ThroughputMeasures{
					OutputTokens:     new(int64(250)),
					ActiveDurationMS: new(int64(2000)),
					IncludedTurns:    1,
					CoverageKnown:    true,
					Status:           "complete",
					DurationSource:   "step_metadata",
				},
				TurnsTotal: 1,
				RecentTurns: []ThroughputTurn{{
					Key:         Key("agy", "agy-cascade-1", "turn", "0"),
					StartedAtMS: new(int64(1783999998000)),
					EndedAtMS:   new(int64(1784000000000)),
					Measures: ThroughputMeasures{
						OutputTokens:     new(int64(250)),
						ActiveDurationMS: new(int64(2000)),
						IncludedTurns:    1,
						CoverageKnown:    true,
						Status:           "complete",
						DurationSource:   "step_metadata",
					},
				}},
			},
		}},
		Accounts: []Account{{
			Provider:      "agy",
			ID:            scope,
			Plan:          new("Google AI Ultra 20×"),
			CollectedAtMS: 1784000005000,
		}},
		Bindings: []AccountBinding{{
			Provider:      "agy",
			LocalScope:    "public-agy-scope",
			AccountID:     scope,
			ConfirmedAtMS: 1784000005000,
		}},
		Quotas: []QuotaObservation{
			{
				Provider:        "agy",
				ID:              "agy-q-gemini-5h",
				AccountID:       new(scope),
				LocalScope:      "public-agy-scope",
				LimitID:         "agy.gemini",
				WindowKind:      "primary",
				WindowMinutes:   new(int64(300)),
				WindowStartAtMS: new(int64(1783982000000)),
				ResetsAtMS:      new(int64(1784000000000)),
				ObservedAtMS:    1783990000000,
				UsedPercent:     new(25.0),
				Validity:        "accepted",
				Source:          "agy_ls_rpc",
				HistoryOrigin:   "confirmed",
			},
			{
				Provider:        "agy",
				ID:              "agy-q-claude-weekly",
				AccountID:       new(scope),
				LocalScope:      "public-agy-scope",
				LimitID:         "agy.claude_gpt",
				WindowKind:      "secondary",
				WindowMinutes:   new(int64(10080)),
				WindowStartAtMS: new(int64(1783395200000)),
				ResetsAtMS:      new(int64(1784000000000)),
				ObservedAtMS:    1783990000000,
				UsedPercent:     new(10.0),
				Validity:        "accepted",
				Source:          "agy_ls_rpc",
				HistoryOrigin:   "confirmed",
			},
		},
		Status: []DeviceStatus{
			{Provider: "codex", Status: "ready"},
			{Provider: "cursor", Status: "ready"},
			{Provider: "grok", Status: "ready"},
			{Provider: "dsh", Status: "ready"},
			{Provider: "agy", Status: "disabled"},
		},
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("valid AGY batch rejected: %v", err)
	}

	// Reject unknown source kind, unknown quota source, and >5 device statuses.
	badSource := b
	badSource.Sessions = []SessionSnapshot{b.Sessions[0]}
	badSource.Sessions[0].SourceKind = "agy_unknown"
	if !errors.Is(badSource.Validate(), ErrInvalid) {
		t.Fatal("unknown AGY source_kind accepted")
	}
	badRole := b
	badRole.Sessions = []SessionSnapshot{b.Sessions[0]}
	badRole.Sessions[0].SessionRole = "admin_override"
	if !errors.Is(badRole.Validate(), ErrInvalid) {
		t.Fatal("unknown AGY session_role accepted")
	}

	badQuota := b
	badQuota.Quotas = []QuotaObservation{b.Quotas[0]}
	badQuota.Quotas[0].Source = "agy_oauth"
	if !errors.Is(badQuota.Validate(), ErrInvalid) {
		t.Fatal("unknown AGY quota source accepted")
	}

	tooManyStatuses := b
	tooManyStatuses.Status = append(append([]DeviceStatus(nil), b.Status...), DeviceStatus{Provider: "agy", Status: "ready"})
	if !errors.Is(tooManyStatuses.Validate(), ErrInvalid) {
		t.Fatal("more than 5 device statuses accepted")
	}
}
