// Package homeidentity 提供跨重启稳定的 Codex Home 物理身份。
package homeidentity

import (
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

const stableDeviceIDPrefix = "volume:"
const windowsDeviceIDPrefix = "winfile:"

var ErrInvalidIdentity = errors.New("invalid Codex Home identity")

// Identity 的 DeviceID 是不透明平台身份；Windows 在其中完整编码卷号及 128 位文件 ID。
// Windows Inode 固定为 1，仅保持既有正值合同，不截断或散列 128 位身份。
type Identity struct {
	DeviceID string
	Inode    int64
}

func IsStableDeviceID(value string) bool {
	if strings.HasPrefix(value, stableDeviceIDPrefix) {
		part := strings.TrimPrefix(value, stableDeviceIDPrefix)
		_, err := hex.DecodeString(part)
		return len(part) == 32 && strings.ToLower(part) == part && err == nil
	}
	if strings.HasPrefix(value, windowsDeviceIDPrefix) {
		parts := strings.Split(strings.TrimPrefix(value, windowsDeviceIDPrefix), ":")
		if len(parts) != 2 || len(parts[0]) != 16 || len(parts[1]) != 32 || strings.ToLower(value) != value {
			return false
		}
		_, volumeErr := hex.DecodeString(parts[0])
		id, idErr := hex.DecodeString(parts[1])
		return volumeErr == nil && idErr == nil && string(id) != string(make([]byte, 16))
	}
	return false
}

func IsLegacyDeviceID(value string) bool {
	parsed, err := strconv.ParseUint(value, 10, 32)
	return err == nil && parsed > 0 && strconv.FormatUint(parsed, 10) == value
}
