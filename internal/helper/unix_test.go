package helper

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

// 测试 ListenUnix 在安全父目录中创建 0600 socket，并由 cleanup 删除。
func TestListenUnixCreatesPrivateSocketAndCleansIt(t *testing.T) {
	directory := shortSocketDirectory(t)
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "core.sock")
	listener, cleanup, err := ListenUnix(path)
	if err != nil {
		t.Fatalf("ListenUnix() error = %v", err)
	}
	if _, ok := listener.(*net.UnixListener); !ok {
		t.Fatalf("listener type = %T, want *net.UnixListener", listener)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("socket mode = %v, want socket 0600", info.Mode())
	}
	if err := cleanup(); err != nil {
		t.Fatalf("cleanup() error = %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("socket remains after cleanup: %v", err)
	}
}

// 测试 ListenUnix 拒绝 symlink 父目录和普通残留文件。
func TestListenUnixRejectsUnsafeParentAndResidualFile(t *testing.T) {
	root := shortSocketDirectory(t)
	realDirectory := filepath.Join(root, "real")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	linkDirectory := filepath.Join(root, "linked")
	if err := os.Symlink(realDirectory, linkDirectory); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ListenUnix(filepath.Join(linkDirectory, "core.sock")); err == nil {
		t.Fatal("ListenUnix accepted symlink parent")
	}

	unsafePath := filepath.Join(realDirectory, "core.sock")
	if err := os.WriteFile(unsafePath, []byte("not-a-socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ListenUnix(unsafePath); err == nil {
		t.Fatal("ListenUnix removed or replaced a residual regular file")
	}
}

func shortSocketDirectory(t testing.TB) string {
	t.Helper()
	directory, err := os.MkdirTemp("/private/tmp", "cp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return directory
}
