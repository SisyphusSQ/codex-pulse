package utils

import (
	"time"

	"github.com/SisyphusSQ/codex-pulse/server/config"
)

func NewTimeoutContext(c config.Config) time.Duration {
	return c.ContextTimeout
}
