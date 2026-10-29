//go:build windows

package cursorpaths

import (
	"os"
	"path/filepath"
)

func editorDataDirectory(home string, overridden bool) (string, error) {
	if overridden {
		return filepath.Join(home, "AppData", "Roaming", "Cursor"), nil
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(config, "Cursor"), nil
}
