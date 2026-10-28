//go:build windows

package homeidentity

import (
	"encoding/hex"
	"fmt"

	"golang.org/x/sys/windows"

	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
)

func FromWindowsFileInfo(info platformfs.WindowsFileInfo) (Identity, error) {
	if info.FileID == [16]byte{} {
		return Identity{}, ErrInvalidIdentity
	}
	return Identity{DeviceID: fmt.Sprintf("%s%016x:%s", windowsDeviceIDPrefix, info.VolumeID, hex.EncodeToString(info.FileID[:])), Inode: 1}, nil
}

func FromDescriptor(fileDescriptor int) (Identity, error) {
	info, err := platformfs.InspectHandle(windows.Handle(fileDescriptor))
	if err != nil || info.Attributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return Identity{}, ErrInvalidIdentity
	}
	return FromWindowsFileInfo(info)
}
