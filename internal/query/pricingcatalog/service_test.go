package pricingcatalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/agentprovider"
	"github.com/SisyphusSQ/codex-pulse/internal/pricing"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
)

func TestCurrentReturnsSortedExactPricesAndPreservesUnknown(t *testing.T) {
	t.Parallel()

	reader := &readerStub{catalog: pricing.CatalogVersion{
		PricingVersion: "openai-api-test", Source: SourceOpenAIAPI, Currency: CurrencyUSD,
		EffectiveFromMS: 100, SourceURL: "https://developers.openai.com/api/docs/pricing",
		VerifiedAtMS: 150,
		Models: []pricing.ModelPrice{
			{
				MatchKind: pricing.ModelMatchExact, ModelPattern: "model-b", Priority: 100,
				InputMicrosPerMillion:       pointerTo(int64(1_000_000)),
				CachedInputMicrosPerMillion: nil,
				OutputMicrosPerMillion:      pointerTo(int64(2_000_000)),
			},
			{
				MatchKind: pricing.ModelMatchExact, ModelPattern: "model-a", Priority: 100,
				InputMicrosPerMillion:       pointerTo(int64(0)),
				CachedInputMicrosPerMillion: pointerTo(int64(100_000)),
				OutputMicrosPerMillion:      pointerTo(int64(3_000_000)),
			},
		},
	}}
	service, err := NewService(reader, func() time.Time { return time.UnixMilli(200) })
	if err != nil {
		t.Fatal(err)
	}

	got, err := service.Current(context.Background(), agentprovider.Scope{Provider: agentprovider.Codex})
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if reader.source != SourceOpenAIAPI || reader.currency != CurrencyUSD || reader.atMS != 200 ||
		got.ProviderContext.EffectiveProvider != agentprovider.Codex ||
		got.PricingVersion != "openai-api-test" || got.Basis != BasisStandard ||
		got.UnitTokens.Value == nil || *got.UnitTokens.Value != UnitTokens ||
		len(got.Items) != 2 || got.Items[0].ModelID != "model-a" || got.Items[1].ModelID != "model-b" {
		t.Fatalf("Current() = %#v, reader = %#v", got, reader)
	}
	if got.Items[0].InputMicros.Value == nil || *got.Items[0].InputMicros.Value != 0 {
		t.Fatalf("known zero became unknown: %#v", got.Items[0].InputMicros)
	}
	unknown := got.Items[1].CachedInputMicros
	if unknown.Value != nil || unknown.UnknownReason == nil ||
		*unknown.UnknownReason != basequery.UnknownUnavailable {
		t.Fatalf("unknown cached input = %#v", unknown)
	}
}

func TestCurrentFailsClosedForNonExactOrUnavailableCatalog(t *testing.T) {
	t.Parallel()

	for name, reader := range map[string]*readerStub{
		"non exact": {
			catalog: pricing.CatalogVersion{
				PricingVersion: "pattern", Source: SourceOpenAIAPI, Currency: CurrencyUSD,
				EffectiveFromMS: 100, Models: []pricing.ModelPrice{{
					MatchKind: pricing.ModelMatchPrefix, ModelPattern: "gpt-", Priority: 10,
					InputMicrosPerMillion: pointerTo(int64(1)),
				}},
			},
		},
		"read failure": {err: errors.New("database unavailable")},
		"untrusted source": {
			catalog: pricing.CatalogVersion{
				PricingVersion: "external", Source: SourceOpenAIAPI, Currency: CurrencyUSD,
				EffectiveFromMS: 100, SourceURL: "https://example.com/pricing",
				Models: []pricing.ModelPrice{{
					MatchKind: pricing.ModelMatchExact, ModelPattern: "gpt-5", Priority: 100,
					InputMicrosPerMillion: pointerTo(int64(1)),
				}},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			service, err := NewService(reader, func() time.Time { return time.UnixMilli(200) })
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Current(context.Background(), agentprovider.Scope{Provider: agentprovider.Codex}); !errors.Is(err, basequery.ErrUnavailable) {
				t.Fatalf("Current() error = %v, want ErrUnavailable", err)
			}
		})
	}
}

func TestCurrentReturnsCursorPublishedReferenceCatalog(t *testing.T) {
	t.Parallel()

	reader := &readerStub{err: errors.New("Codex catalog must not be read")}
	service, err := NewService(reader, func() time.Time { return time.UnixMilli(1_786_838_400_000) })
	if err != nil {
		t.Fatal(err)
	}

	got, err := service.Current(context.Background(), agentprovider.Scope{Provider: agentprovider.Cursor})
	if err != nil {
		t.Fatalf("Current(cursor) error = %v", err)
	}
	if reader.source != "" || got.ProviderContext.EffectiveProvider != agentprovider.Cursor ||
		got.Source != SourceCursorDocs || got.PricingVersion != pricing.CursorPricingVersion ||
		got.SourceURL == nil || *got.SourceURL != pricing.CursorPricingSourceURL || len(got.Items) != 15 {
		t.Fatalf("Current(cursor) = %#v, reader = %#v", got, reader)
	}

	items := make(map[string]ModelReferencePrice, len(got.Items))
	for _, item := range got.Items {
		items[item.ModelID] = item
	}
	composer := items["Composer 2.5"]
	if composer.InputMicros.Value == nil || *composer.InputMicros.Value != 500_000 ||
		composer.CachedInputMicros.Value == nil || *composer.CachedInputMicros.Value != 200_000 ||
		composer.OutputMicros.Value == nil || *composer.OutputMicros.Value != 2_500_000 ||
		composer.CacheWriteMicros.Value != nil {
		t.Fatalf("Composer 2.5 reference price = %#v", composer)
	}
	sonnet := items["Claude Sonnet 5"]
	if sonnet.CacheWriteMicros.Value == nil || *sonnet.CacheWriteMicros.Value != 2_500_000 {
		t.Fatalf("Claude Sonnet 5 cache write = %#v", sonnet.CacheWriteMicros)
	}
}

func TestCurrentRejectsUnknownProvider(t *testing.T) {
	t.Parallel()
	service, err := NewService(&readerStub{}, func() time.Time { return time.UnixMilli(200) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Current(context.Background(), agentprovider.Scope{Provider: "other"}); !errors.Is(err, basequery.ErrValidation) {
		t.Fatalf("Current(other) error = %v, want validation", err)
	}
}

type readerStub struct {
	catalog          pricing.CatalogVersion
	err              error
	source, currency string
	atMS             int64
}

func (stub *readerStub) PricingCatalogAt(
	_ context.Context,
	source string,
	currency string,
	atMS int64,
) (pricing.CatalogVersion, error) {
	stub.source, stub.currency, stub.atMS = source, currency, atMS
	return stub.catalog, stub.err
}

func pointerTo[T any](value T) *T {
	return &value
}
