//go:build darwin

// Package homeidentity 提供跨重启稳定的 Codex Home 物理身份。
package homeidentity

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"path/filepath"
	"runtime"
	"strconv"
	"unsafe"

	"golang.org/x/sys/unix"
)

const stableDeviceIDPrefix = "volume:"

var ErrInvalidIdentity = errors.New("invalid Codex Home identity")

type Identity struct {
	DeviceID string
	Inode    int64
}

// FromDescriptor 使用卷 UUID 与目录 inode 构造持久身份。
//
// Darwin 的 st_dev 是挂载期编号，重启后可能变化，不能直接持久化。
func FromDescriptor(fileDescriptor int) (Identity, error) {
	if fileDescriptor < 0 {
		return Identity{}, ErrInvalidIdentity
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fileDescriptor, &stat); err != nil {
		return Identity{}, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Ino == 0 || stat.Ino > math.MaxInt64 {
		return Identity{}, ErrInvalidIdentity
	}
	volumeUUID, err := volumeUUIDFromDescriptor(fileDescriptor)
	if err != nil {
		return Identity{}, err
	}
	return Identity{
		DeviceID: stableDeviceIDPrefix + hex.EncodeToString(volumeUUID[:]),
		Inode:    int64(stat.Ino),
	}, nil
}

func IsStableDeviceID(value string) bool {
	if len(value) != len(stableDeviceIDPrefix)+32 ||
		value[:len(stableDeviceIDPrefix)] != stableDeviceIDPrefix {
		return false
	}
	for _, character := range value[len(stableDeviceIDPrefix):] {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

func IsLegacyDeviceID(value string) bool {
	parsed, err := strconv.ParseUint(value, 10, 32)
	return err == nil && parsed > 0 && strconv.FormatUint(parsed, 10) == value
}

func volumeUUIDFromDescriptor(fileDescriptor int) ([16]byte, error) {
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(fileDescriptor, &filesystem); err != nil {
		return [16]byte{}, err
	}
	mountPath := filepath.Clean(unix.ByteSliceToString(filesystem.Mntonname[:]))
	if !filepath.IsAbs(mountPath) {
		return [16]byte{}, ErrInvalidIdentity
	}
	mountDescriptor, err := unix.Open(
		mountPath,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY,
		0,
	)
	if err != nil {
		return [16]byte{}, err
	}
	defer func() { _ = unix.Close(mountDescriptor) }()

	var mountedFilesystem unix.Statfs_t
	if err := unix.Fstatfs(mountDescriptor, &mountedFilesystem); err != nil {
		return [16]byte{}, err
	}
	if mountedFilesystem.Fsid != filesystem.Fsid {
		return [16]byte{}, ErrInvalidIdentity
	}

	attributes := unix.Attrlist{
		Bitmapcount: unix.ATTR_BIT_MAP_COUNT,
		Volattr:     unix.ATTR_VOL_INFO | unix.ATTR_VOL_UUID,
	}
	var buffer [4 + 16]byte
	_, _, errno := unix.Syscall6(
		unix.SYS_FGETATTRLIST,
		uintptr(mountDescriptor),
		uintptr(unsafe.Pointer(&attributes)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
		0,
		0,
	)
	runtime.KeepAlive(attributes)
	runtime.KeepAlive(buffer)
	if errno != 0 {
		return [16]byte{}, errno
	}
	if binary.LittleEndian.Uint32(buffer[:4]) != uint32(len(buffer)) {
		return [16]byte{}, ErrInvalidIdentity
	}
	var volumeUUID [16]byte
	copy(volumeUUID[:], buffer[4:])
	if volumeUUID == ([16]byte{}) {
		return [16]byte{}, ErrInvalidIdentity
	}
	return volumeUUID, nil
}
