package store

import (
	"context"
	"errors"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	"reflect"
	"testing"
)

// 测试 PricingVersion immutable、半开生效区间和 deterministic model match。
func TestPricingVersionsAreImmutableAndUseHalfOpenEffectiveIntervals(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	versionOne := pricing.CatalogVersion{
		PricingVersion:  "pricing-v1",
		Source:          "builtin",
		Currency:        "USD",
		EffectiveFromMS: 100,
		CreatedAtMS:     90,
		Models: []pricing.ModelPrice{
			{
				MatchKind: pricing.ModelMatchExact, ModelPattern: "gpt-5", Priority: 10,
				InputMicrosPerMillion:       pointerTo(int64(1_000_000)),
				CachedInputMicrosPerMillion: pointerTo(int64(0)),
				OutputMicrosPerMillion:      pointerTo(int64(2_000_000)),
			},
			{
				MatchKind: pricing.ModelMatchPrefix, ModelPattern: "gpt-", Priority: 10,
				InputMicrosPerMillion:  pointerTo(int64(900_000)),
				OutputMicrosPerMillion: pointerTo(int64(1_800_000)),
			},
			{
				MatchKind: pricing.ModelMatchDefault, ModelPattern: "*", Priority: 1,
				InputMicrosPerMillion: pointerTo(int64(500_000)),
			},
		},
	}
	versionTwo := pricing.CatalogVersion{
		PricingVersion: "pricing-v2", Source: "builtin", Currency: "USD",
		EffectiveFromMS: 200, CreatedAtMS: 190,
		Models: []pricing.ModelPrice{{
			MatchKind: pricing.ModelMatchExact, ModelPattern: "gpt-5", Priority: 10,
			InputMicrosPerMillion:  pointerTo(int64(1_100_000)),
			OutputMicrosPerMillion: pointerTo(int64(2_100_000)),
		}},
	}
	for _, version := range []pricing.CatalogVersion{versionOne, versionTwo} {
		if err := repository.AddPricingVersion(context.Background(), version); err != nil {
			t.Fatalf("AddPricingVersion(%s) error = %v", version.PricingVersion, err)
		}
	}
	if err := repository.AddPricingVersion(context.Background(), versionOne); err != nil {
		t.Fatalf("AddPricingVersion(replay) error = %v", err)
	}

	if _, err := repository.PricingForModelAt(context.Background(), "builtin", "USD", "gpt-5", 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("PricingForModelAt(before first) error = %v, want ErrNotFound", err)
	}
	for _, atMS := range []int64{100, 199} {
		got, err := repository.PricingForModelAt(context.Background(), "builtin", "USD", "gpt-5", atMS)
		if err != nil {
			t.Fatalf("PricingForModelAt(%d) error = %v", atMS, err)
		}
		if got.PricingVersion.PricingVersion != versionOne.PricingVersion ||
			got.EffectiveToMS == nil || *got.EffectiveToMS != 200 ||
			got.Matched.MatchKind != pricing.ModelMatchExact ||
			got.Matched.CachedInputMicrosPerMillion == nil || *got.Matched.CachedInputMicrosPerMillion != 0 {
			t.Fatalf("PricingForModelAt(%d) = %#v, want v1 exact with real cached zero and end=200", atMS, got)
		}
	}
	gotV2, err := repository.PricingForModelAt(context.Background(), "builtin", "USD", "gpt-5", 200)
	if err != nil {
		t.Fatalf("PricingForModelAt(v2 boundary) error = %v", err)
	}
	if gotV2.PricingVersion.PricingVersion != versionTwo.PricingVersion || gotV2.EffectiveToMS != nil {
		t.Fatalf("PricingForModelAt(v2 boundary) = %#v, want open v2 interval", gotV2)
	}

	prefix, err := repository.PricingForModelAt(context.Background(), "builtin", "USD", "gpt-4.1", 150)
	if err != nil {
		t.Fatalf("PricingForModelAt(prefix) error = %v", err)
	}
	if prefix.Matched.MatchKind != pricing.ModelMatchPrefix || prefix.Matched.ModelPattern != "gpt-" {
		t.Fatalf("PricingForModelAt(prefix) matched = %#v", prefix.Matched)
	}
	defaultPrice, err := repository.PricingForModelAt(context.Background(), "builtin", "USD", "other-model", 150)
	if err != nil {
		t.Fatalf("PricingForModelAt(default) error = %v", err)
	}
	if defaultPrice.Matched.MatchKind != pricing.ModelMatchDefault {
		t.Fatalf("PricingForModelAt(default) matched = %#v", defaultPrice.Matched)
	}
	if _, err := repository.PricingForModelAt(context.Background(), "builtin", "CNY", "gpt-5", 150); !errors.Is(err, ErrNotFound) {
		t.Fatalf("PricingForModelAt(currency isolation) error = %v, want ErrNotFound", err)
	}

	mutated := versionOne
	mutated.Models = append([]pricing.ModelPrice(nil), versionOne.Models...)
	mutated.Models[0].InputMicrosPerMillion = pointerTo(int64(9_999_999))
	if err := repository.AddPricingVersion(context.Background(), mutated); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("AddPricingVersion(mutation) error = %v, want ErrInvalidRecord", err)
	}
	stored, err := repository.PricingVersion(context.Background(), versionOne.PricingVersion)
	if err != nil {
		t.Fatalf("PricingVersion() error = %v", err)
	}
	if !pricingVersionsEquivalent(stored, versionOne) {
		t.Fatalf("stored version changed after mutation attempt: got %#v, want %#v", stored, versionOne)
	}

	sameBoundary := versionOne
	sameBoundary.PricingVersion = "pricing-conflicting-boundary"
	if err := repository.AddPricingVersion(context.Background(), sameBoundary); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("AddPricingVersion(same boundary) error = %v, want ErrInvalidRecord", err)
	}
}

// 测试 Pricing Catalog 原子拒绝重复规则并保持 nullable/真实零语义。
func TestPricingCatalogRejectsInvalidOrDuplicateRulesAtomically(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	invalid := pricing.CatalogVersion{
		PricingVersion: "pricing-invalid", Source: "builtin", Currency: "USD",
		EffectiveFromMS: 100, CreatedAtMS: 90,
		Models: []pricing.ModelPrice{
			{MatchKind: pricing.ModelMatchDefault, ModelPattern: "*", Priority: 1, InputMicrosPerMillion: pointerTo(int64(1))},
			{MatchKind: pricing.ModelMatchDefault, ModelPattern: "*", Priority: 2, InputMicrosPerMillion: pointerTo(int64(2))},
		},
	}
	if err := repository.AddPricingVersion(context.Background(), invalid); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("AddPricingVersion(duplicate rules) error = %v, want ErrInvalidRecord", err)
	}
	if _, err := repository.PricingVersion(context.Background(), invalid.PricingVersion); !errors.Is(err, ErrNotFound) {
		t.Fatalf("PricingVersion(invalid) error = %v, want ErrNotFound", err)
	}

	unknownAndZero := pricing.CatalogVersion{
		PricingVersion: "pricing-null-zero", Source: "builtin", Currency: "USD",
		EffectiveFromMS: 300, CreatedAtMS: 290,
		Models: []pricing.ModelPrice{{
			MatchKind: pricing.ModelMatchDefault, ModelPattern: "*", Priority: 1,
			InputMicrosPerMillion: nil, CachedInputMicrosPerMillion: pointerTo(int64(0)),
			OutputMicrosPerMillion: nil,
		}},
	}
	if err := repository.AddPricingVersion(context.Background(), unknownAndZero); err != nil {
		t.Fatalf("AddPricingVersion(null/zero) error = %v", err)
	}
	stored, err := repository.PricingVersion(context.Background(), unknownAndZero.PricingVersion)
	if err != nil {
		t.Fatalf("PricingVersion(null/zero) error = %v", err)
	}
	if !reflect.DeepEqual(stored.Models[0].InputMicrosPerMillion, (*int64)(nil)) ||
		stored.Models[0].CachedInputMicrosPerMillion == nil || *stored.Models[0].CachedInputMicrosPerMillion != 0 ||
		stored.Models[0].OutputMicrosPerMillion != nil {
		t.Fatalf("null/zero round trip = %#v", stored.Models[0])
	}
}

// 测试 PricingCatalogAt 在生效边界返回完整版本，而不是单模型匹配结果。
func TestPricingCatalogAtSelectsLatestEffectiveVersion(t *testing.T) {
	t.Parallel()

	repository := openRuntimeRepository(t)
	for _, version := range []pricing.CatalogVersion{
		{
			PricingVersion: "catalog-v1", Source: "openai-api", Currency: "USD",
			EffectiveFromMS: 100, CreatedAtMS: 90,
			Models: []pricing.ModelPrice{{
				MatchKind: pricing.ModelMatchExact, ModelPattern: "model-a", Priority: 100,
				InputMicrosPerMillion: pointerTo(int64(1)),
			}},
		},
		{
			PricingVersion: "catalog-v2", Source: "openai-api", Currency: "USD",
			EffectiveFromMS: 200, CreatedAtMS: 190,
			Models: []pricing.ModelPrice{{
				MatchKind: pricing.ModelMatchExact, ModelPattern: "model-b", Priority: 100,
				OutputMicrosPerMillion: pointerTo(int64(2)),
			}},
		},
	} {
		if err := repository.AddPricingVersion(context.Background(), version); err != nil {
			t.Fatalf("AddPricingVersion(%s) error = %v", version.PricingVersion, err)
		}
	}

	if _, err := repository.PricingCatalogAt(context.Background(), "openai-api", "USD", 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("PricingCatalogAt(before first) error = %v, want ErrNotFound", err)
	}
	got, err := repository.PricingCatalogAt(context.Background(), "openai-api", "USD", 200)
	if err != nil {
		t.Fatalf("PricingCatalogAt(boundary) error = %v", err)
	}
	if got.PricingVersion != "catalog-v2" || len(got.Models) != 1 ||
		got.Models[0].ModelPattern != "model-b" {
		t.Fatalf("PricingCatalogAt(boundary) = %#v", got)
	}
}
