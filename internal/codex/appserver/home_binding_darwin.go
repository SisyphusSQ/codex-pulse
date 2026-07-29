//go:build darwin

package appserver

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/homeidentity"
)

type darwinConfirmedHomeBinding struct {
	home   ConfirmedHome
	root   *os.File
	kqueue int
}

const (
	accountHomeLauncherArgument    = "--codex-pulse-account-home-launcher"
	accountHomeLauncherEnvironment = "CODEX_PULSE_ACCOUNT_HOME_LAUNCHER"
)

// 启动器先切换到继承的 Home 描述符，再执行 App Server，避免路径替换改变访问目标。
func init() {
	if os.Getenv(accountHomeLauncherEnvironment) != "1" {
		return
	}
	if len(os.Args) < 4 || os.Args[1] != accountHomeLauncherArgument {
		os.Exit(125)
	}
	descriptor, err := strconv.Atoi(os.Args[2])
	if err != nil || descriptor < 3 || unix.Fchdir(descriptor) != nil {
		os.Exit(125)
	}
	unix.CloseOnExec(descriptor)
	target := os.Args[3]
	targetArguments := append([]string{target}, os.Args[4:]...)
	environment := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, accountHomeLauncherEnvironment+"=") {
			continue
		}
		environment = append(environment, entry)
	}
	if unix.Exec(target, targetArguments, environment) != nil {
		os.Exit(125)
	}
}

func openConfirmedHomeBinding(
	ctx context.Context,
	home ConfirmedHome,
) (processHomeBinding, error) {
	if ctx == nil || home.Generation < 0 || !filepath.IsAbs(home.Path) ||
		filepath.Clean(home.Path) != home.Path || home.Path == string(filepath.Separator) ||
		home.DeviceID == "" || home.Inode <= 0 {
		return nil, ErrConfirmedHomeChanged
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(home.Path)
	if err != nil || resolved != home.Path {
		return nil, ErrConfirmedHomeChanged
	}
	descriptor, err := openAccountHomeNoFollow(home.Path)
	if err != nil {
		return nil, ErrConfirmedHomeChanged
	}
	root := os.NewFile(uintptr(descriptor), home.Path)
	if root == nil {
		_ = unix.Close(descriptor)
		return nil, ErrConfirmedHomeChanged
	}
	binding := &darwinConfirmedHomeBinding{home: home, root: root, kqueue: -1}
	if err := binding.validateDescriptor(); err != nil {
		_ = root.Close()
		return nil, err
	}
	binding.kqueue, err = watchAccountHomeNamespace(descriptor)
	if err != nil {
		_ = root.Close()
		return nil, ErrConfirmedHomeChanged
	}
	if err := binding.validate(ctx); err != nil {
		_ = binding.close()
		return nil, err
	}
	return binding, nil
}

func (binding *darwinConfirmedHomeBinding) canonicalPath() string {
	if binding == nil {
		return ""
	}
	return binding.home.Path
}

func (binding *darwinConfirmedHomeBinding) attach(command *exec.Cmd) (string, error) {
	if binding == nil || binding.root == nil || command == nil {
		return "", ErrConfirmedHomeChanged
	}
	launcher, err := os.Executable()
	if err != nil {
		return "", ErrConfirmedHomeChanged
	}
	target := command.Path
	targetArguments := append([]string(nil), command.Args[1:]...)
	descriptor := 3 + len(command.ExtraFiles)
	command.ExtraFiles = append(command.ExtraFiles, binding.root)
	command.Path = launcher
	command.Args = append(
		[]string{
			launcher,
			accountHomeLauncherArgument,
			strconv.Itoa(descriptor),
			target,
		},
		targetArguments...,
	)
	command.Env = append(command.Env, accountHomeLauncherEnvironment+"=1")
	return ".", nil
}

func (binding *darwinConfirmedHomeBinding) validate(ctx context.Context) error {
	if binding == nil || binding.root == nil || binding.kqueue < 0 {
		return ErrConfirmedHomeChanged
	}
	if ctx == nil {
		return ErrConfirmedHomeChanged
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := binding.validateDescriptor(); err != nil {
		return err
	}
	events := make([]unix.Kevent_t, 1)
	timeout := unix.Timespec{}
	count, err := unix.Kevent(binding.kqueue, nil, events, &timeout)
	if err != nil || count != 0 {
		return ErrConfirmedHomeChanged
	}
	return nil
}

func (binding *darwinConfirmedHomeBinding) validateDescriptor() error {
	if binding == nil || binding.root == nil {
		return ErrConfirmedHomeChanged
	}
	identity, err := homeidentity.FromDescriptor(int(binding.root.Fd()))
	if err != nil || identity.DeviceID != binding.home.DeviceID ||
		identity.Inode != binding.home.Inode {
		return ErrConfirmedHomeChanged
	}
	return nil
}

func (binding *darwinConfirmedHomeBinding) close() error {
	if binding == nil {
		return nil
	}
	var closeErr error
	if binding.kqueue >= 0 {
		closeErr = unix.Close(binding.kqueue)
		binding.kqueue = -1
	}
	if binding.root != nil {
		closeErr = errors.Join(closeErr, binding.root.Close())
		binding.root = nil
	}
	return closeErr
}

func openAccountHomeNoFollow(path string) (int, error) {
	current, err := unix.Open(
		string(filepath.Separator),
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY,
		0,
	)
	if err != nil {
		return -1, err
	}
	components := strings.Split(
		strings.TrimPrefix(path, string(filepath.Separator)),
		string(filepath.Separator),
	)
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			_ = unix.Close(current)
			return -1, ErrConfirmedHomeChanged
		}
		next, openErr := unix.Openat(
			current,
			component,
			unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY,
			0,
		)
		_ = unix.Close(current)
		if openErr != nil {
			return -1, openErr
		}
		current = next
	}
	return current, nil
}

func watchAccountHomeNamespace(descriptor int) (int, error) {
	kqueue, err := unix.Kqueue()
	if err != nil {
		return -1, err
	}
	unix.CloseOnExec(kqueue)
	change := unix.Kevent_t{}
	unix.SetKevent(
		&change,
		descriptor,
		unix.EVFILT_VNODE,
		unix.EV_ADD|unix.EV_CLEAR,
	)
	change.Fflags = unix.NOTE_DELETE | unix.NOTE_RENAME | unix.NOTE_REVOKE
	if _, err := unix.Kevent(kqueue, []unix.Kevent_t{change}, nil, nil); err != nil {
		_ = unix.Close(kqueue)
		return -1, err
	}
	return kqueue, nil
}
