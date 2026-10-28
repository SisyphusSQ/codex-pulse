//go:build !windows

package appserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func prepareCodexInvocations(path, nodeOverride string) ([]codexInvocation, error) {
	invocation := codexInvocation{binary: path}
	if !usesEnvNode(path) {
		return []codexInvocation{invocation}, nil
	}
	var invocations []codexInvocation
	seen := make(map[string]bool)
	for _, candidate := range nodeCandidates(path, nodeOverride) {
		node, err := executablePath(candidate)
		if err == nil && !seen[node] {
			seen[node] = true
			invocations = append(invocations, codexInvocation{binary: path, node: node})
		}
	}
	if len(invocations) == 0 {
		return nil, ErrNodeRuntimeUnavailable
	}
	return invocations, nil
}

func usesEnvNode(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	var header [256]byte
	read, _ := file.Read(header[:])
	line, _, _ := strings.Cut(string(header[:read]), "\n")
	if !strings.HasPrefix(line, "#!/usr/bin/env") {
		return false
	}
	fields := strings.Fields(strings.TrimPrefix(line, "#!/usr/bin/env"))
	return len(fields) > 0 && (fields[0] == "node" ||
		(fields[0] == "-S" && len(fields) > 1 && fields[1] == "node"))
}

func nodeCandidates(binary, override string) []string {
	if override != "" {
		if filepath.IsAbs(override) {
			return []string{override}
		}
		return nil
	}
	var candidates []string
	for _, directory := range strings.Split(os.Getenv("PATH"), string(os.PathListSeparator)) {
		if directory != "" {
			candidates = append(candidates, filepath.Join(directory, "node"))
		}
	}
	candidates = append(candidates, filepath.Join(filepath.Dir(binary), "node"))
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return append(candidates, "/opt/homebrew/bin/node", "/usr/local/bin/node", "/opt/local/bin/node")
	}
	candidates = append(candidates,
		filepath.Join(home, ".volta", "bin", "node"),
		filepath.Join(home, ".asdf", "shims", "node"),
		filepath.Join(home, ".local", "share", "mise", "shims", "node"),
	)
	for _, pattern := range []string{
		filepath.Join(home, ".nvm", "versions", "node", "*", "bin", "node"),
		filepath.Join(home, ".fnm", "node-versions", "*", "installation", "bin", "node"),
		filepath.Join(home, ".local", "share", "fnm", "node-versions", "*", "installation", "bin", "node"),
	} {
		matches, _ := filepath.Glob(pattern)
		for index := len(matches) - 1; index >= 0; index-- {
			candidates = append(candidates, matches[index])
		}
	}
	return append(candidates, "/opt/homebrew/bin/node", "/usr/local/bin/node", "/opt/local/bin/node")
}

func executablePath(candidate string) (string, error) {
	path, err := exec.LookPath(candidate)
	if err != nil {
		return "", err
	}
	return filepath.Abs(path)
}

func defaultCodexBinaryCandidates() []string {
	candidates := []string{
		"/Applications/ChatGPT.app/Contents/Resources/codex",
		"/Applications/Codex.app/Contents/Resources/codex",
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		candidates = append(candidates,
			filepath.Join(home, "Applications", "ChatGPT.app", "Contents", "Resources", "codex"),
			filepath.Join(home, "Applications", "Codex.app", "Contents", "Resources", "codex"),
			filepath.Join(home, ".local", "bin", "codex"),
		)
	}
	candidates = append(candidates,
		"/opt/homebrew/bin/codex",
		"/usr/local/bin/codex",
	)
	if err != nil || home == "" {
		return candidates
	}
	candidates = append(candidates,
		filepath.Join(home, ".codex", "plugins", ".plugin-appserver", "codex"),
	)
	nvmCandidates, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin", "codex"))
	for index := len(nvmCandidates) - 1; index >= 0; index-- {
		candidates = append(candidates, nvmCandidates[index])
	}
	return candidates
}

func environmentKeyEqual(a, b string) bool  { return a == b }
func executablePathsEqual(a, b string) bool { return a == b }
