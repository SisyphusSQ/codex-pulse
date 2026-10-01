package reportingv1

import (
	"errors"
	"math"
	"testing"
)

func TestCacheUsageCapsuleReconcilesLifetimeAndRejectsUnsupportedVersion(t *testing.T) {
	b := validBatch()
	s := &b.Sessions[0]
	s.SourceKind = "light_index"
	s.Contributions[0].CachedTokens = new(int64(0))
	s.CacheUsage = &CacheUsageCapsule{Version: 1, Basis: "lifetime_cached_input", InputTokens: new(int64(0)), CachedInputTokens: new(int64(0))}
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	s.CacheUsage.InputTokens = new(int64(1))
	if !errors.Is(b.Validate(), ErrInvalid) {
		t.Fatal("unmatched lifetime accepted")
	}
	s.CacheUsage.InputTokens = nil
	s.CacheUsage.CachedInputTokens = nil
	s.CacheUsage.Reason = "history_filtered"
	s.HistoryStartAtMS = 1000
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	s.CacheUsage.Version = 2
	if !errors.Is(b.Validate(), ErrVersion) {
		t.Fatal("unknown version accepted")
	}
	s.CacheUsage.Version = 1
	s.Provider = "cursor"
	if !errors.Is(b.Validate(), ErrInvalid) {
		t.Fatal("unsupported provider accepted")
	}
}

func TestCacheUsageKeepsInt64DecimalPrecision(t *testing.T) {
	s := SessionSnapshot{Provider: "codex", SourceKind: "light_index", Contributions: []Contribution{{InputTokens: new(int64(math.MaxInt64)), CachedTokens: new(int64(math.MaxInt64))}}, CacheUsage: &CacheUsageCapsule{Version: 1, Basis: "lifetime_cached_input", InputTokens: new(int64(math.MaxInt64)), CachedInputTokens: new(int64(math.MaxInt64))}}
	if !validCacheUsage(s) {
		t.Fatal("decimal counters incorrectly constrained to JavaScript numbers")
	}
}
