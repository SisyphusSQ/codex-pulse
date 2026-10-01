package core

import (
	"testing"

	corev1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/core/v1"
	query "github.com/SisyphusSQ/codex-pulse/internal/query"
)

func TestCacheHitRateProtoPreservesPresenceAndUnit(t *testing.T) {
	for _, value := range []int64{0, 9_125, 10_000} {
		rate, err := query.KnownNumeric(value, query.NumericBasisPoints)
		if err != nil {
			t.Fatal(err)
		}
		target := &corev1.SessionItem{}
		if err := EncodeResponse(map[string]any{"cacheHitRate": rate}, target); err != nil {
			t.Fatal(err)
		}
		if target.CacheHitRate == nil || target.CacheHitRate.Value == nil ||
			target.CacheHitRate.GetValue() != value || target.CacheHitRate.Unit != "basis_points" {
			t.Fatalf("cache hit rate = %+v", target.CacheHitRate)
		}
	}
	unknown, err := query.UnknownNumeric(query.NumericBasisPoints, query.UnknownNotApplicable)
	if err != nil {
		t.Fatal(err)
	}
	target := &corev1.SessionItem{}
	if err := EncodeResponse(map[string]any{"cacheHitRate": unknown}, target); err != nil {
		t.Fatal(err)
	}
	if target.CacheHitRate.Value != nil || target.CacheHitRate.GetUnknownReason() != "not_applicable" {
		t.Fatal("unknown cache hit rate became zero")
	}
	if err := EncodeResponse(map[string]any{}, target); err != nil {
		t.Fatal(err)
	}
	if target.CacheHitRate != nil {
		t.Fatal("absent provider metric became present")
	}
}
