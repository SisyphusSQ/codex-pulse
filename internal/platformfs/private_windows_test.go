//go:build windows

package platformfs

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func privateTestDirectory(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "私有 runtime")
	if err := MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWindowsPrivateACLAndHardLinks(t *testing.T) {
	root := privateTestDirectory(t)
	info, _ := os.Stat(root)
	if !PrivateMode(root, info, 0o700) {
		t.Fatal("private directory ACL rejected")
	}
	path := filepath.Join(root, "凭据.db")
	file, err := OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("synthetic secret"); err != nil {
		t.Fatal(err)
	}
	file.Close()
	info, _ = os.Stat(path)
	if !PrivateMode(path, info, 0o600) {
		t.Fatal("private file ACL rejected")
	}
	link := filepath.Join(root, "other.db")
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	if PrivateMode(path, info, 0o600) {
		t.Fatal("hard-linked credential accepted")
	}
	if _, err := OpenFile(path, os.O_RDONLY, 0o600); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("hard-link open error = %v", err)
	}
}

func TestWindowsPinnedDirectoryCannotBeReplacedUntilReleased(t *testing.T) {
	root := privateTestDirectory(t)
	release, err := PinDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, root+"-moved"); err == nil {
		release()
		t.Fatal("pinned private root was moved")
	}
	release()
	if err := os.Rename(root, root+"-moved"); err != nil {
		t.Fatalf("directory guard leaked after release: %v", err)
	}
}

func TestWindowsRejectsBroadACLAndReparse(t *testing.T) {
	root := privateTestDirectory(t)
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, _ := sd.DACL()
	if err := windows.SetNamedSecurityInfo(root, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(root)
	if PrivateMode(root, info, 0o700) {
		t.Fatal("Everyone ACL accepted")
	}
	if err := Secure(root, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "junction")
	createTestJunction(t, link, root)
	info, _ = os.Lstat(link)
	if PrivateMode(link, info, 0o700) {
		t.Fatal("reparse directory accepted")
	}
}

func createTestJunction(t *testing.T, link, target string) {
	t.Helper()
	if err := os.Mkdir(link, 0o700); err != nil {
		t.Fatal(err)
	}
	name, _ := windows.UTF16PtrFromString(link)
	handle, err := windows.CreateFile(name, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	substitute, _ := windows.UTF16FromString(`\??\` + target)
	printName, _ := windows.UTF16FromString(target)
	data := make([]byte, 16+2*(len(substitute)+len(printName)))
	binary.LittleEndian.PutUint32(data, windows.IO_REPARSE_TAG_MOUNT_POINT)
	binary.LittleEndian.PutUint16(data[4:], uint16(len(data)-8))
	binary.LittleEndian.PutUint16(data[10:], uint16(2*(len(substitute)-1)))
	binary.LittleEndian.PutUint16(data[12:], uint16(2*len(substitute)))
	binary.LittleEndian.PutUint16(data[14:], uint16(2*(len(printName)-1)))
	for index, value := range append(substitute, printName...) {
		binary.LittleEndian.PutUint16(data[16+2*index:], value)
	}
	var returned uint32
	if err := windows.DeviceIoControl(handle, windows.FSCTL_SET_REPARSE_POINT, (*byte)(unsafe.Pointer(&data[0])), uint32(len(data)), nil, 0, &returned, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsLockPublicationAndExchange(t *testing.T) {
	root := privateTestDirectory(t)
	path := filepath.Join(root, "owner.lock")
	first, err := OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if locked, err := TryLock(first); !locked || err != nil {
		t.Fatalf("first lock = %v, %v", locked, err)
	}
	if locked, err := TryLock(second); locked || err != nil {
		t.Fatalf("competing lock = %v, %v", locked, err)
	}
	if err := Unlock(first); err != nil {
		t.Fatal(err)
	}
	if locked, err := TryLock(second); !locked || err != nil {
		t.Fatalf("lock after release = %v, %v", locked, err)
	}
	Unlock(second)
	left, right := filepath.Join(root, "canonical.db"), filepath.Join(root, "ready.db")
	for path, content := range map[string]string{left: "old", right: "new"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := Exchange(left, right); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{left: "new", right: "old"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("exchange readback = %q, %v", got, err)
		}
	}
	if _, err := PublishNoReplace(right, left); err == nil {
		t.Fatal("no-clobber publication overwrote canonical")
	}
	got, _ := os.ReadFile(left)
	if string(got) != "new" {
		t.Fatal("failed publication changed canonical")
	}
	if free, err := AvailableBytes(root); err != nil || free <= 0 {
		t.Fatalf("available bytes = %d, %v", free, err)
	}
}

func TestWindowsExchangeRestoresInterruptedReplacementAndRejectsOccupiedFile(t *testing.T) {
	root := privateTestDirectory(t)
	canonical := filepath.Join(root, "canonical.db")
	backup := filepath.Join(root, exchangePrefix(canonical)+"controlled.db")
	if err := os.WriteFile(backup, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecoverExchange(canonical); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(canonical); err != nil || string(content) != "old" {
		t.Fatalf("interrupted recovery = %q, %v", content, err)
	}
	ready := filepath.Join(root, "ready.db")
	if err := os.WriteFile(ready, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	occupied, err := os.Open(canonical)
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	if err := Exchange(canonical, ready); err == nil || errors.Is(err, ErrReplaceCommitted) {
		t.Fatalf("occupied replacement = %v", err)
	}
	if content, err := os.ReadFile(canonical); err != nil || string(content) != "old" {
		t.Fatalf("occupied canonical changed = %q, %v", content, err)
	}
}
