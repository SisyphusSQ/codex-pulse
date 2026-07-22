package appserver

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
)

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
	binary := options.CodexBinary
	if binary == "" {
		binary = "codex"
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
