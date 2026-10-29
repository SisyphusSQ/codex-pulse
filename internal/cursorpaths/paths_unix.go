//go:build !windows

package cursorpaths

import "path/filepath"

func editorDataDirectory(home string, _ bool) (string, error) {
	return filepath.Join(home, "Library", "Application Support", "Cursor"), nil
}
