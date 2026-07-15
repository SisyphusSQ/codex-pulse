package quota

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

const WhamUsageEndpoint = "https://chatgpt.com/backend-api/wham/usage"

var (
	ErrCredentialUnavailable = errors.New("quota credential is unavailable")
	ErrInvalidClientConfig   = errors.New("quota client config is invalid")
)

type CredentialProvider interface {
	WithAccessToken(context.Context, func([]byte) error) error
}

type RetryPolicy interface {
	Delay(int) (time.Duration, bool, error)
}

type ClientConfig struct {
	Transport        http.RoundTripper
	Credentials      CredentialProvider
	Now              func() time.Time
	Timeout          time.Duration
	MaxAttempts      int
	MaxResponseBytes int64
	RetryPolicy      RetryPolicy
	Wait             func(context.Context, time.Duration) error
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
