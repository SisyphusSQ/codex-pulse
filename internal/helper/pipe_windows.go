//go:build windows

package helper

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"

	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
)

const transportName = "grpc+namedpipe"
const pipePrefix = `\\.\pipe\codex-pulse-`

func logonSID() (string, error) {
	groups, err := windows.GetCurrentProcessToken().GetTokenGroups()
	if err != nil {
		return "", ErrRuntime
	}
	for _, group := range groups.AllGroups() {
		if group.Attributes&windows.SE_GROUP_LOGON_ID == windows.SE_GROUP_LOGON_ID {
			return group.Sid.String(), nil
		}
	}
	return "", ErrRuntime
}

func listenEndpoint(config RuntimeConfig) (net.Listener, func() error, error) {
	if !strings.HasPrefix(config.SocketPath, pipePrefix) || !filepath.IsAbs(config.RuntimeDirectory) {
		return nil, nil, ErrRuntime
	}
	name := strings.TrimPrefix(config.SocketPath, pipePrefix)
	if len(name) == 0 || len(name) > 96 {
		return nil, nil, ErrRuntime
	}
	for _, ch := range name {
		if !(ch >= 'a' && ch <= 'z') && !(ch >= '0' && ch <= '9') && ch != '-' {
			return nil, nil, ErrRuntime
		}
	}
	info, err := os.Stat(config.RuntimeDirectory)
	if err != nil || !platformfs.PrivateMode(config.RuntimeDirectory, info, 0o700) {
		return nil, nil, ErrRuntime
	}
	release, err := platformfs.PinDirectory(config.RuntimeDirectory)
	if err != nil {
		return nil, nil, ErrRuntime
	}
	sid, err := logonSID()
	if err != nil {
		release()
		return nil, nil, err
	}
	listener, err := winio.ListenPipe(config.SocketPath, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;" + sid + ")",
		InputBufferSize:    64 << 10, OutputBufferSize: 64 << 10,
	})
	if err != nil {
		release()
		return nil, nil, errors.Join(ErrRuntime, err)
	}
	var once sync.Once
	var closeErr error
	cleanup := func() error {
		once.Do(func() { closeErr = listener.Close(); release() })
		return closeErr
	}
	return listener, cleanup, nil
}
