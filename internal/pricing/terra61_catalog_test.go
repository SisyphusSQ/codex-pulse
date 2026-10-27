package pricing

import (
	"reflect"
	"testing"
)

func TestGPT61TerraCatalogPreservesHistoryAndCopies(t *testing.T) {
	versions := BuiltinOpenAICatalog()
	if len(versions) != 9 {
		t.Fatalf("catalog versions = %d", len(versions))
	}
	current, previous := versions[8], versions[7]
	if current.PricingVersion != "openai-api-2026-10-26" || current.EffectiveFromMS != 1_792_972_800_000 || current.VerifiedAtMS <= current.EffectiveFromMS {
		t.Fatalf("metadata = %#v", current)
	}
	if !reflect.DeepEqual(current.Models[:len(previous.Models)], previous.Models) {
		t.Fatal("old rates changed")
	}
	want := map[string][3]int64{"gpt-6.1-terra": {1_000_000, 50_000, 5_000_000}}
	if !reflect.DeepEqual(selectedCatalogRates(t, current, "gpt-6.1-terra"), want) {
		t.Fatal("wrong Terra rates")
	}
	for _, old := range versions[:8] {
		if len(selectedCatalogRates(t, old, "gpt-6.1-terra")) != 0 {
			t.Fatal("old catalog changed")
		}
	}
	*current.Models[0].InputMicrosPerMillion = 0
	*current.Models[len(current.Models)-1].InputMicrosPerMillion = 0
	if !reflect.DeepEqual(previous, BuiltinOpenAI20260929()) || !reflect.DeepEqual(selectedCatalogRates(t, BuiltinOpenAI20261026(), "gpt-6.1-terra"), want) {
		t.Fatal("mutation leaked")
	}
}

func TestDSHTerraHistoricalPriceAndExclusiveBuckets(t *testing.T) {
	at := BuiltinOpenAI20261026().EffectiveFromMS
	for _, tc := range []struct {
		route, model string
		at           int64
		known        bool
	}{
		{"openai-codex", "gpt-6.1-terra", at - 1, false},
		{"openai-codex", "gpt-6.1-terra", at, true},
		{"openai-codex", "gpt-6.1-terra", at + 1, true},
		{"deepseek-account", "gpt-6.1-terra", at, false},
		{"openai-codex", "gpt-6.1-terra-fast", at, false},
	} {
		rate, ok := DSHRateAt(tc.route, tc.model, tc.at)
		if ok != tc.known {
			t.Fatalf("unexpected rate: %+v", tc)
		}
		if !ok {
			continue
		}
		// DSH input/output are exclusive: output already includes reasoning.
		if cost, known := EstimateDSHCost(rate, 800_000, 200_000, 0, 150_000); !known || cost != 1_560_000 {
			t.Fatalf("cost = %d, %v", cost, known)
		}
		if _, known := EstimateDSHCost(rate, 1, 1, 1, 1); known {
			t.Fatal("invented cache write")
		}
	}
}
