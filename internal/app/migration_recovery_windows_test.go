package app

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
)

// Windows 验证实际 DACL；Unix chmod 不能建立或证明该安全边界。
func TestWindowsMigrationRecoveryRejectsPublicBackupACL(t *testing.T) {
	config := migrationRecoveryTestConfig(t)
	directory := filepath.Join(filepath.Dir(config.Path), "backups")
	if err := platformfs.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "fixture.db")
	if err := os.WriteFile(path, []byte("synthetic backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	controller := migrationRecoveryTestController(t, config)
	if len(controller.Snapshot().Backups) != 1 {
		t.Fatal("private backup was not listed")
	}
	public, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := public.DACL()
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{path, directory} {
		if err := windows.SetNamedSecurityInfo(target, windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
			nil, nil, acl, nil); err != nil {
			t.Fatal(err)
		}
		if len(controller.Snapshot().Backups) != 0 {
			t.Fatal("public backup ACL was accepted")
		}
		mode := os.FileMode(0o600)
		if target == directory {
			mode = 0o700
		}
		if err := platformfs.Secure(target, mode); err != nil {
			t.Fatal(err)
		}
	}
}
