package providercontrol

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	logsource "github.com/SisyphusSQ/codex-pulse/internal/codex/logs/source"
	"github.com/SisyphusSQ/codex-pulse/internal/preferences"
)

func DefaultProbes(_ PreferencesReader, defaultCodexHome func() string) ProbeSet {
	return ProbeSet{
		Codex: func(ctx context.Context, home *preferences.CodexHomePreferences) ProbeResult {
			if err := ctx.Err(); err != nil {
				return ProbeResult{State: DiscoveryInvalid, ReasonCode: ReasonProbeFailed}
			}
			if home != nil {
				return probeConfirmedCodexHome(ctx, home.Source)
			}
			path := ""
			if defaultCodexHome != nil {
				path = strings.TrimSpace(defaultCodexHome())
			}
			if path == "" {
				return ProbeResult{State: DiscoveryMissing, ReasonCode: ReasonNotFound}
			}
			return probeDirectory(path)
		},
		Cursor: func(ctx context.Context) ProbeResult {
			if err := ctx.Err(); err != nil {
				return ProbeResult{State: DiscoveryInvalid, ReasonCode: ReasonProbeFailed}
			}
			projectsRoot, stateDatabase, err := cursorProbePaths()
			if err != nil {
				return ProbeResult{State: DiscoveryInvalid, ReasonCode: ReasonProbeFailed}
			}
			return firstAvailable(probeDirectory(projectsRoot), probeFile(stateDatabase))
		},
		Grok: func(ctx context.Context) ProbeResult {
			if err := ctx.Err(); err != nil {
				return ProbeResult{State: DiscoveryInvalid, ReasonCode: ReasonProbeFailed}
			}
			homePath, sessionsRoot, err := grokProbePaths()
			if err != nil {
				return ProbeResult{State: DiscoveryInvalid, ReasonCode: ReasonProbeFailed}
			}
			home := probeDirectory(homePath)
			sessions := probeDirectory(sessionsRoot)
			if home.State == DiscoveryAvailable && sessions.State == DiscoveryAvailable {
				return ProbeResult{State: DiscoveryAvailable, ReasonCode: ReasonAvailable}
			}
			return worseProbe(home, sessions)
		},
	}
}

func probeConfirmedCodexHome(ctx context.Context, source preferences.ConfirmedSource) ProbeResult {
	if result := probeDirectory(source.Path); result.State != DiscoveryAvailable {
		return result
	}
	metadata, err := logsource.NewHomeProbe().ProbeIdentity(ctx, source.Path)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return ProbeResult{State: DiscoveryInaccessible, ReasonCode: ReasonPermissionDenied}
		}
		return ProbeResult{State: DiscoveryInvalid, ReasonCode: ReasonProbeFailed}
	}
	if metadata.Path != source.Path || metadata.DeviceID != source.DeviceID || metadata.Inode != source.Inode {
		return ProbeResult{State: DiscoveryInvalid, ReasonCode: ReasonUnsafePath}
	}
	return ProbeResult{State: DiscoveryAvailable, ReasonCode: ReasonAvailable}
}

func probeDirectory(path string) ProbeResult {
	return probePath(path, true)
}

func probeFile(path string) ProbeResult {
	return probePath(path, false)
}

func probePath(path string, directory bool) ProbeResult {
	if !safeAbsolutePath(path) {
		return ProbeResult{State: DiscoveryInvalid, ReasonCode: ReasonUnsafePath}
	}
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			return ProbeResult{State: DiscoveryInvalid, ReasonCode: ReasonUnsafePath}
		}
		if directory && !info.IsDir() {
			return ProbeResult{State: DiscoveryInvalid, ReasonCode: ReasonInvalidType}
		}
		if !directory && info.IsDir() {
			return ProbeResult{State: DiscoveryInvalid, ReasonCode: ReasonInvalidType}
		}
		return ProbeResult{State: DiscoveryAvailable, ReasonCode: ReasonAvailable}
	case errors.Is(err, fs.ErrNotExist):
		return ProbeResult{State: DiscoveryMissing, ReasonCode: ReasonNotFound}
	case errors.Is(err, fs.ErrPermission):
		return ProbeResult{State: DiscoveryInaccessible, ReasonCode: ReasonPermissionDenied}
	default:
		return ProbeResult{State: DiscoveryInvalid, ReasonCode: ReasonProbeFailed}
	}
}

func safeAbsolutePath(path string) bool {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	return true
}

func firstAvailable(values ...ProbeResult) ProbeResult {
	missing := 0
	for _, value := range values {
		if value.State == DiscoveryAvailable {
			return ProbeResult{State: DiscoveryAvailable, ReasonCode: ReasonAvailable}
		}
		if value.State == DiscoveryMissing {
			missing++
		}
	}
	if missing == len(values) {
		return ProbeResult{State: DiscoveryMissing, ReasonCode: ReasonNotFound}
	}
	for _, value := range values {
		if value.State == DiscoveryInaccessible {
			return value
		}
	}
	for _, value := range values {
		if value.State == DiscoveryInvalid {
			return value
		}
	}
	return ProbeResult{State: DiscoveryInvalid, ReasonCode: ReasonProbeFailed}
}

func worseProbe(left, right ProbeResult) ProbeResult {
	rank := map[DiscoveryState]int{
		DiscoveryAvailable:    0,
		DiscoveryUnchecked:    1,
		DiscoveryMissing:      2,
		DiscoveryInaccessible: 3,
		DiscoveryInvalid:      4,
	}
	if rank[right.State] > rank[left.State] {
		return right
	}
	return left
}

func cursorProbePaths() (projectsRoot, stateDatabase string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	if override := strings.TrimSpace(os.Getenv("CODEX_PULSE_CURSOR_HOME")); override != "" {
		if !filepath.IsAbs(override) {
			return "", "", errors.New("cursor home is invalid")
		}
		home = filepath.Clean(override)
	}
	return filepath.Join(home, ".cursor", "projects"),
		filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb"),
		nil
}

func grokProbePaths() (homePath, sessionsRoot string, err error) {
	if override := strings.TrimSpace(os.Getenv("CODEX_PULSE_GROK_HOME")); override != "" {
		if !filepath.IsAbs(override) {
			return "", "", errors.New("grok home is invalid")
		}
		homePath = filepath.Clean(override)
	} else if value := strings.TrimSpace(os.Getenv("GROK_HOME")); value != "" {
		if !filepath.IsAbs(value) {
			return "", "", errors.New("grok home is invalid")
		}
		homePath = filepath.Clean(value)
	} else {
		userHome, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return "", "", homeErr
		}
		homePath = filepath.Join(userHome, ".grok")
	}
	return homePath, filepath.Join(homePath, "sessions"), nil
}
