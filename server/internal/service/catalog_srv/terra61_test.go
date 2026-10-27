package catalog_srv

import (
	"testing"

	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	catalog_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/catalog_repo"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestTerraCatalogCurrentModesHistoryAndRelease(t *testing.T) {
	engine := centerfixture.Engine(t)
	out, err := NewCatalog(catalog_repo.NewCatalog(engine)).Current(t.Context(), access_dto.Principal{ID: "synthetic-admin", Purpose: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	current, historical, dsh := 0, 0, 0
	for _, row := range out.Models {
		if row.Model != "gpt-6.1-terra" {
			continue
		}
		if row.ReleasedAtMS == nil || *row.ReleasedAtMS != 1_792_972_800_000 || row.ReleaseSourceURL != "https://developers.openai.com/api/docs/changelog" {
			t.Fatal("release evidence missing", row)
		}
		if row.Evidence == "historical" {
			historical++
		}
		if row.Provider == "dsh" && row.Evidence == "current" {
			dsh++
			if row.InputPrice == nil || *row.InputPrice != "1" || row.CachedPrice == nil || *row.CachedPrice != "0.05" || row.OutputPrice == nil || *row.OutputPrice != "5" || row.CacheWritePrice != nil {
				t.Fatal("DSH basis changed", row)
			}
		}
		if row.Provider != "codex" || row.Evidence != "current" {
			continue
		}
		current++
		if row.Version != "reference-2026-10-27" || row.EffectiveFromMS != nil || row.VerifiedAtMS == 0 {
			t.Fatal("verification/effective date mixed", row)
		}
		if row.Mode == "Standard · 短上下文" && (*row.InputPrice != "1" || *row.CachedPrice != "0.05" || *row.OutputPrice != "5" || *row.CacheWritePrice != "1.25") {
			t.Fatal("standard rates", row)
		}
		if row.Mode == "Fast · 长上下文 · 缓存12小时" && (*row.InputPrice != "4" || *row.CachedPrice != "0.20" || *row.OutputPrice != "15.0" || *row.CacheWritePrice != "10.00") {
			t.Fatal("fast long 12h rates", row)
		}
	}
	if current != 16 || historical != 2 || dsh != 1 {
		t.Fatal("incomplete Terra catalog", current, historical, dsh)
	}
}
