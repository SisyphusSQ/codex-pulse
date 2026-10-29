// Package cursorpaths shares metadata-only source locations between discovery
// and collection. Resolving locations never opens an Agent file.
package cursorpaths

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var ErrPaths = errors.New("cursor source locations are unavailable")

// Locations are absolute source paths; resolving them does not read contents.
type Locations struct {
	ProjectsRoot         string
	StateDatabase        string
	ConversationDatabase string
	AITrackingDatabase   string
}

// Resolve honors the explicit profile override without falling through to the
// real profile's AppData. A custom/portable editor can supply its user-data root.
func Resolve() (Locations, error) {
	home := strings.TrimSpace(os.Getenv("CODEX_PULSE_CURSOR_HOME"))
	overridden := home != ""
	if !overridden {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return Locations{}, ErrPaths
		}
	}
	if !filepath.IsAbs(home) {
		return Locations{}, ErrPaths
	}
	home = filepath.Clean(home)
	data := strings.TrimSpace(os.Getenv("CODEX_PULSE_CURSOR_USER_DATA_DIR"))
	if data == "" {
		var err error
		data, err = editorDataDirectory(home, overridden)
		if err != nil {
			return Locations{}, ErrPaths
		}
	}
	if !filepath.IsAbs(data) {
		return Locations{}, ErrPaths
	}
	storage := filepath.Join(data, "User", "globalStorage")
	return Locations{
		ProjectsRoot:         filepath.Join(home, ".cursor", "projects"),
		StateDatabase:        filepath.Join(storage, "state.vscdb"),
		ConversationDatabase: filepath.Join(storage, "conversation-search.db"),
		AITrackingDatabase:   filepath.Join(home, ".cursor", "ai-tracking", "ai-code-tracking.db"),
	}, nil
}
