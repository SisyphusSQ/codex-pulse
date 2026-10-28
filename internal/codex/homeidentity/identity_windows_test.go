//go:build windows

package homeidentity

import (
	"path/filepath"
	"testing"

	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
)

func TestWindowsIdentityRetainsAll128Bits(t *testing.T) {
	first := platformfs.WindowsFileInfo{VolumeID: 123, FileID: [16]byte{1}}
	second := first
	second.FileID[15] = 1
	a, err := FromWindowsFileInfo(first)
	if err != nil || !IsStableDeviceID(a.DeviceID) || a.Inode != 1 {
		t.Fatalf("identity = %#v, %v", a, err)
	}
	b, err := FromWindowsFileInfo(second)
	if err != nil || a == b {
		t.Fatal("high file ID bits were lost")
	}
	path := filepath.Clean(t.TempDir())
	root, err := platformfs.OpenReadRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	before, err := FromDescriptor(int(root.Fd()))
	root.Close()
	if err != nil {
		t.Fatal(err)
	}
	root, err = platformfs.OpenReadRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	after, err := FromDescriptor(int(root.Fd()))
	if err != nil || before != after || !IsStableDeviceID(after.DeviceID) {
		t.Fatalf("reopened identity = %#v, %v", after, err)
	}
}
