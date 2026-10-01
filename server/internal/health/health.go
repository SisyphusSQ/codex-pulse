package health

import (
	"context"
	"fmt"
	"go.uber.org/fx"
	"sync/atomic"
)

// Check 只检查当前启用且提供请求服务必需的组件。
type Check struct {
	Name    string
	Enabled bool
	Ping    func(context.Context) error
}
type Params struct {
	fx.In
	Checks []Check `group:"readiness"`
}
type Registry struct {
	ready  atomic.Bool
	checks []Check
}

func New(p Params) *Registry            { return &Registry{checks: p.Checks} }
func (r *Registry) SetReady(value bool) { r.ready.Store(value) }
func (r *Registry) Ready(ctx context.Context) error {
	if !r.ready.Load() {
		return fmt.Errorf("service is not ready")
	}
	for _, check := range r.checks {
		if !check.Enabled {
			continue
		}
		if err := check.Ping(ctx); err != nil {
			return fmt.Errorf("%s unavailable: %w", check.Name, err)
		}
	}
	return nil
}
