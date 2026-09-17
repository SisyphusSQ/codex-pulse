package lightindex

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
)

type LocalMetadataProvider struct {
	Options appserver.ProcessOptions
}

func (provider LocalMetadataProvider) List(ctx context.Context, confirmedHome string) (appserver.ThreadList, error) {
	options := provider.Options
	if os.Getenv("CODEX_PULSE_LIGHT_INDEX_PROFILE") == "1" {
		previous := options.OnExit
		options.OnExit = func(user, system time.Duration) {
			if previous != nil {
				previous(user, system)
			}
			log.Printf("light_index phase=appserver_child user_ms=%d system_ms=%d", user.Milliseconds(), system.Milliseconds())
		}
	}
	return appserver.ListLocalThreads(ctx, confirmedHome, options)
}

var _ MetadataProvider = LocalMetadataProvider{}
