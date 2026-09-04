package app

import (
	"context"

	"github.com/SisyphusSQ/codex-pulse/internal/core"
)

type queryInvalidationNotifier interface {
	Notify(context.Context, core.InvalidationDomain) error
}

type dashboardCacheInvalidator interface {
	InvalidateUsage()
	InvalidateUsageAndQuota()
}

type dashboardAwareInvalidation struct {
	inner   queryInvalidationNotifier
	summary dashboardCacheInvalidator
}

func (notifier *dashboardAwareInvalidation) Notify(
	ctx context.Context,
	domain core.InvalidationDomain,
) error {
	if notifier != nil && notifier.summary != nil {
		switch domain {
		case core.InvalidationIndex:
			notifier.summary.InvalidateUsage()
		case core.InvalidationQuota:
			// Cursor Dashboard refreshes both quota and usage/cost facts while
			// publishing the shared quota invalidation domain.
			notifier.summary.InvalidateUsageAndQuota()
		}
	}
	if notifier == nil || notifier.inner == nil {
		return nil
	}
	return notifier.inner.Notify(ctx, domain)
}

func notifyQueryInvalidation(
	notifier queryInvalidationNotifier,
	ctx context.Context,
	domain core.InvalidationDomain,
) {
	if notifier == nil || ctx == nil {
		return
	}
	defer func() { _ = recover() }()
	_ = notifier.Notify(ctx, domain)
}
