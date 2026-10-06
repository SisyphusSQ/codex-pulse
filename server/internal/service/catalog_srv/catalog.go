package catalog_srv

import (
	"context"
	_ "embed"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strconv"
	"strings"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	catalog_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/catalog_vo"
	catalog_repo "github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/catalog_repo"
	access_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
)

//go:embed models-20261002.json
var publicModels []byte

//go:embed plans-20261002.json
var publicPlans []byte

type Catalog struct{ repository *catalog_repo.Catalog }

func NewCatalog(repository *catalog_repo.Catalog) *Catalog { return &Catalog{repository: repository} }
func (s *Catalog) Current(ctx context.Context, p access_dto.Principal) (out catalog_vo.Response, err error) {
	if err = access_srv.RequireAdmin(p); err != nil {
		return
	}
	err = s.repository.Snapshot(ctx, func(ctx context.Context) error {
		out, err = s.current(ctx)
		return err
	})
	return
}

func (s *Catalog) current(ctx context.Context) (out catalog_vo.Response, err error) {
	out.Version = "reference-2026-10-02"
	out.Models = []catalog_vo.Model{}
	out.Plans = []catalog_vo.Plan{}
	if err = json.Unmarshal(publicModels, &out.Models, json.RejectUnknownMembers(true)); err != nil {
		return out, fmt.Errorf("read public model catalog: %w", err)
	}
	if err = json.Unmarshal(publicPlans, &out.Plans, json.RejectUnknownMembers(true)); err != nil {
		return out, fmt.Errorf("read public plan catalog: %w", err)
	}
	for _, version := range pricing.BuiltinOpenAICatalog() {
		for _, m := range version.Models {
			if m.MatchKind != pricing.ModelMatchExact {
				continue
			}
			out.Models = append(out.Models, catalog_vo.Model{Key: "codex:" + m.ModelPattern + ":" + version.PricingVersion, Provider: "codex", Model: m.ModelPattern, Mode: "Standard · 本机历史基础文本", Currency: "USD", Unit: "1M tokens", InputPrice: amount(m.InputMicrosPerMillion), CachedPrice: amount(m.CachedInputMicrosPerMillion), OutputPrice: amount(m.OutputMicrosPerMillion), Version: version.PricingVersion, SourceURL: version.SourceURL, VerifiedAtMS: version.VerifiedAtMS, EffectiveFromMS: new(version.EffectiveFromMS), Evidence: "historical", Notes: "本机历史计算目录；不推断Fast、长上下文或缓存写入费率。"})
		}
	}
	for _, m := range pricing.BuiltinCursorModelRates() {
		out.Models = append(out.Models, catalog_vo.Model{Key: "cursor:" + m.ModelID + ":" + pricing.CursorPricingVersion, Provider: "cursor", Model: m.ModelID, Mode: "本机参考目录", Currency: "USD", Unit: "1M tokens", InputPrice: amount(new(m.InputMicros)), CachedPrice: amount(new(m.CacheReadMicros)), CacheWritePrice: amount(m.CacheWriteMicros), OutputPrice: amount(new(m.OutputMicros)), Version: pricing.CursorPricingVersion, SourceURL: pricing.CursorPricingSourceURL, VerifiedAtMS: pricing.CursorPricingVerifiedAtMS, Evidence: "historical", Notes: "已上报统计继续使用其费率证据；当前参考价单独展示。"})
	}
	for _, m := range pricing.BuiltinGrokModelRates() {
		out.Models = append(out.Models, catalog_vo.Model{Key: "grok:" + m.ModelID + ":" + pricing.GrokPricingVersion, Provider: "grok", Model: m.ModelID, Mode: "本机参考目录", Currency: "USD", Unit: "1M tokens", InputPrice: amount(new(m.InputMicros)), CachedPrice: amount(new(m.CachedMicros)), OutputPrice: amount(new(m.OutputMicros)), Version: pricing.GrokPricingVersion, SourceURL: pricing.GrokPricingSourceURL, VerifiedAtMS: pricing.GrokPricingVerifiedAtMS, Evidence: "historical"})
	}

	for _, m := range pricing.DSHReferenceRates() {
		out.Models = append(out.Models, catalog_vo.Model{Key: "dsh:" + m.ModelID + ":" + pricing.DSHPriceVersion, Provider: "dsh", Model: m.ModelID, Mode: "按请求时间适用峰谷价", Currency: "USD", Unit: "1M tokens", InputPrice: amount(new(m.InputMicros)), CachedPrice: amount(new(m.CachedMicros)), OutputPrice: amount(new(m.OutputMicros)), Version: pricing.DSHPriceVersion, SourceURL: pricing.DSHPriceSource, VerifiedAtMS: pricing.DSHPricingVerifiedAtMS, Evidence: "historical", Notes: "北京时间工作日09–12、14–18为峰时；周末、中国法定节假日和其余时间为谷时。缺少历史价格或缓存计数时不估价。"})
	}
	evidence, err := s.repository.Prices(ctx)
	if err != nil {
		return out, err
	}
	for _, r := range evidence {
		a, b, c, d := amount(r.InputPrice), amount(r.CachedPrice), amount(r.CacheWritePrice), amount(r.OutputPrice)
		duplicate := slices.ContainsFunc(out.Models, func(m catalog_vo.Model) bool {
			return m.Provider == r.Provider && m.Model == r.Model && m.Version == r.PricingVersion && sameAmount(m.InputPrice, a) && sameAmount(m.CachedPrice, b) && sameAmount(m.CacheWritePrice, c) && sameAmount(m.OutputPrice, d)
		})
		if duplicate {
			continue
		}
		out.Models = append(out.Models, catalog_vo.Model{Key: reportingv1.Key("price-evidence", r.Provider, r.Model, r.PricingVersion, value(a), value(b), value(c), value(d)), Provider: r.Provider, Model: r.Model, Mode: "已上报历史费率", Currency: "USD", Unit: "1M tokens", InputPrice: a, CachedPrice: b, CacheWritePrice: c, OutputPrice: d, Version: r.PricingVersion, Evidence: "historical", Notes: "来自已接受的结构化费率证据；没有额外官方核对时间，不改写历史成本。"})
	}
	observed, err := s.repository.Models(ctx)
	if err != nil {
		return out, err
	}
	known := make(map[string]bool)
	for _, m := range out.Models {
		if m.Evidence == "current" {
			known[m.Provider+"\x00"+strings.ToLower(m.Model)] = true
		}
	}
	for _, m := range observed {
		if known[m.Provider+"\x00"+strings.ToLower(m.Model)] {
			continue
		}
		out.Models = append(out.Models, catalog_vo.Model{Key: m.Provider + ":observed:" + m.Model, Provider: m.Provider, Model: m.Model, Mode: "已观测 · 参考价未知", Currency: "USD", Unit: "未知", Evidence: "observed", Notes: "保留实际模型名；没有匹配公开价格证据，不猜价或视为免费。"})
	}
	if err := applyModelReleases(out.Models); err != nil {
		return out, err
	}
	slices.SortFunc(out.Models, compareModelRelease)
	return out, nil
}
func amount(v *int64) *string {
	if v == nil {
		return nil
	}
	whole := strconv.FormatInt(*v/1_000_000, 10)
	fraction := strings.TrimRight(fmt.Sprintf("%06d", *v%1_000_000), "0")
	if fraction != "" {
		whole += "." + fraction
	}
	return &whole
}

func value(v *string) string {
	if v == nil {
		return "null"
	}
	return *v
}
func sameAmount(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}
