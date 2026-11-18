package pricing

import (
	"reflect"
	"testing"
)

func TestGPT62CatalogExtendsHistoryAndDSHUsesExactRates(t *testing.T) {
	t.Parallel()
	versions := BuiltinOpenAICatalog()
	current := versions[len(versions)-1]
	previous := BuiltinOpenAI20261026()
	const publishedAt = 1_794_873_600_000
	if current.PricingVersion != "openai-api-2026-11-17" || current.EffectiveFromMS != publishedAt ||
		current.VerifiedAtMS <= publishedAt || len(current.Models) != len(previous.Models)+2 ||
		!reflect.DeepEqual(current.Models[:len(previous.Models)], previous.Models) {
		t.Fatal("6.2 catalog did not append two models to the immutable history")
	}
	for key, want := range map[string][3]int64{
		"gpt-6.2-astra": {8_000_000, 400_000, 40_000_000},
		"gpt-6.2-sol":   {2_000_000, 100_000, 10_000_000},
	} {
		if _, ok := DSHRateAt("openai-codex", key, publishedAt-1); ok {
			t.Fatalf("%s priced before publication", key)
		}
		rate, ok := DSHRateAt("openai-codex", key, publishedAt)
		if !ok || rate.Version != current.PricingVersion || [3]int64{rate.InputMicros, rate.CachedMicros, rate.OutputMicros} != want {
			t.Fatalf("%s rate = %#v", key, rate)
		}
		if _, ok := DSHRateAt("openai-codex", key+"-fast", publishedAt); ok {
			t.Fatalf("invented alias for %s", key)
		}
	}
}
