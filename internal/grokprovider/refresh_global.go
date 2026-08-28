package grokprovider

import (
	"context"
)

type classifiedBillingRefresher interface {
	RefreshClassified(context.Context, bool) BillingRefreshClass
}

type GlobalRefreshReport struct {
	Local   LocalRefreshClass
	Billing BillingRefreshClass
}

type LocalRefreshClass struct {
	Attempted bool
	Err       error
}

func (service *QueryService) RefreshForGlobal(ctx context.Context, interactive bool) GlobalRefreshReport {
	if service == nil || service.collector == nil || ctx == nil {
		return GlobalRefreshReport{
			Local:   LocalRefreshClass{Err: ErrCollector},
			Billing: BillingRefreshClass{Err: ErrCollector},
		}
	}
	if _, ok := service.reader.(emptySnapshotReader); ok {
		return GlobalRefreshReport{
			Local:   LocalRefreshClass{Err: ErrCollector},
			Billing: BillingRefreshClass{Err: ErrCollector},
		}
	}
	report := GlobalRefreshReport{}
	performed, err := service.collector.RefreshIfDue(ctx)
	report.Local = LocalRefreshClass{Attempted: performed, Err: err}
	if service.billing == nil {
		report.Billing = BillingRefreshClass{Err: ErrCollector}
	} else if classified, ok := service.billing.(classifiedBillingRefresher); ok {
		report.Billing = classified.RefreshClassified(ctx, interactive)
	} else if conditional, ok := service.billing.(conditionalRefresher); ok && !interactive {
		performed, err := conditional.RefreshIfDue(ctx)
		report.Billing = BillingRefreshClass{Attempted: performed, Remote: performed || err != nil, Err: err}
	} else {
		err := service.billing.Refresh(ctx)
		report.Billing = classifyGrokBillingError(err)
	}
	if (report.Local.Attempted && report.Local.Err == nil) ||
		(report.Billing.Attempted && report.Billing.Err == nil) {
		service.invalidateSnapshot()
	}
	return report
}
