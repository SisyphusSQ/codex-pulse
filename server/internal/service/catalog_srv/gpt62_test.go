package catalog_srv

import (
	"testing"
	"time"

	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	catalog_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/catalog_vo"
	catalog_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/catalog_repo"
	centerfixture "github.com/SisyphusSQ/codex-pulse/server/internal/testsupport/center"
)

func TestGPT62PublicRatesAndUltrafastBoundary(t *testing.T) {
	out, err := NewCatalog(catalog_repo.NewCatalog(centerfixture.Engine(t))).Current(t.Context(), access_dto.Principal{ID: "synthetic-admin", Purpose: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	byKey := make(map[string]catalog_vo.Model, len(out.Models))
	for _, row := range out.Models {
		if _, exists := byKey[row.Key]; exists {
			t.Fatalf("duplicate catalog key %s", row.Key)
		}
		byKey[row.Key] = row
	}
	release := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC).UnixMilli()
	switchAt := time.Date(2027, 3, 1, 8, 0, 0, 0, time.UTC).UnixMilli()
	for _, tc := range []struct {
		model, mode, currency                string
		input, cacheRead, cacheWrite, output string
		future                               bool
	}{
		{"gpt-6.2-astra", "Standard · 短上下文", "USD", "8", "0.4", "10", "40", false},
		{"gpt-6.2-sol", "Standard · 短上下文", "USD", "2", "0.1", "2.5", "10", false},
		{"gpt-6.2-astra", "Batch · 长上下文 · 缓存12小时", "USD", "8", "0.4", "20", "30", false},
		{"gpt-6.2-sol", "Fast · 长上下文", "USD", "8", "0.4", "10", "30", false},
		{"gpt-6.2-astra", "Ultrafast · 短上下文 · 促销期", "USD", "24", "1.2", "30", "120", false},
		{"gpt-6.2-astra", "Ultrafast · 短上下文 · 促销结束后", "USD", "48", "2.4", "60", "240", true},
		{"gpt-6.2-sol", "Ultrafast · 促销期 · Codex Credits", "credits", "150", "7.5", "", "750", false},
		{"gpt-6.2-sol", "Ultrafast · 促销结束后 · Codex Credits", "credits", "300", "15", "", "1500", true},
	} {
		row := byKey["codex:"+tc.model+":"+tc.mode+":1M tokens:"+tc.currency]
		if row.Evidence != "current" || row.ReleasedAtMS == nil || *row.ReleasedAtMS != release ||
			value(row.InputPrice) != tc.input || value(row.CachedPrice) != tc.cacheRead || value(row.OutputPrice) != tc.output ||
			(tc.cacheWrite == "" && row.CacheWritePrice != nil) || (tc.cacheWrite != "" && value(row.CacheWritePrice) != tc.cacheWrite) {
			t.Fatalf("%s %s has wrong rates or release metadata: %#v", tc.model, tc.mode, row)
		}
		if tc.future && (row.EffectiveFromMS == nil || *row.EffectiveFromMS != switchAt) || !tc.future && row.EffectiveFromMS != nil {
			t.Fatalf("%s %s has wrong effective boundary: %v", tc.model, tc.mode, row.EffectiveFromMS)
		}
	}
}
