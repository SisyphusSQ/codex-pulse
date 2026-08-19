package grokprovider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type staticTokenSource struct {
	token AccessToken
}

func (source staticTokenSource) ReadAccessToken() (AccessToken, error) {
	return source.token, nil
}

func TestBillingClientGetCreditsUsesOfficialEnvelope(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/billing" || request.URL.Query().Get("format") != "credits" {
			http.NotFound(writer, request)
			return
		}
		if request.Header.Get("Authorization") != "Bearer secret" || request.Header.Get("x-userid") != "user-1" {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"config": {
				"creditUsagePercent": 12,
				"currentPeriod": {
					"type": "USAGE_PERIOD_TYPE_WEEKLY",
					"start": "2026-08-10T00:00:00Z",
					"end": "2026-08-17T00:00:00Z"
				},
				"onDemandCap": {"val": 100},
				"onDemandUsed": {"val": 25}
			},
			"subscriptionTier": "SuperGrok"
		}`))
	}))
	t.Cleanup(server.Close)
	client, err := NewBillingClient(BillingClientConfig{
		BaseURL: server.URL, HTTPClient: server.Client(),
		TokenSource: staticTokenSource{token: AccessToken{Token: "secret", UserID: "user-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	credits, err := client.GetCredits(context.Background())
	if err != nil || credits.UsedPercent != 12 || credits.PeriodType != "weekly" ||
		credits.SubscriptionTier == nil || *credits.SubscriptionTier != "SuperGrok" {
		t.Fatalf("credits = %#v, %v", credits, err)
	}
}

func TestBillingClientGetCreditsReadsOfficialSnakeCaseSubscriptionTier(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/billing" || request.URL.Query().Get("format") != "credits" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"config": {
				"creditUsagePercent": 12,
				"currentPeriod": {
					"type": "USAGE_PERIOD_TYPE_WEEKLY",
					"start": "2026-08-10T00:00:00Z",
					"end": "2026-08-17T00:00:00Z"
				}
			},
			"subscription_tier": "SuperGrok Heavy"
		}`))
	}))
	t.Cleanup(server.Close)
	client, err := NewBillingClient(BillingClientConfig{
		BaseURL: server.URL, HTTPClient: server.Client(),
		TokenSource: staticTokenSource{token: AccessToken{Token: "secret", UserID: "user-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	credits, err := client.GetCredits(context.Background())
	if err != nil || credits.SubscriptionTier == nil || *credits.SubscriptionTier != "SuperGrok Heavy" {
		t.Fatalf("credits = %#v, %v", credits, err)
	}
}

func TestBillingClientFailClosedOnUnauthorized(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "nope", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	client, err := NewBillingClient(BillingClientConfig{
		BaseURL: server.URL, HTTPClient: server.Client(),
		TokenSource: staticTokenSource{token: AccessToken{Token: "secret", UserID: "user-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetCredits(context.Background()); !errors.Is(err, ErrAuthExpired) {
		t.Fatalf("unauthorized error = %v", err)
	}
}

func TestBillingCollectorRecordsOnDemandObservation(t *testing.T) {
	t.Parallel()
	used, capAmount := 25.0, 100.0
	writer := &billingSnapshotCapture{}
	collector, err := NewBillingCollector(staticCreditsClient{credits: BillingCredits{
		UsedPercent: 40, PeriodType: "weekly", PeriodStartMS: 1_000, PeriodEndMS: 8_000,
		OnDemandUsed: &used, OnDemandCap: &capAmount,
	}}, writer, BillingCollectorConfig{Now: func() time.Time { return time.UnixMilli(2_000) }})
	if err != nil {
		t.Fatal(err)
	}
	if err := collector.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if writer.snapshot.UsedPercent != 40 || len(writer.snapshot.QuotaObservations) != 2 ||
		writer.snapshot.QuotaObservations[1].LimitID != grokOnDemandLimitID ||
		writer.snapshot.QuotaObservations[1].UsedPercent != 25 {
		t.Fatalf("snapshot = %#v", writer.snapshot)
	}
}

type staticCreditsClient struct {
	credits BillingCredits
}

func (client staticCreditsClient) GetCredits(context.Context) (BillingCredits, error) {
	return client.credits, nil
}

type billingSnapshotCapture struct {
	snapshot store.GrokBillingSnapshot
}

func (capture *billingSnapshotCapture) CommitGrokBillingSnapshot(
	_ context.Context,
	snapshot store.GrokBillingSnapshot,
) error {
	capture.snapshot = snapshot
	return nil
}

func (capture *billingSnapshotCapture) RecordGrokBillingFailure(context.Context, int64, string) error {
	return nil
}
