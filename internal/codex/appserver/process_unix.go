//go:build !windows

package appserver

import (
	"context"
	"os/exec"
)

func startCodexProcess(command *exec.Cmd) (func(), error) {
	if err := command.Start(); err != nil {
		return nil, err
	}
	return func() {}, nil
}
func bindProcessHome(context.Context, string) (processHomeBinding, error) { return nil, nil }
