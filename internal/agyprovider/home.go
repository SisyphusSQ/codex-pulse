package agyprovider

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	SourceSummaries    = "agy.summaries"
	SourceTrajectories = "agy.trajectories"
	SourceTranscripts  = "agy.transcripts"
	SourceLSRPC        = "agy.ls_rpc"
)

var (
	ErrCollector = errors.New("agy provider collector is unavailable")

	// ProductDirectories 覆盖 Antigravity 的 4 个固定产品子目录。
	ProductDirectories = []string{
		"antigravity",
		"antigravity-ide",
		"antigravity-cli",
		"antigravity-idea",
	}
)

// Config 控制 Antigravity 本地只读轨迹采集器的根目录与刷新节流。
type Config struct {
	BaseRoot       string
	MinimumRefresh time.Duration
	Now            func() time.Time
}

// DefaultConfig 按 CODEX_PULSE_AGY_HOME -> ANTIGRAVITY_GEMINI_DIR -> $HOME/.gemini 顺序解析根目录。
func DefaultConfig() (Config, error) {
	baseRoot, err := resolveBaseRoot()
	if err != nil {
		return Config{}, err
	}
	return Config{
		BaseRoot:       baseRoot,
		MinimumRefresh: 15 * time.Second,
		Now:            time.Now,
	}, nil
}

func resolveBaseRoot() (string, error) {
	for _, envKey := range []string{"CODEX_PULSE_AGY_HOME", "ANTIGRAVITY_GEMINI_DIR"} {
		if raw := strings.TrimSpace(os.Getenv(envKey)); raw != "" {
			cleaned := filepath.Clean(raw)
			if !filepath.IsAbs(cleaned) || cleaned != raw {
				return "", ErrCollector
			}
			return cleaned, nil
		}
	}
	userHome, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(userHome) == "" {
		return "", ErrCollector
	}
	return filepath.Join(userHome, ".gemini"), nil
}
