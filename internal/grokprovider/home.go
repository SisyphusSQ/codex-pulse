package grokprovider

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrCollector = errors.New("grok provider collector is unavailable")

const (
	SourceSummary       = "grok.summary"
	SourceUpdates       = "grok.updates"
	SourceSessionSearch = "grok.session_search"
	SourceBilling       = "grok.billing"

	envTestHome = "CODEX_PULSE_GROK_HOME"
	envHome     = "GROK_HOME"

	DefaultBillingBaseURL = "https://cli-chat-proxy.grok.com/v1"
	envBillingBaseURL     = "GROK_CLI_CHAT_PROXY_BASE_URL"
)

type Config struct {
	Home           string
	SessionsRoot   string
	AuthPath       string
	BillingBaseURL string
	MinimumRefresh time.Duration
	Now            func() time.Time
}

func DefaultConfig() (Config, error) {
	home, err := resolveHome()
	if err != nil {
		return Config{}, err
	}
	billing := strings.TrimSpace(os.Getenv(envBillingBaseURL))
	if billing == "" {
		billing = DefaultBillingBaseURL
	}
	return Config{
		Home:           home,
		SessionsRoot:   filepath.Join(home, "sessions"),
		AuthPath:       filepath.Join(home, "auth.json"),
		BillingBaseURL: billing,
		MinimumRefresh: 15 * time.Second,
		Now:            time.Now,
	}, nil
}

func resolveHome() (string, error) {
	if override := strings.TrimSpace(os.Getenv(envTestHome)); override != "" {
		if !filepath.IsAbs(override) {
			return "", ErrCollector
		}
		return filepath.Clean(override), nil
	}
	if value := strings.TrimSpace(os.Getenv(envHome)); value != "" {
		if !filepath.IsAbs(value) {
			return "", ErrCollector
		}
		return filepath.Clean(value), nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(userHome, ".grok"), nil
}
