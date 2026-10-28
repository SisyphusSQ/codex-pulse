//go:build windows

package appserver

import (
	"context"
	"os/exec"
	"path/filepath"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/homeidentity"
	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
)

type windowsConfirmedHomeBinding struct {
	home ConfirmedHome
	root *platformfs.ReadRoot
}

func openConfirmedHomeBinding(ctx context.Context, home ConfirmedHome) (processHomeBinding, error) {
	if ctx == nil || home.Generation < 0 || !filepath.IsAbs(home.Path) || filepath.Clean(home.Path) != home.Path ||
		filepath.Dir(home.Path) == home.Path || home.DeviceID == "" || home.Inode <= 0 {
		return nil, ErrConfirmedHomeChanged
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := platformfs.OpenReadRoot(home.Path)
	if err != nil {
		return nil, ErrConfirmedHomeChanged
	}
	binding := &windowsConfirmedHomeBinding{home: home, root: root}
	if err := binding.validate(ctx); err != nil {
		root.Close()
		return nil, err
	}
	return binding, nil
}

func (binding *windowsConfirmedHomeBinding) canonicalPath() string { return binding.home.Path }
func (binding *windowsConfirmedHomeBinding) attach(command *exec.Cmd) (string, error) {
	if binding == nil || binding.root == nil || command == nil {
		return "", ErrConfirmedHomeChanged
	}
	command.Dir = binding.home.Path
	return binding.home.Path, nil
}
func (binding *windowsConfirmedHomeBinding) validate(ctx context.Context) error {
	if ctx == nil || binding == nil || binding.root == nil || binding.root.File == nil {
		return ErrConfirmedHomeChanged
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	identity, err := homeidentity.FromDescriptor(int(binding.root.Fd()))
	if err != nil || identity.DeviceID != binding.home.DeviceID || identity.Inode != binding.home.Inode {
		return ErrConfirmedHomeChanged
	}
	return nil
}
func (binding *windowsConfirmedHomeBinding) close() error {
	if binding == nil || binding.root == nil {
		return nil
	}
	err := binding.root.Close()
	binding.root = nil
	return err
}

func bindProcessHome(ctx context.Context, path string) (processHomeBinding, error) {
	root, err := platformfs.OpenReadRoot(path)
	if err != nil {
		return nil, ErrConfirmedHomeChanged
	}
	identity, err := homeidentity.FromDescriptor(int(root.Fd()))
	root.Close()
	if err != nil {
		return nil, ErrConfirmedHomeChanged
	}
	return openConfirmedHomeBinding(ctx, ConfirmedHome{Path: path, DeviceID: identity.DeviceID, Inode: identity.Inode})
}
