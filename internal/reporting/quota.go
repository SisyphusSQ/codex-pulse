package reporting

import (
	"context"
	"strconv"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type FactsGroup struct {
	Key   string
	Batch reportingv1.Batch
}
type ExportFactsPage struct {
	Partition string
	Groups    []FactsGroup
	Next      string
	Done      bool
}

func (e *Exporter) captureIdentities(ctx context.Context) error {
	if e.identities == nil {
		return nil
	}
	identities, err := e.identities.ReportingAccountIdentities(ctx)
	if err != nil {
		return err
	}
	return e.state.RememberIdentities(ctx, identities)
}
func (e *Exporter) FactsPartition(ctx context.Context, provider string) (string, error) {
	if err := e.captureIdentities(ctx); err != nil {
		return "", err
	}
	source, err := e.source(ctx, provider, 0)
	if err != nil {
		return "", err
	}
	revision := ""
	if provider == "codex" {
		revision, err = e.repository.ReportingQuotaPartition(ctx)
		if err != nil {
			return "", err
		}
	}
	identities, err := e.state.Identities(ctx)
	if err != nil {
		return "", err
	}
	relation := []string{}
	for _, identity := range identities {
		if identity.Provider == provider {
			relation = append(relation, identity.LocalScope, identity.AccountID)
		}
	}
	return e.state.HomeID("quota-partition", source.HomeID, revision, reportingv1.Key(relation...)), nil
}
func (e *Exporter) Facts(ctx context.Context, provider, after string, start int64) (ExportFactsPage, error) {
	return e.facts(ctx, provider, after, start, false)
}
func (e *Exporter) CurrentFacts(ctx context.Context, provider string, start int64) (ExportFactsPage, error) {
	return e.facts(ctx, provider, "", start, true)
}
func (e *Exporter) facts(ctx context.Context, provider, after string, start int64, current bool) (out ExportFactsPage, err error) {
	before, err := e.FactsPartition(ctx, provider)
	if err != nil {
		return out, err
	}
	identities, err := e.state.Identities(ctx)
	if err != nil {
		return out, err
	}
	known := map[string]AccountIdentity{}
	for _, identity := range identities {
		if identity.Provider == provider {
			known[identity.LocalScope] = identity
		}
	}
	var page store.ReportingQuotaPage
	if current {
		page, err = e.repository.ReportingCurrentQuotaPage(ctx, provider, start)
	} else {
		page, err = e.repository.ReportingQuotaPage(ctx, provider, after, start)
	}
	if err != nil {
		return out, err
	}
	out.Next = page.Next
	out.Partition = before
	out.Done = page.Done
	// 资料独立分组，不把其新采集时间塞进每条历史观测的幂等正文。
	for offset := 0; offset < len(identities); offset += 32 {
		batch := reportingv1.Batch{}
		for _, identity := range identities[offset:min(offset+32, len(identities))] {
			if identity.Provider != provider {
				continue
			}
			account := reportingv1.Account{Provider: provider, ID: identity.AccountID, Email: identity.Email, Plan: identity.Plan, CollectedAtMS: identity.CollectedAtMS}
			found := false
			for i, old := range batch.Accounts {
				if old.ID == account.ID {
					found = true
					if old.CollectedAtMS <= account.CollectedAtMS {
						batch.Accounts[i] = account
					}
					break
				}
			}
			if !found {
				batch.Accounts = append(batch.Accounts, account)
			}
			batch.Bindings = append(batch.Bindings, bindingForIdentity(e.state, identity))
		}
		if len(batch.Accounts) > 0 {
			out.Groups = append(out.Groups, FactsGroup{Key: e.state.HomeID("account-metadata", provider, strconv.Itoa(offset)), Batch: batch})
		}
	}
	for _, q := range page.Batch.Quotas {
		original := q.LocalScope
		q.ID = e.state.HomeID("quota-fact", provider, q.ID)
		q.LocalScope = e.state.PublicScope(provider, original)
		batch := reportingv1.Batch{}
		if original == "default" && provider == "codex" {
			if page.LinkedScope != nil {
				if identity, ok := known[*page.LinkedScope]; ok {
					q.AccountID = new(identity.AccountID)
					q.AssociationScope = new(e.state.PublicScope(provider, identity.LocalScope))
					q.HistoryOrigin = "linked_history"
					batch.Bindings = []reportingv1.AccountBinding{bindingForIdentity(e.state, identity)}
				}
			}
		} else if identity, ok := known[original]; ok {
			q.AccountID = new(identity.AccountID)
			q.HistoryOrigin = "confirmed"
			batch.Bindings = []reportingv1.AccountBinding{bindingForIdentity(e.state, identity)}
		} else if original == "default" {
			// Cursor/Grok 的现有观测没有可验证 raw ID；不同采集来源不自动合并。
			q.LocalScope = e.state.HomeID("unassigned-quota", provider)
		}
		batch.Quotas = []reportingv1.QuotaObservation{q}
		out.Groups = append(out.Groups, FactsGroup{Key: reportingv1.Key("quota", provider, q.ID), Batch: batch})
	}
	for _, c := range page.Batch.Credits {
		original := c.LocalScope
		c.ID = e.state.HomeID("credit-fact", provider, c.ID)
		c.LocalScope = e.state.PublicScope(provider, original)
		batch := reportingv1.Batch{}
		if identity, ok := known[original]; ok {
			c.AccountID = new(identity.AccountID)
			batch.Bindings = []reportingv1.AccountBinding{bindingForIdentity(e.state, identity)}
		}
		batch.Credits = []reportingv1.ResetCredits{c}
		out.Groups = append(out.Groups, FactsGroup{Key: reportingv1.Key("credits", provider, c.ID), Batch: batch})
	}
	later, err := e.FactsPartition(ctx, provider)
	if err != nil || later != before {
		return ExportFactsPage{}, store.ErrReportingSource
	}
	return
}
func bindingForIdentity(state *State, identity AccountIdentity) reportingv1.AccountBinding {
	return reportingv1.AccountBinding{Provider: identity.Provider, LocalScope: state.PublicScope(identity.Provider, identity.LocalScope), AccountID: identity.AccountID, ConfirmedAtMS: identity.ConfirmedAtMS}
}
