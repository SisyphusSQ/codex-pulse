//go:build darwin

package homeidentity

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestFromDescriptorUsesStableVolumeIdentity(t *testing.T) {
	t.Parallel()

	directory, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatalf("os.Open() error = %v", err)
	}
	defer directory.Close()

	identity, err := FromDescriptor(int(directory.Fd()))
	if err != nil {
		t.Fatalf("FromDescriptor() error = %v", err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(directory.Fd()), &stat); err != nil {
		t.Fatalf("unix.Fstat() error = %v", err)
	}
	if !IsStableDeviceID(identity.DeviceID) ||
		!strings.HasPrefix(identity.DeviceID, "volume:") ||
		len(identity.DeviceID) != len("volume:")+32 {
		t.Fatalf("DeviceID = %q, want volume:<32 lowercase hex>", identity.DeviceID)
	}
	if identity.Inode != int64(stat.Ino) {
		t.Fatalf("Inode = %d, want %d", identity.Inode, stat.Ino)
	}
}

func TestDeviceIDClassifiesLegacyAndStableFormats(t *testing.T) {
	t.Parallel()

	if !IsLegacyDeviceID("16777231") || IsLegacyDeviceID("volume:00112233445566778899aabbccddeeff") {
		t.Fatal("IsLegacyDeviceID() did not distinguish raw st_dev from stable volume UUID")
	}
	for _, value := range []string{
		"",
		"0",
		"-1",
		"device-1",
		"volume:00112233445566778899AABBCCDDEEFF",
		"volume:00112233445566778899aabbccddeefg",
		"volume:00112233445566778899aabbccddeeff00",
	} {
		if IsLegacyDeviceID(value) || IsStableDeviceID(value) {
			t.Fatalf("invalid DeviceID %q was accepted", value)
		}
	}
}
