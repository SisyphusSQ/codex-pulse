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
	available, err := NewQuota(engine).StreamAccountModelFacts(t.Context(), quota_dto.Query{}, "account", 0, 1000, 500, func(row quota_dto.AccountModelFact) {
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
