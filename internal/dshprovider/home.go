package dshprovider

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrCollector = errors.New("dsh local collector unavailable")

const (
	SourceSummary = "dsh.header"
	SourceUpdates = "dsh.logs"
)

type Config struct {
	Home           string
	SessionsRoot   string
	MinimumRefresh time.Duration
	Now            func() time.Time
}

func DefaultConfig() (Config, error) {
	home := strings.TrimSpace(os.Getenv("CODEX_PULSE_DSH_HOME"))
	if home == "" {
		home = strings.TrimSpace(os.Getenv("DSH_HOME"))
	}
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return Config{}, ErrCollector
		}
		home = filepath.Join(userHome, ".dsh")
	}
	if !filepath.IsAbs(home) {
		return Config{}, ErrCollector
	}
	root := strings.TrimSpace(os.Getenv("CODEX_PULSE_DSH_SESSIONS_ROOT"))
	if root == "" {
		root = filepath.Join(home, "sessions")
	}
	if !filepath.IsAbs(root) {
		return Config{}, ErrCollector
	}
	return Config{Home: filepath.Clean(home), SessionsRoot: filepath.Clean(root), MinimumRefresh: 15 * time.Second, Now: time.Now}, nil
}
