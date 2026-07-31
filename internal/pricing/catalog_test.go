package pricing

import (
	"reflect"
	"testing"
)

func TestBuiltinOpenAI20260714CatalogIsFrozenAndDefensivelyCopied(t *testing.T) {
	t.Parallel()

	first := BuiltinOpenAI20260714()
	if first.PricingVersion != "openai-api-2026-07-14" ||
		first.Source != "openai-api" || first.Currency != "USD" ||
		first.EffectiveFromMS != 0 || first.CreatedAtMS != 1_783_987_200_000 ||
		first.SourceURL != "https://developers.openai.com/api/docs/pricing" ||
		first.VerifiedAtMS != 1_783_987_200_000 {
		t.Fatalf("BuiltinOpenAI20260714() metadata = %#v", first)
	}

	want := map[string][3]int64{
		"gpt-5-codex":       {1_250_000, 125_000, 10_000_000},
		"gpt-5.1-codex":     {1_250_000, 125_000, 10_000_000},
		"gpt-5.1-codex-max": {1_250_000, 125_000, 10_000_000},
		"gpt-5.2-codex":     {1_750_000, 175_000, 14_000_000},
		"gpt-5.3-codex":     {1_750_000, 175_000, 14_000_000},
		"gpt-5.4":           {2_500_000, 250_000, 15_000_000},
		"gpt-5.5":           {5_000_000, 500_000, 30_000_000},
		"gpt-5.6":           {5_000_000, 500_000, 30_000_000},
		"gpt-5.6-sol":       {5_000_000, 500_000, 30_000_000},
		"gpt-5.6-terra":     {2_500_000, 250_000, 15_000_000},
		"gpt-5.6-luna":      {1_000_000, 100_000, 6_000_000},
	}
	got := make(map[string][3]int64, len(first.Models))
	for _, model := range first.Models {
		if model.MatchKind != ModelMatchExact || model.Priority != 100 ||
			model.InputMicrosPerMillion == nil ||
			model.CachedInputMicrosPerMillion == nil ||
			model.OutputMicrosPerMillion == nil {
			t.Fatalf("catalog model contract = %#v", model)
		}
		got[model.ModelPattern] = [3]int64{
			*model.InputMicrosPerMillion,
			*model.CachedInputMicrosPerMillion,
			*model.OutputMicrosPerMillion,
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog prices = %#v, want %#v", got, want)
	}

	*first.Models[0].InputMicrosPerMillion = 1
	first.Models[0].ModelPattern = "mutated"
	second := BuiltinOpenAI20260714()
	if second.Models[0].ModelPattern == "mutated" || *second.Models[0].InputMicrosPerMillion == 1 {
		t.Fatalf("BuiltinOpenAI20260714() leaked mutable catalog state: %#v", second.Models[0])
	}
}

func TestBuiltinOpenAI20260722AddsGPT54MiniWithoutMutatingPriorCatalog(t *testing.T) {
	t.Parallel()

	catalog := BuiltinOpenAI20260722()
	if catalog.PricingVersion != "openai-api-2026-07-22" ||
		catalog.EffectiveFromMS != 1_773_705_600_000 || catalog.VerifiedAtMS != 1_784_678_400_000 {
		t.Fatalf("BuiltinOpenAI20260722() metadata = %#v", catalog)
	}
	var mini *ModelPrice
	for index := range catalog.Models {
		if catalog.Models[index].ModelPattern == "gpt-5.4-mini" {
			mini = &catalog.Models[index]
			break
		}
	}
	if mini == nil || mini.InputMicrosPerMillion == nil || *mini.InputMicrosPerMillion != 750_000 ||
		mini.CachedInputMicrosPerMillion == nil || *mini.CachedInputMicrosPerMillion != 75_000 ||
		mini.OutputMicrosPerMillion == nil || *mini.OutputMicrosPerMillion != 4_500_000 {
		t.Fatalf("gpt-5.4-mini catalog entry = %#v", mini)
	}
	versions := BuiltinOpenAICatalog()
	if len(versions) != 4 || versions[0].PricingVersion != "openai-api-2026-07-14" ||
		versions[1].PricingVersion != "openai-api-2026-07-22" ||
		versions[2].PricingVersion != "openai-api-2026-07-29" ||
		versions[3].PricingVersion != "openai-api-2026-07-31" ||
		len(BuiltinOpenAI20260714().Models) != 11 {
		t.Fatalf("builtin catalog history = %#v", versions)
	}
}

func TestBuiltinOpenAI20260729UsesGeneralPricingSourceWithoutMutatingHistory(t *testing.T) {
	t.Parallel()

	current := BuiltinOpenAI20260729()
	previous := BuiltinOpenAI20260722()
	if current.PricingVersion != "openai-api-2026-07-29" ||
		current.EffectiveFromMS != 1_785_254_400_000 ||
		current.VerifiedAtMS != 1_785_254_400_000 ||
		current.SourceURL != "https://developers.openai.com/api/docs/pricing" ||
		!reflect.DeepEqual(current.Models, previous.Models) {
		t.Fatalf("BuiltinOpenAI20260729() = %#v, previous = %#v", current, previous)
	}
	if previous.SourceURL != "https://developers.openai.com/api/docs/models/gpt-5.4-mini" {
		t.Fatalf("BuiltinOpenAI20260722() source URL mutated to %q", previous.SourceURL)
	}
}

func TestBuiltinOpenAICatalogApplies20260731PriceCutsWithoutMutatingHistory(t *testing.T) {
	t.Parallel()

	versions := BuiltinOpenAICatalog()
	if len(versions) != 4 {
		t.Fatalf("BuiltinOpenAICatalog() versions = %d, want 4", len(versions))
	}
	current := versions[3]
	if current.PricingVersion != "openai-api-2026-07-31" ||
		current.EffectiveFromMS != 1_785_464_448_000 ||
		current.VerifiedAtMS != 1_785_464_448_000 ||
		current.SourceURL != "https://developers.openai.com/api/docs/pricing" {
		t.Fatalf("current catalog metadata = %#v", current)
	}

	wantCurrent := map[string][3]int64{
		"gpt-5.6-terra": {2_000_000, 200_000, 12_000_000},
		"gpt-5.6-luna":  {200_000, 20_000, 1_200_000},
	}
	gotCurrent := selectedCatalogRates(t, current, "gpt-5.6-terra", "gpt-5.6-luna")
	if !reflect.DeepEqual(gotCurrent, wantCurrent) {
		t.Fatalf("current reduced prices = %#v, want %#v", gotCurrent, wantCurrent)
	}

	previous := BuiltinOpenAI20260729()
	wantPrevious := map[string][3]int64{
		"gpt-5.6-terra": {2_500_000, 250_000, 15_000_000},
		"gpt-5.6-luna":  {1_000_000, 100_000, 6_000_000},
	}
	gotPrevious := selectedCatalogRates(t, previous, "gpt-5.6-terra", "gpt-5.6-luna")
	if !reflect.DeepEqual(gotPrevious, wantPrevious) {
		t.Fatalf("previous prices mutated = %#v, want %#v", gotPrevious, wantPrevious)
	}
}

func selectedCatalogRates(t *testing.T, catalog CatalogVersion, modelIDs ...string) map[string][3]int64 {
	t.Helper()

	selected := make(map[string]struct{}, len(modelIDs))
	for _, modelID := range modelIDs {
		selected[modelID] = struct{}{}
	}
	got := make(map[string][3]int64, len(modelIDs))
	for _, model := range catalog.Models {
		if _, ok := selected[model.ModelPattern]; !ok {
			continue
		}
		if model.InputMicrosPerMillion == nil ||
			model.CachedInputMicrosPerMillion == nil ||
			model.OutputMicrosPerMillion == nil {
			t.Fatalf("catalog model %q has incomplete rates: %#v", model.ModelPattern, model)
		}
		got[model.ModelPattern] = [3]int64{
			*model.InputMicrosPerMillion,
			*model.CachedInputMicrosPerMillion,
			*model.OutputMicrosPerMillion,
		}
	}
	return got
}
