package maintenance_srv

import (
	"context"
	"time"

	"go.uber.org/fx"

	"github.com/SisyphusSQ/codex-pulse/server/config"
	"github.com/SisyphusSQ/codex-pulse/server/internal/lib/log"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/quota_srv"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/reporting_srv"
)

// Maintenance 由中心生命周期托管；小时精简与摘要补建串行、有界、可取消。
type Maintenance struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func NewMaintenance(lifecycle fx.Lifecycle, cfg config.Config, quota *quota_srv.Quota, reporting *reporting_srv.Reporting) *Maintenance {
	m := &Maintenance{done: make(chan struct{})}
	lifecycle.Append(fx.Hook{OnStart: func(context.Context) error {
		ctx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel
		go func() {
			defer close(m.done)
			compact := func() {
				if !cfg.Server.QuotaMaintenance {
					return
				}
				c, cancel := context.WithTimeout(ctx, 2*time.Minute)
				defer cancel()
				n, err := quota.Compact(c)
				if err != nil && ctx.Err() == nil {
					log.Logger.Errorf("quota compaction failed: %T", err)
				} else if n > 0 {
					log.Logger.Infof("quota compaction retired %d observations", n)
				}
			}
			compact()
			hour := time.NewTicker(time.Hour)
			defer hour.Stop()
			warm := time.NewTicker(time.Second)
			defer warm.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-hour.C:
					compact()
				case <-warm.C:
					c, cancel := context.WithTimeout(ctx, 30*time.Second)
					processed, err := reporting.WarmCapsules(c, 100)
					cancel()
					if err == nil && processed == 0 {
						warm.Stop()
					}
					if err != nil && ctx.Err() == nil {
						log.Logger.Errorf("session capsule rebuild failed: %T", err)
					}
				}
			}
		}()
		return nil
	}, OnStop: func(ctx context.Context) error {
		if m.cancel != nil {
			m.cancel()
		}
		select {
		case <-m.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
	return m
}
