package lightindex

import (
	"context"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
)

type LocalMetadataProvider struct {
	Options appserver.ProcessOptions
}

func (provider LocalMetadataProvider) List(ctx context.Context, confirmedHome string) (appserver.ThreadList, error) {
	return appserver.ListLocalThreads(ctx, confirmedHome, provider.Options)
}

var _ MetadataProvider = LocalMetadataProvider{}
