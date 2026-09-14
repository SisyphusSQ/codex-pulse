package quota

import (
	"context"
	"errors"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/appserver"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

var (
	ErrInvalidClientConfig = errors.New("quota client config is invalid")
)

type AccountBindingFence struct {
	AccountScope      string
	BindingGeneration int64
}

type BoundRefreshRequest struct {
	RequestID string
	Binding   AccountBindingFence
}

type AccountRateLimitsReader interface {
	Read(context.Context, bool) (appserver.AccountRateLimitsSnapshot, error)
}

type RetryPolicy interface {
	Delay(int) (time.Duration, bool, error)
}

type ClientConfig struct {
	Reader      AccountRateLimitsReader
	ScopeKey    [32]byte
	Now         func() time.Time
	Timeout     time.Duration
	MaxAttempts int
	RetryPolicy RetryPolicy
	Wait        func(context.Context, time.Duration) error
}

type Failure struct {
	Code      store.SourceFailureCode
	RetryAtMS *int64
}

type Result struct {
	Observations  []store.QuotaObservationSample
	Failure       *Failure
	AttemptCount  int64
	HTTPStatus    *int64
	ResponseBytes int64
	PayloadSHA256 *store.SHA256Digest
	StartedAtMS   int64
	FinishedAtMS  int64
}
