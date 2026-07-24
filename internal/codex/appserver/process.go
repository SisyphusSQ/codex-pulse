package appserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrCodexBinaryUnavailable 表示当前环境没有可执行的 Codex CLI。
var ErrCodexBinaryUnavailable = errors.New("Codex binary unavailable")

type ProcessOptions struct {
	CodexBinary string
	PageSize    int
	ClientName  string
	Version     string
}

func ListLocalThreads(ctx context.Context, confirmedHome string, options ProcessOptions) (ThreadList, error) {
	canonicalHome, err := canonicalConfirmedHome(confirmedHome)
	if err != nil {
		return ThreadList{}, err
	}
	binary, err := resolveCodexBinary(options.CodexBinary, defaultCodexBinaryCandidates())
	if err != nil {
		return ThreadList{}, err
	}
	clientName := options.ClientName
	if clientName == "" {
		clientName = "codex-pulse"
	}
	version := options.Version
	if version == "" {
		version = "development"
	}

	processContext, cancelProcess := context.WithCancel(ctx)
	defer cancelProcess()
	command := exec.CommandContext(processContext, binary, "app-server", "--listen", "stdio://")
	command.Env = isolatedCodexEnvironment(os.Environ(), canonicalHome)
	stdin, err := command.StdinPipe()
	if err != nil {
		return ThreadList{}, errors.New("open App Server stdin")
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return ThreadList{}, errors.New("open App Server stdout")
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return ThreadList{}, errors.New("start Codex App Server")
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer func() {
		cancelProcess()
		_ = stdin.Close()
		<-done
	}()

	rpc := newJSONLineRPC(stdin, stdout)
	var initializeResult struct{}
	if err := rpc.Call(ctx, "initialize", struct {
		ClientInfo struct {
			Name    string `json:"name"`
			Title   string `json:"title"`
			Version string `json:"version"`
		} `json:"clientInfo"`
	}{ClientInfo: struct {
		Name    string `json:"name"`
		Title   string `json:"title"`
		Version string `json:"version"`
	}{Name: clientName, Title: "Codex Pulse", Version: version}}, &initializeResult); err != nil {
		return ThreadList{}, err
	}
	if err := rpc.Notify(ctx, "initialized", struct{}{}); err != nil {
		return ThreadList{}, err
	}
	return NewThreadLister(rpc, ThreadListerOptions{PageSize: options.PageSize}).List(ctx, canonicalHome)
}

func resolveCodexBinary(explicit string, fallbacks []string) (string, error) {
	if explicit != "" {
		path, err := executablePath(explicit)
		if err != nil {
			return "", fmt.Errorf("%w: configured executable", ErrCodexBinaryUnavailable)
		}
		return path, nil
	}
	if path, err := executablePath("codex"); err == nil {
		return path, nil
	}
	for _, candidate := range fallbacks {
		if candidate == "" {
			continue
		}
		if path, err := executablePath(candidate); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("%w: searched PATH and known installation locations", ErrCodexBinaryUnavailable)
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
		"/opt/homebrew/bin/codex",
		"/usr/local/bin/codex",
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return candidates
	}
	candidates = append(candidates,
		filepath.Join(home, "Applications", "ChatGPT.app", "Contents", "Resources", "codex"),
		filepath.Join(home, "Applications", "Codex.app", "Contents", "Resources", "codex"),
		filepath.Join(home, ".codex", "plugins", ".plugin-appserver", "codex"),
	)
	nvmCandidates, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin", "codex"))
	for index := len(nvmCandidates) - 1; index >= 0; index-- {
		candidates = append(candidates, nvmCandidates[index])
	}
	return candidates
}

func isolatedCodexEnvironment(environment []string, confirmedHome string) []string {
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if strings.HasPrefix(entry, "CODEX_HOME=") {
			continue
		}
		result = append(result, entry)
	}
	return append(result, "CODEX_HOME="+confirmedHome)
}
