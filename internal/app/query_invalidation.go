package app

import (
	"context"

	"github.com/SisyphusSQ/codex-pulse/internal/core"
)

type queryInvalidationNotifier interface {
	Notify(context.Context, core.InvalidationDomain) error
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
