package pricingcatalog

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
)

var ErrInvalidService = errors.New("pricing catalog query service is invalid")

type Reader interface {
	PricingCatalogAt(context.Context, string, string, int64) (pricing.CatalogVersion, error)
}

type Service struct {
	reader Reader
	now    func() time.Time
}

func NewService(reader Reader, now func() time.Time) (*Service, error) {
	if reader == nil || now == nil {
		return nil, ErrInvalidService
	}
	return &Service{reader: reader, now: now}, nil
}

// Current 返回当前完整 API 参考价格目录；它不表示 Codex 订阅的实际账单。
func (service *Service) Current(ctx context.Context, scope agentprovider.Scope) (CurrentResponse, error) {
	if service == nil || service.reader == nil || service.now == nil {
		return CurrentResponse{}, ErrInvalidService
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return CurrentResponse{}, err
	}
	provider, err := agentprovider.Normalize(scope.Provider)
	if err != nil {
		return CurrentResponse{}, basequery.NewValidationFailure("provider", err)
	}
	evaluatedAtMS := service.now().UnixMilli()
	if evaluatedAtMS < 0 || evaluatedAtMS > basequery.JavaScriptMaxSafeInteger {
		return CurrentResponse{}, basequery.NewUnavailableFailure(
			fmt.Errorf("pricing catalog clock is outside supported range"),
		)
	}
	switch provider {
	case agentprovider.Cursor:
		return currentCursorResponse(evaluatedAtMS)
	case agentprovider.Grok:
		return currentGrokResponse(evaluatedAtMS)
	case agentprovider.Codex:
	default:
		return CurrentResponse{}, basequery.NewValidationFailure("provider", agentprovider.ErrInvalidProvider)
	}
	catalog, err := service.reader.PricingCatalogAt(
		ctx,
		SourceOpenAIAPI,
		CurrencyUSD,
		evaluatedAtMS,
	)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return CurrentResponse{}, err
		}
		return CurrentResponse{}, basequery.NewUnavailableFailure(err)
	}
	return currentResponse(catalog, evaluatedAtMS)
}

func currentResponse(catalog pricing.CatalogVersion, evaluatedAtMS int64) (CurrentResponse, error) {
	if catalog.PricingVersion == "" || catalog.Source != SourceOpenAIAPI ||
		catalog.Currency != CurrencyUSD || catalog.EffectiveFromMS < 0 ||
		catalog.EffectiveFromMS > evaluatedAtMS || len(catalog.Models) == 0 {
		return CurrentResponse{}, unavailableCatalog()
	}
	sourceURL, err := validatedSourceURL(catalog.SourceURL)
	if err != nil {
		return CurrentResponse{}, err
	}
	items := make([]ModelReferencePrice, 0, len(catalog.Models))
	seen := make(map[string]struct{}, len(catalog.Models))
	for _, model := range catalog.Models {
		if model.MatchKind != pricing.ModelMatchExact || model.ModelPattern == "" {
			return CurrentResponse{}, unavailableCatalog()
		}
		if _, exists := seen[model.ModelPattern]; exists {
			return CurrentResponse{}, unavailableCatalog()
		}
		seen[model.ModelPattern] = struct{}{}
		input, err := referenceRate(model.InputMicrosPerMillion)
		if err != nil {
			return CurrentResponse{}, err
		}
		cached, err := referenceRate(model.CachedInputMicrosPerMillion)
		if err != nil {
			return CurrentResponse{}, err
		}
		output, err := referenceRate(model.OutputMicrosPerMillion)
		if err != nil {
			return CurrentResponse{}, err
		}
		cacheWrite, err := referenceRate(nil)
		if err != nil {
			return CurrentResponse{}, err
		}
		items = append(items, ModelReferencePrice{
			ModelID: model.ModelPattern, InputMicros: input,
			CachedInputMicros: cached, OutputMicros: output, CacheWriteMicros: cacheWrite,
		})
	}
	sort.Slice(items, func(left, right int) bool {
		return items[left].ModelID < items[right].ModelID
	})

	meta, err := basequery.NewResponseMeta(basequery.ResponseComplete, nil, nil)
	if err != nil {
		return CurrentResponse{}, err
	}
	evaluated, err := basequery.KnownNumeric(evaluatedAtMS, basequery.NumericMilliseconds)
	if err != nil {
		return CurrentResponse{}, unavailableCatalog()
	}
	unitTokens, err := basequery.KnownNumeric(UnitTokens, basequery.NumericTokens)
	if err != nil {
		return CurrentResponse{}, unavailableCatalog()
	}
	effective, err := basequery.KnownNumeric(catalog.EffectiveFromMS, basequery.NumericMilliseconds)
	if err != nil {
		return CurrentResponse{}, unavailableCatalog()
	}
	verified, err := optionalTimestamp(catalog.VerifiedAtMS)
	if err != nil {
		return CurrentResponse{}, err
	}
	return CurrentResponse{
		ProviderContext: pricingProviderContext(agentprovider.Codex, SourceOpenAIAPI, int64(len(items))),
		Meta:            meta, EvaluatedAtMS: evaluated, PricingVersion: catalog.PricingVersion,
		Source: catalog.Source, Currency: catalog.Currency, Basis: BasisStandard,
		UnitTokens: unitTokens, EffectiveFromMS: effective, VerifiedAtMS: verified,
		SourceURL: sourceURL, Items: items,
	}, nil
}

func currentCursorResponse(evaluatedAtMS int64) (CurrentResponse, error) {
	rates := pricing.BuiltinCursorModelRates()
	items := make([]ModelReferencePrice, 0, len(rates))
	for _, rate := range rates {
		input, err := referenceRate(&rate.InputMicros)
		if err != nil {
			return CurrentResponse{}, err
		}
		cacheWrite, err := referenceRate(rate.CacheWriteMicros)
		if err != nil {
			return CurrentResponse{}, err
		}
		cacheRead, err := referenceRate(&rate.CacheReadMicros)
		if err != nil {
			return CurrentResponse{}, err
		}
		output, err := referenceRate(&rate.OutputMicros)
		if err != nil {
			return CurrentResponse{}, err
		}
		items = append(items, ModelReferencePrice{
			ModelID: rate.ModelID, InputMicros: input, CacheWriteMicros: cacheWrite,
			CachedInputMicros: cacheRead, OutputMicros: output,
		})
	}
	sort.Slice(items, func(left, right int) bool { return items[left].ModelID < items[right].ModelID })

	meta, err := basequery.NewResponseMeta(basequery.ResponseComplete, nil, nil)
	if err != nil {
		return CurrentResponse{}, err
	}
	evaluated, err := basequery.KnownNumeric(evaluatedAtMS, basequery.NumericMilliseconds)
	if err != nil {
		return CurrentResponse{}, unavailableCatalog()
	}
	unitTokens, err := basequery.KnownNumeric(UnitTokens, basequery.NumericTokens)
	if err != nil {
		return CurrentResponse{}, unavailableCatalog()
	}
	effective, err := basequery.KnownNumeric(0, basequery.NumericMilliseconds)
	if err != nil {
		return CurrentResponse{}, unavailableCatalog()
	}
	verified, err := basequery.KnownNumeric(pricing.CursorPricingVerifiedAtMS, basequery.NumericMilliseconds)
	if err != nil {
		return CurrentResponse{}, unavailableCatalog()
	}
	sourceURL := pricing.CursorPricingSourceURL
	return CurrentResponse{
		ProviderContext: pricingProviderContext(agentprovider.Cursor, SourceCursorDocs, int64(len(items))),
		Meta:            meta, EvaluatedAtMS: evaluated, PricingVersion: pricing.CursorPricingVersion,
		Source: SourceCursorDocs, Currency: CurrencyUSD, Basis: BasisCursor,
		UnitTokens: unitTokens, EffectiveFromMS: effective, VerifiedAtMS: verified,
		SourceURL: &sourceURL, Items: items,
	}, nil
}

func currentGrokResponse(evaluatedAtMS int64) (CurrentResponse, error) {
	rates := pricing.BuiltinGrokModelRates()
	items := make([]ModelReferencePrice, 0, len(rates))
	for _, rate := range rates {
		input, err := referenceRate(&rate.InputMicros)
		if err != nil {
			return CurrentResponse{}, err
		}
		cached, err := referenceRate(&rate.CachedMicros)
		if err != nil {
			return CurrentResponse{}, err
		}
		output, err := referenceRate(&rate.OutputMicros)
		if err != nil {
			return CurrentResponse{}, err
		}
		cacheWrite, err := referenceRate(nil)
		if err != nil {
			return CurrentResponse{}, err
		}
		items = append(items, ModelReferencePrice{
			ModelID: rate.ModelID, InputMicros: input, CachedInputMicros: cached,
			OutputMicros: output, CacheWriteMicros: cacheWrite,
		})
	}
	sort.Slice(items, func(left, right int) bool { return items[left].ModelID < items[right].ModelID })
	meta, err := basequery.NewResponseMeta(basequery.ResponseComplete, nil, nil)
	if err != nil {
		return CurrentResponse{}, err
	}
	evaluated, err := basequery.KnownNumeric(evaluatedAtMS, basequery.NumericMilliseconds)
	if err != nil {
		return CurrentResponse{}, unavailableCatalog()
	}
	unitTokens, err := basequery.KnownNumeric(UnitTokens, basequery.NumericTokens)
	if err != nil {
		return CurrentResponse{}, unavailableCatalog()
	}
	effective, err := basequery.KnownNumeric(0, basequery.NumericMilliseconds)
	if err != nil {
		return CurrentResponse{}, unavailableCatalog()
	}
	verified, err := basequery.KnownNumeric(pricing.GrokPricingVerifiedAtMS, basequery.NumericMilliseconds)
	if err != nil {
		return CurrentResponse{}, unavailableCatalog()
	}
	sourceURL := pricing.GrokPricingSourceURL
	return CurrentResponse{
		ProviderContext: pricingProviderContext(agentprovider.Grok, SourceXAIDocs, int64(len(items))),
		Meta:            meta, EvaluatedAtMS: evaluated, PricingVersion: pricing.GrokPricingVersion,
		Source: SourceXAIDocs, Currency: CurrencyUSD, Basis: BasisGrok,
		UnitTokens: unitTokens, EffectiveFromMS: effective, VerifiedAtMS: verified,
		SourceURL: &sourceURL, Items: items,
	}, nil
}

func pricingProviderContext(provider, source string, itemCount int64) agentprovider.Context {
	return agentprovider.Context{
		EffectiveProvider: provider,
		Sources:           []string{source},
		Capabilities:      []string{"model_reference_pricing"},
		Coverage: []agentprovider.Coverage{{
			Capability: "model_reference_pricing", State: "available", Source: source,
			Reason: "published_model_rates", ItemCount: &itemCount,
		}},
	}
}

func referenceRate(value *int64) (basequery.NumericValue, error) {
	if value == nil {
		return basequery.UnknownNumeric(basequery.NumericMicroUSD, basequery.UnknownUnavailable)
	}
	numeric, err := basequery.KnownNumeric(*value, basequery.NumericMicroUSD)
	if err != nil {
		return basequery.NumericValue{}, unavailableCatalog()
	}
	return numeric, nil
}

func optionalTimestamp(value int64) (basequery.NumericValue, error) {
	if value == 0 {
		return basequery.UnknownNumeric(basequery.NumericMilliseconds, basequery.UnknownUnavailable)
	}
	numeric, err := basequery.KnownNumeric(value, basequery.NumericMilliseconds)
	if err != nil {
		return basequery.NumericValue{}, unavailableCatalog()
	}
	return numeric, nil
}

func validatedSourceURL(value string) (*string, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "developers.openai.com" ||
		parsed.User != nil {
		return nil, unavailableCatalog()
	}
	return &value, nil
}

func unavailableCatalog() error {
	return basequery.NewUnavailableFailure(fmt.Errorf("pricing catalog is inconsistent"))
}
