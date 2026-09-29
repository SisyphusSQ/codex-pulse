package pricing

import (
	"reflect"
	"testing"
)

func TestGPT61SolCatalogPreservesHistoryAndIndependentCopies(t *testing.T) {
	t.Parallel()
	versions := BuiltinOpenAICatalog()
	if len(versions) != 8 {
		t.Fatalf("catalog versions = %d, want 8", len(versions))
	}
	current, previous := versions[7], versions[6]
	if current.PricingVersion != "openai-api-2026-09-29" ||
		current.EffectiveFromMS != 1_790_640_000_000 ||
		current.CreatedAtMS != 1_790_721_621_000 || current.VerifiedAtMS != current.CreatedAtMS ||
		current.Source != "openai-api" || current.Currency != "USD" ||
		current.SourceURL != "https://developers.openai.com/api/docs/pricing" {
		t.Fatalf("6.1 Sol catalog metadata = %#v", current)
	}
	want := map[string][3]int64{"gpt-6.1-sol": {2_000_000, 100_000, 10_000_000}}
	if got := selectedCatalogRates(t, current, "gpt-6.1-sol"); !reflect.DeepEqual(got, want) {
		t.Fatalf("6.1 Sol rates = %#v, want %#v", got, want)
	}
	if len(current.Models) != len(previous.Models)+1 ||
		!reflect.DeepEqual(current.Models[:len(previous.Models)], previous.Models) {
		t.Fatal("6.1 Sol addition changed previous model prices")
	}
	for i, catalog := range versions {
		if i > 0 && catalog.EffectiveFromMS <= versions[i-1].EffectiveFromMS {
			t.Fatal("catalog history is not strictly ordered")
		}
		for _, model := range catalog.Models {
			if model.MatchKind != ModelMatchExact || model.Priority != 100 {
				t.Fatalf("non-exact catalog rule: %#v", model)
			}
		}
		if i < 7 && len(selectedCatalogRates(t, catalog, "gpt-6.1-sol")) != 0 {
			t.Fatal("previous catalog unexpectedly prices 6.1 Sol")
		}
	}
	*current.Models[0].InputMicrosPerMillion = 0
	*current.Models[len(current.Models)-1].CachedInputMicrosPerMillion = 0
	if !reflect.DeepEqual(BuiltinOpenAI20260922(), previous) ||
		*BuiltinOpenAI20260929().Models[0].InputMicrosPerMillion == 0 ||
		!reflect.DeepEqual(selectedCatalogRates(t, BuiltinOpenAI20260929(), "gpt-6.1-sol"), want) {
		t.Fatal("catalog mutation leaked to prior or fresh copies")
	}
}
