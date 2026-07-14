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
