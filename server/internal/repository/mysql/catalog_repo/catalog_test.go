package catalog_repo

import (
	"context"
	"reflect"
	"strconv"
	"testing"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	catalog_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/catalog_dto"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestCatalogUsesOnlyPublishedFacts(t *testing.T) {
	engine := centerfixture.Engine(t)
	db := engine.DB(t.Context())
	for _, key := range []string{"legacy", "published", "deleted", "staged", "empty"} {
		if err := db.Create(&reporting_do.Session{ID: key, Provider: "codex", Deleted: key == "deleted"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{"published", "empty", "deleted"} {
		if err := db.Create(&reporting_do.SessionPublication{SessionKey: key, Generation: "current"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		session, generation, model string
		position                   int64
		price                      *int64
	}{
		{"legacy", "", "legacy-model", 1, nil},
		{"legacy", "", "legacy-model", 2, nil},
		{"published", "", "replaced-inline", 1, new(int64(1))},
		{"published", "obsolete", "obsolete-model", 1, new(int64(2))},
		{"published", "pending", "pending-model", 1, new(int64(3))},
		{"published", "current", "published-model", 1, new(int64(9007199254740993))},
		{"published", "current", "published-model", 2, new(int64(9007199254740993))},
		{"published", "current", "published-model", 3, new(int64(0))},
		{"deleted", "current", "deleted-model", 1, new(int64(4))},
		{"staged", "pending", "never-published", 1, new(int64(5))},
		{"empty", "", "removed-inline", 1, new(int64(6))},
	} {
		row := reporting_do.Usage{SessionKey: item.session, ContributionID: item.model, Position: item.position, Model: new(item.model), PricingVersion: new("historical"), InputPrice: item.price}
		// 同值的独立贡献只在目录中去重；不同费率仍保留。
		row.ContributionID += strconv.FormatInt(item.position, 10)
		var err error
		if item.generation == "" {
			err = db.Create(&row).Error
		} else {
			err = db.Create(&reporting_do.UsageVersion{Generation: item.generation, Usage: row}).Error
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	// 无价格证据的模型仍可见；NULL 模型不进入目录。
	for i, model := range []*string{new("unpriced-model"), nil} {
		row := reporting_do.Usage{SessionKey: "published", ContributionID: strconv.Itoa(i), Position: int64(10 + i), Model: model}
		if err := db.Create(&reporting_do.UsageVersion{Generation: "current", Usage: row}).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := NewCatalog(engine)
	err := repo.Snapshot(t.Context(), func(ctx context.Context) error {
		models, err := repo.Models(ctx)
		if err != nil {
			return err
		}
		want := []catalog_dto.ObservedModel{{Provider: "codex", Model: "legacy-model"}, {Provider: "codex", Model: "published-model"}, {Provider: "codex", Model: "unpriced-model"}}
		if !reflect.DeepEqual(models, want) {
			t.Fatalf("published model catalog = %#v, want %#v", models, want)
		}
		prices, err := repo.Prices(ctx)
		if err != nil {
			return err
		}
		if len(prices) != 3 {
			t.Fatalf("price evidence count = %d, want 3", len(prices))
		}
		seen := map[string]bool{}
		for _, row := range prices {
			if row.Provider != "codex" || row.PricingVersion != "historical" || row.CachedPrice != nil || row.CacheWritePrice != nil || row.OutputPrice != nil {
				t.Fatalf("price evidence changed: %#v", row)
			}
			switch {
			case row.Model == "legacy-model" && row.InputPrice == nil:
				seen["unknown"] = true
			case row.Model == "published-model" && row.InputPrice != nil && *row.InputPrice == 0:
				seen["zero"] = true
			case row.Model == "published-model" && row.InputPrice != nil && *row.InputPrice == 9007199254740993:
				seen["exact"] = true
			default:
				t.Fatalf("unexpected price evidence: %#v", row)
			}
		}
		if len(seen) != 3 {
			t.Fatal("NULL, zero or exact price evidence lost", seen)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
