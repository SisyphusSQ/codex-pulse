package catalog_srv

import (
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	catalog_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/catalog_repo"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestAGYCatalogVersionedPhasesAndObservedUnknown(t *testing.T) {
	engine := centerfixture.Engine(t)
	db := engine.DB(t.Context())
	if err := db.Create(&reporting_do.Session{ID: "agy-sess", Provider: "agy", SessionID: "agy-cascade", CanonicalRevision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	// Known AGY alias (HideInMenu=true) and truly unknown AGY model.
	if err := db.Create(&reporting_do.Usage{SessionKey: "agy-sess", ContributionID: "1", Position: 1, Model: new("gemini-4-carbon-high"), CostStatus: "unpriced", PricingMode: "event_cost"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&reporting_do.Usage{SessionKey: "agy-sess", ContributionID: "2", Position: 2, Model: new("agy-future-unpriced"), CostStatus: "unpriced", PricingMode: "event_cost"}).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewCatalog(catalog_repo.NewCatalog(engine))
	admin := access_dto.Principal{ID: "synthetic-admin", Purpose: "admin"}

	// Phase 1 (2026-11-26): Carbon=2/0.1/10, Flash 3.8=0.75/0.075/3.75.
	svc.now = func() time.Time { return time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC) }
	out1, err := svc.Current(t.Context(), admin)
	if err != nil {
		t.Fatal(err)
	}
	var carbonCur1, carbonHist1, carbonHist2, flashCur1, unknownObserved bool
	for _, m := range out1.Models {
		if m.Provider != "agy" {
			continue
		}
		if m.Model == "gemini-4-carbon-high" && m.Evidence == "observed" {
			t.Fatalf("known AGY alias wrongly marked observed unknown: %+v", m)
		}
		if m.Model == "agy-future-unpriced" && m.Evidence == "observed" && m.InputPrice == nil {
			unknownObserved = true
		}
		if m.Model == "gemini-4-carbon" {
			switch {
			case m.Evidence == "current" && m.Version == pricing.AGYPriceVersion+":phase1" && *m.InputPrice == "2" && *m.CachedPrice == "0.1" && *m.OutputPrice == "10":
				carbonCur1 = true
			case m.Evidence == "historical" && m.Version == pricing.AGYPriceVersion+":phase1" && *m.InputPrice == "2":
				carbonHist1 = true
			case m.Evidence == "historical" && m.Version == pricing.AGYPriceVersion+":phase2" && *m.InputPrice == "4" && *m.OutputPrice == "20":
				carbonHist2 = true
			}
		}
		if m.Model == "gemini-3.8-flash" && m.Evidence == "current" && m.Version == pricing.AGYPriceVersion+":phase1" && *m.InputPrice == "0.75" && *m.OutputPrice == "3.75" {
			flashCur1 = true
		}
	}
	if !carbonCur1 || !carbonHist1 || !carbonHist2 || !flashCur1 || !unknownObserved {
		t.Fatalf("phase1 AGY catalog mismatch: carbonCur1=%v carbonHist1=%v carbonHist2=%v flashCur1=%v unknownObserved=%v",
			carbonCur1, carbonHist1, carbonHist2, flashCur1, unknownObserved)
	}

	// Phase 2 (2027-01-02): Carbon=4/0.2/20, Flash 3.8=1.5/0.15/7.5.
	svc.now = func() time.Time { return time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC) }
	out2, err := svc.Current(t.Context(), admin)
	if err != nil {
		t.Fatal(err)
	}
	var carbonCur2, flashCur2 bool
	for _, m := range out2.Models {
		if m.Provider != "agy" || m.Evidence != "current" {
			continue
		}
		if m.Model == "gemini-4-carbon" && m.Version == pricing.AGYPriceVersion+":phase2" && *m.InputPrice == "4" && *m.CachedPrice == "0.2" && *m.OutputPrice == "20" {
			carbonCur2 = true
		}
		if m.Model == "gemini-3.8-flash" && m.Version == pricing.AGYPriceVersion+":phase2" && *m.InputPrice == "1.5" && *m.CachedPrice == "0.15" && *m.OutputPrice == "7.5" {
			flashCur2 = true
		}
	}
	if !carbonCur2 || !flashCur2 {
		t.Fatalf("phase2 AGY catalog switch failed: carbonCur2=%v flashCur2=%v", carbonCur2, flashCur2)
	}
	agyPlans := 0
	for _, p := range out1.Plans {
		if p.Provider == "agy" {
			agyPlans++
		}
	}
	if agyPlans < 4 {
		t.Fatalf("expected AGY subscription plans in catalog, got %d", agyPlans)
	}
	var hasOpus46Release, hasGemini31Release, carbonReleaseUnknown bool
	for _, m := range out1.Models {
		if m.Provider != "agy" || m.Evidence != "current" {
			continue
		}
		switch m.Model {
		case "claude-opus-4-6":
			if m.ReleasedAtMS != nil && time.UnixMilli(*m.ReleasedAtMS).UTC().Format(time.DateOnly) == "2026-02-05" {
				hasOpus46Release = true
			}
		case "gemini-3.1-pro":
			if m.ReleasedAtMS != nil && time.UnixMilli(*m.ReleasedAtMS).UTC().Format(time.DateOnly) == "2026-02-19" {
				hasGemini31Release = true
			}
		case "gemini-4-carbon":
			if m.ReleasedAtMS == nil {
				carbonReleaseUnknown = true
			}
		}
	}
	if !hasOpus46Release || !hasGemini31Release || !carbonReleaseUnknown {
		t.Fatalf("AGY release dates mismatch: opus46=%v gemini31=%v carbonUnknown=%v", hasOpus46Release, hasGemini31Release, carbonReleaseUnknown)
	}
}
