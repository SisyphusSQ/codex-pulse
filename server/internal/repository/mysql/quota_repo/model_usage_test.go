package quota_repo

import (
	"testing"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestAccountModelFactsReadOnlyPublishedUsageRows(t *testing.T) {
	engine := centerfixture.Engine(t)
	db := engine.DB(t.Context())
	period := reporting_do.AccountTokenPeriod{ID: "period", AccountKey: "account", Provider: "codex", ClientID: "client", WindowStartAtMS: 0, ResetsAtMS: 1000, CollectedAtMS: 500}
	if err := db.Create(&period).Error; err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"migrated", "versioned", "deleted"} {
		meta := reporting_do.Session{ID: session, SessionID: session, Provider: "codex", Deleted: session == "deleted"}
		if err := db.Create(&meta).Error; err != nil {
			t.Fatal(err)
		}
		fact := reporting_do.AccountTokenFact{ID: session, AccountKey: "account", Provider: "codex", ObservedAtMS: 100, TotalTokens: 10}
		if err := db.Create(&fact).Error; err != nil {
			t.Fatal(err)
		}
		usage := reporting_do.Usage{SessionKey: session, ContributionID: session, ObservedAtMS: new(int64(100)), TotalTokens: new(int64(10)), Model: new(session + "-model")}
		if err := db.Create(&reporting_do.UsageVersion{Generation: session, Usage: usage}).Error; err != nil {
			t.Fatal(err)
		}
		if session != "versioned" {
			if err := db.Create(&reporting_do.SessionPublication{SessionKey: session, Generation: session}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	for generation, model := range map[string]string{"published": "published-model", "staged": "staged-model"} {
		row := reporting_do.UsageVersion{Generation: generation, Usage: reporting_do.Usage{SessionKey: "versioned", ContributionID: "versioned", ObservedAtMS: new(int64(100)), TotalTokens: new(int64(10)), Model: new(model)}}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&reporting_do.SessionPublication{SessionKey: "versioned", Generation: "published"}).Error; err != nil {
		t.Fatal(err)
	}
	var unmatched int
	models := map[string]int{}
	available, err := NewQuota(engine).StreamAccountModelFacts(t.Context(), quota_dto.Query{}, "codex", "account", 0, 1000, 500, func(row quota_dto.AccountModelFact) {
		if !row.Matched {
			unmatched++
		} else {
			models[*row.Model]++
		}
	})
	if err != nil || !available || unmatched != 1 || len(models) != 2 || models["migrated-model"] != 1 || models["published-model"] != 1 {
		t.Fatalf("usage rows: %v unmatched=%d available=%v err=%v", models, unmatched, available, err)
	}
}

func TestAccountModelFactsIsolateProvidersAndWindowDurations(t *testing.T) {
	engine := centerfixture.Engine(t)
	db := engine.DB(t.Context())
	// AGY 5h period [700, 1000) must not activate AGY weekly [0, 1000) or Codex.
	if err := db.Create(&reporting_do.AccountTokenPeriod{
		ID: "agy-5h", AccountKey: "acct-agy", Provider: "agy", ClientID: "client",
		WindowStartAtMS: 700, ResetsAtMS: 1000, CollectedAtMS: 850,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&reporting_do.Session{ID: "agy-s1", SessionID: "agy-s1", Provider: "agy"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&reporting_do.AccountTokenFact{ID: "agy-f1", AccountKey: "acct-agy", Provider: "agy", ObservedAtMS: 800, TotalTokens: 1600}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&reporting_do.UsageVersion{
		Generation: "g1",
		Usage: reporting_do.Usage{
			SessionKey: "agy-s1", ContributionID: "agy-f1", ObservedAtMS: new(int64(800)),
			Model: new("gemini-4-carbon-high"), InputTokens: new(int64(1500)), CachedTokens: new(int64(500)),
			OutputTokens: new(int64(100)), TotalTokens: new(int64(1600)),
		},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&reporting_do.SessionPublication{SessionKey: "agy-s1", Generation: "g1"}).Error; err != nil {
		t.Fatal(err)
	}

	repo := NewQuota(engine)
	// Matching 5h duration [700, 1000) succeeds and returns AGY model fact with nil reasoning.
	var got []quota_dto.AccountModelFact
	available, err := repo.StreamAccountModelFacts(t.Context(), quota_dto.Query{}, "agy", "acct-agy", 700, 1000, 850, func(row quota_dto.AccountModelFact) {
		got = append(got, row)
	})
	if err != nil || !available || len(got) != 1 || !got[0].Matched || got[0].Model == nil || *got[0].Model != "gemini-4-carbon-high" || got[0].ReasoningTokens != nil {
		t.Fatalf("agy 5h stream mismatch: available=%v err=%v got=%+v", available, err, got)
	}
	// Different window duration [0, 1000) must remain unavailable (not falsely activated by 5h period).
	available, err = repo.StreamAccountModelFacts(t.Context(), quota_dto.Query{}, "agy", "acct-agy", 0, 1000, 850, func(quota_dto.AccountModelFact) {})
	if err != nil || available {
		t.Fatalf("5h period falsely activated weekly window: available=%v err=%v", available, err)
	}
	// Different provider must remain unavailable.
	available, err = repo.StreamAccountModelFacts(t.Context(), quota_dto.Query{}, "codex", "acct-agy", 700, 1000, 850, func(quota_dto.AccountModelFact) {})
	if err != nil || available {
		t.Fatalf("agy period leaked to codex: available=%v err=%v", available, err)
	}
}
