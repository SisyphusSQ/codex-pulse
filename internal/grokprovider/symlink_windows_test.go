//go:build windows

package grokprovider

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func skipUnavailableSymlink(t *testing.T, err error) {
	t.Helper()
	if errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) {
		t.Skip("未执行：当前 Windows 用户没有创建文件符号链接的权限")
	}
}
