package reporting

import (
	"context"
	"time"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/creditbalance"
)

func (e *Exporter) PaidCreditFacts(ctx context.Context, start int64) ([]FactsGroup, error) {
	before, err := e.FactsPartition(ctx, "codex")
	if err != nil {
		return nil, err
	}
	identities, err := e.state.Identities(ctx)
	if err != nil {
		return nil, err
	}
	known := map[string]AccountIdentity{}
	for _, identity := range identities {
		if identity.Provider == "codex" {
			known[identity.LocalScope] = identity
		}
	}
	facts, err := e.repository.ReportingPaidCredits(ctx, max(start, time.Now().UnixMilli()-30*creditbalance.DayMS))
	if err != nil {
		return nil, err
	}
	groups := []FactsGroup{}
	for _, c := range facts {
		identity, ok := known[c.LocalScope]
		if !ok {
			continue
		}
		c.ID = e.state.HomeID("paid-credit-fact", "codex", c.ID)
		c.LocalScope = e.state.PublicScope("codex", c.LocalScope)
		c.AccountID = new(identity.AccountID)
		groups = append(groups, FactsGroup{Key: reportingv1.Key("paid-credit", c.ID), Batch: reportingv1.Batch{PaidCredits: []reportingv1.PaidCredit{c}, Bindings: []reportingv1.AccountBinding{bindingForIdentity(e.state, identity)}}})
	}
	later, err := e.FactsPartition(ctx, "codex")
	if err != nil || later != before {
		return nil, ErrUnavailable
	}
	return groups, nil
}

func (c *Client) supportsPaidCredits(ctx context.Context, credential string) (bool, error) {
	var out struct {
		PaidCredits bool `json:"paid_credits"`
	}
	err := c.requestMethod(ctx, "GET", "/api/v1/sync", credential, nil, &out)
	return out.PaidCredits, err
}
