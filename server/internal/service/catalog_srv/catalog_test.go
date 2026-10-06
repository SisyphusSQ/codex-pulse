package catalog_srv

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	catalog_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/catalog_repo"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func TestCatalogAllModelsVersionsAndObservedUnknown(t *testing.T) {
	engine := centerfixture.Engine(t)
	s := NewCatalog(catalog_repo.NewCatalog(engine))
	admin := access_dto.Principal{ID: "synthetic-admin", Purpose: "admin"}
	if err := engine.DB(t.Context()).Create(&reporting_do.Session{ID: "synthetic", Provider: "codex", SessionID: "synthetic-session", CanonicalRevision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := engine.DB(t.Context()).Create(&reporting_do.Usage{SessionKey: "synthetic", Position: 1, Model: new("unpublished-model"), CostStatus: "unpriced", PricingMode: "event_cost"}).Error; err != nil {
		t.Fatal(err)
	}
	out, err := s.Current(t.Context(), admin)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	unknown, old, credits, media := false, false, false, false
	for _, r := range out.Models {
		if seen[r.Key] {
			t.Fatal("duplicate key", r.Key)
		}
		seen[r.Key] = true
		if r.Model == "unpublished-model" {
			unknown = r.Evidence == "observed" && r.InputPrice == nil && r.OutputPrice == nil
		}
		if r.Model == "gpt-5.3-codex" {
			old = true
		}
		credits = credits || r.Currency == "credits"
		media = media || r.Unit == "image"
	}
	if !unknown || !old || !credits || !media || len(out.Models) < 300 || len(out.Plans) < 20 {
		t.Fatal("incomplete catalog", len(out.Models), unknown, old, credits, media)
	}
	if !slices.IsSortedFunc(out.Models, compareModelRelease) || out.Models[0].ReleasedAtMS == nil || out.Models[len(out.Models)-1].ReleasedAtMS != nil {
		t.Fatal("full catalog is not ordered newest first with unknown dates last")
	}
	if _, err := s.Current(t.Context(), access_dto.Principal{Purpose: "collector"}); !errors.Is(err, utils.ErrForbidden) {
		t.Fatal("collector queried prices", err)
	}
}

func TestCatalogReportedHistoricalRateTrace(t *testing.T) {
	engine := centerfixture.Engine(t)
	db := engine.DB(t.Context())
	if err := db.Create(&reporting_do.Session{ID: "reported", Provider: "codex", SessionID: "reported-session", CanonicalRevision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	for _, position := range []int64{1, 2} {
		row := reporting_do.Usage{SessionKey: "reported", ContributionID: strconv.FormatInt(position, 10), Position: position, Model: new("unpublished-model"), PricingVersion: new("received-historical"), InputPrice: new(int64(2000000)), CachedPrice: new(int64(100000)), OutputPrice: new(int64(10000000)), CostStatus: "known", PricingMode: "codex_model_sum"}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	out, err := NewCatalog(catalog_repo.NewCatalog(engine)).Current(t.Context(), access_dto.Principal{ID: "synthetic-admin", Purpose: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	historical, unknown := 0, false
	for _, row := range out.Models {
		if row.Version == "received-historical" {
			historical++
			if row.Evidence != "historical" || row.InputPrice == nil || *row.InputPrice != "2" || row.CachedPrice == nil || *row.CachedPrice != "0.1" || row.OutputPrice == nil || *row.OutputPrice != "10" || row.CacheWritePrice != nil || row.VerifiedAtMS != 0 || row.SourceURL != "" || row.EffectiveFromMS != nil {
				t.Fatal("incorrect received evidence", row)
			}
		}
		unknown = unknown || (row.Model == "unpublished-model" && row.Evidence == "observed" && row.InputPrice == nil)
	}
	if historical != 1 || !unknown {
		t.Fatal("historical rate dedup or unknown reference", historical, unknown)
	}
}

func TestDSHCodexCatalogHasHistoricalPricesAndKnownObservedModel(t *testing.T) {
	engine := centerfixture.Engine(t)
	db := engine.DB(t.Context())
	if err := db.Create(&reporting_do.Session{ID: "dsh", Provider: "dsh", SessionID: "dsh-synthetic", CanonicalRevision: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&reporting_do.Usage{SessionKey: "dsh", Position: 1, Model: new("gpt-6.1-sol"), CostStatus: "unpriced", PricingMode: "event_cost"}).Error; err != nil {
		t.Fatal(err)
	}
	out, err := NewCatalog(catalog_repo.NewCatalog(engine)).Current(t.Context(), access_dto.Principal{ID: "synthetic-admin", Purpose: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range out.Models {
		if m.Provider == "dsh" && m.Model == "gpt-6.1-sol" {
			if m.Evidence == "observed" {
				t.Fatal("supported DSH model marked price unknown", m)
			}
			if m.Version == "openai-api-2026-09-29" {
				found = true
				if m.InputPrice == nil || *m.InputPrice != "2" || m.CachedPrice == nil || *m.CachedPrice != "0.1" || m.OutputPrice == nil || *m.OutputPrice != "10" || m.SourceURL != "https://developers.openai.com/api/docs/pricing" || m.EffectiveFromMS == nil || !strings.Contains(m.Notes, "订阅实际扣费") {
					t.Fatal("wrong DSH OpenAI evidence", m)
				}
			}
		}
	}
	if !found {
		t.Fatal("DSH OpenAI history missing")
	}
}
