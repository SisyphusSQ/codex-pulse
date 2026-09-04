package app

import (
	"context"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/core"
)

func TestDashboardAwareInvalidationRefreshesAllFactsChangedByQuotaDomain(t *testing.T) {
	t.Parallel()

	cache := &recordingDashboardInvalidator{}
	inner := &recordingInvalidationNotifier{}
	notifier := &dashboardAwareInvalidation{inner: inner, summary: cache}
	if err := notifier.Notify(context.Background(), core.InvalidationQuota); err != nil {
		t.Fatalf("Notify(quota) error = %v", err)
	}
	if cache.combined != 1 || cache.usage != 0 || inner.quota != 1 {
		t.Fatalf(
			"quota invalidation = cache(%d, combined %d), inner=%d",
			cache.usage, cache.combined, inner.quota,
		)
	}
	if err := notifier.Notify(context.Background(), core.InvalidationIndex); err != nil {
		t.Fatalf("Notify(index) error = %v", err)
	}
	if cache.combined != 1 || cache.usage != 1 || inner.index != 1 {
		t.Fatalf(
			"index invalidation = cache(%d, combined %d), inner=%d",
			cache.usage, cache.combined, inner.index,
		)
	}
}

type recordingDashboardInvalidator struct {
	usage    int
	combined int
}

func (invalidator *recordingDashboardInvalidator) InvalidateUsage() { invalidator.usage++ }
func (invalidator *recordingDashboardInvalidator) InvalidateUsageAndQuota() {
	invalidator.combined++
}

type recordingInvalidationNotifier struct {
	index int
	quota int
}

func (notifier *recordingInvalidationNotifier) Notify(
	_ context.Context,
	domain core.InvalidationDomain,
) error {
	switch domain {
	case core.InvalidationIndex:
		notifier.index++
	case core.InvalidationQuota:
		notifier.quota++
	}
	return nil
}
