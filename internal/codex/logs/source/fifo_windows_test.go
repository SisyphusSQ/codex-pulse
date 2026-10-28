//go:build windows

package source

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func makeFIFOFixture(t *testing.T, _ string, _ uint32) error {
	t.Helper()
	t.Skip("Windows has no POSIX filesystem FIFO; Windows non-regular/reparse coverage uses native fixtures")
	return nil
}

func restrictFixtureContentRead(t *testing.T, path string) error {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sid := user.User.Sid.String()
	sd, err := windows.SecurityDescriptorFromString("D:P(D;;0x1;;;" + sid + ")(A;;FA;;;" + sid + ")(A;;FA;;;SY)")
	if err != nil {
		return err
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		return err
	}
	if _, err := os.ReadFile(path); err == nil {
		t.Fatal("content-read denial fixture is ineffective")
	}
	return nil
}
