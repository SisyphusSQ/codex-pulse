package appserver

import (
	"context"
	"errors"
	"io"
	"os/exec"
)

type ProcessOptions struct {
	CodexBinary string
	PageSize    int
	ClientName  string
	Version     string
}

func ListLocalThreads(ctx context.Context, confirmedHome string, options ProcessOptions) (ThreadList, error) {
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
	return NewThreadLister(rpc, ThreadListerOptions{PageSize: options.PageSize}).List(ctx, confirmedHome)
}
