package catalog_srv

import (
	"slices"
	"testing"
	"time"

	catalog_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/catalog_vo"
)

func TestCatalogModelReleaseOrderAndIndependentPriceDates(t *testing.T) {
	priceDate := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC).UnixMilli()
	models := []catalog_vo.Model{
		{Key: "old", Provider: "codex", Model: "gpt-4.1", InputPrice: new("2"), EffectiveFromMS: &priceDate, VerifiedAtMS: priceDate},
		{Key: "unknown", Provider: "codex", Model: "unpublished-model", VerifiedAtMS: priceDate, EffectiveFromMS: &priceDate},
		{Key: "sol-credits", Provider: "codex", Model: "GPT-6.1 Sol", Currency: "credits"},
		{Key: "sol-usd", Provider: "codex", Model: "gpt-6.1-sol", Currency: "USD"},
		{Key: "cursor-alias", Provider: "cursor", Model: "cursor-grok-4.6-xhigh-fast"},
	}
	if err := applyModelReleases(models); err != nil {
		t.Fatal(err)
	}
	if *models[0].EffectiveFromMS != priceDate || models[0].VerifiedAtMS != priceDate || *models[0].InputPrice != "2" || *models[0].ReleasedAtMS == priceDate {
		t.Fatal("release metadata changed the rate or reused a pricing date")
	}
	if models[1].ReleasedAtMS != nil || models[1].ReleaseSourceURL != "" {
		t.Fatal("unknown model acquired an invented release date")
	}
	solDate := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC).UnixMilli()
	if *models[2].ReleasedAtMS != solDate || *models[3].ReleasedAtMS != solDate || models[2].ReleaseSourceURL == "" {
		t.Fatal("different modes did not share the verified model release")
	}
	if models[4].ReleasedAtMS == nil || *models[4].ReleasedAtMS != time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatal("known Cursor effort/fast alias lost its model release")
	}
	slices.SortFunc(models, compareModelRelease)
	if models[0].Key != "sol-credits" || models[1].Key != "sol-usd" || models[2].Key != "cursor-alias" || models[3].Key != "old" || models[4].Key != "unknown" {
		t.Fatal("release order, tie order or unknown tail is incorrect", models)
	}
}
