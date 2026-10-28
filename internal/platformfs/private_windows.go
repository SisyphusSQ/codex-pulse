//go:build windows

package platformfs

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func nativePath(path string) (*uint16, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrUnsafe
	}
	path = filepath.Clean(path)
	if !strings.HasPrefix(path, `\\?\`) {
		if strings.HasPrefix(path, `\\`) {
			path = `\\?\UNC\` + path[2:]
		} else {
			path = `\\?\` + path
		}
	}
	return windows.UTF16PtrFromString(path)
}

func openMetadata(path string, access uint32) (windows.Handle, error) {
	name, err := nativePath(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	handle, err := windows.CreateFile(name, access|windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return windows.InvalidHandle, err
	}
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(handle, &info) != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		windows.CloseHandle(handle)
		return windows.InvalidHandle, ErrUnsafe
	}
	return handle, nil
}

func validateAncestors(path string) error {
	for {
		handle, err := openMetadata(path, 0)
		if err == nil {
			windows.CloseHandle(handle)
		} else if !errors.Is(err, windows.ERROR_PATH_NOT_FOUND) && !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			return err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

func pinPath(path string) (func(), error) {
	var paths []string
	for current := path; ; current = filepath.Dir(current) {
		paths = append(paths, current)
		if filepath.Dir(current) == current {
			break
		}
	}
	var handles []windows.Handle
	release := func() {
		for index := len(handles) - 1; index >= 0; index-- {
			windows.CloseHandle(handles[index])
		}
	}
	for index := len(paths) - 1; index >= 0; index-- {
		name, err := nativePath(paths[index])
		if err != nil {
			release()
			return nil, err
		}
		handle, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL|windows.FILE_LIST_DIRECTORY,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING,
			windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			release()
			return nil, err
		}
		handles = append(handles, handle)
		var info windows.ByHandleFileInformation
		if windows.GetFileInformationByHandle(handle, &info) != nil ||
			info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 ||
			info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			release()
			return nil, ErrUnsafe
		}
	}
	return release, nil
}

// PinDirectory 固定全部路径组件，SQLite 生命周期中禁止目录移动/替换和 reparse 穿透。
func PinDirectory(path string) (func(), error) {
	release, err := pinPath(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil || !PrivateMode(path, info, 0o700) {
		release()
		return nil, ErrUnsafe
	}
	return release, nil
}

func currentUser() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid, nil
}

// UserDataDirectory 使用 LocalAppData，避免把 SQLite 与凭据放入 roaming profile。
func UserDataDirectory() (string, error) { return os.UserCacheDir() }

func privateDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	sid, err := currentUser()
	if err != nil {
		return nil, err
	}
	return windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;" + sid.String() + ")")
}

func privateHandle(handle windows.Handle, directory bool) bool {
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(handle, &info) != nil ||
		info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 ||
		(info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) != directory || (!directory && info.NumberOfLinks != 1) {
		return false
	}
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	owner, _, err := sd.Owner()
	user, userErr := currentUser()
	if err != nil || userErr != nil || owner == nil || !owner.Equals(user) {
		return false
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount == 0 {
		return false
	}
	userAllowed := false
	for index := uint32(0); index < uint32(acl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(acl, index, &ace) != nil {
			return false
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return false
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.IsValid() || (!sid.Equals(user) && !sid.IsWellKnown(windows.WinLocalSystemSid)) {
			return false
		}
		if sid.Equals(user) && ace.Mask&windows.FILE_GENERIC_READ == windows.FILE_GENERIC_READ &&
			(!directory || ace.Header.AceFlags&(windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE) == windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE) {
			userAllowed = true
		}
	}
	return userAllowed
}

func privateMode(path string, info os.FileInfo, mode os.FileMode) bool {
	if (mode == 0o700 && !info.IsDir()) || (mode == 0o600 && !info.Mode().IsRegular()) || validateAncestors(filepath.Dir(path)) != nil {
		return false
	}
	handle, err := openMetadata(path, windows.READ_CONTROL)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	return privateHandle(handle, info.IsDir())
}

// Secure 只在 caller 确认可建立权限时设置当前用户与 SYSTEM 的私有 DACL。
func Secure(path string, _ os.FileMode) error {
	handle, err := openMetadata(path, windows.READ_CONTROL|windows.WRITE_DAC)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	user, userErr := currentUser()
	if err != nil || userErr != nil || owner == nil || !owner.Equals(user) {
		return ErrUnsafe
	}
	private, err := privateDescriptor()
	if err != nil {
		return err
	}
	acl, _, err := private.DACL()
	if err != nil {
		return err
	}
	return windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}

func OpenFile(path string, flags int, mode os.FileMode) (*os.File, error) {
	release, err := pinPath(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer release()
	if info, err := os.Lstat(path); err == nil && !PrivateMode(path, info, mode) {
		return nil, ErrUnsafe
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	access := uint32(windows.GENERIC_READ | windows.READ_CONTROL)
	if flags&(os.O_WRONLY|os.O_RDWR) != 0 {
		access |= windows.GENERIC_WRITE
	}
	if flags&os.O_APPEND != 0 {
		access &^= windows.GENERIC_WRITE
		access |= windows.FILE_APPEND_DATA | windows.FILE_WRITE_ATTRIBUTES | windows.SYNCHRONIZE
	}
	disposition := uint32(windows.OPEN_EXISTING)
	if flags&os.O_CREATE != 0 {
		disposition = windows.OPEN_ALWAYS
		if flags&os.O_EXCL != 0 {
			disposition = windows.CREATE_NEW
		}
	}
	name, err := nativePath(path)
	if err != nil {
		return nil, err
	}
	sd, err := privateDescriptor()
	if err != nil {
		return nil, err
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	handle, err := windows.CreateFile(name, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		sa, disposition, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	if !privateHandle(windows.Handle(file.Fd()), false) {
		file.Close()
		return nil, ErrUnsafe
	}
	if flags&os.O_TRUNC != 0 {
		if err := file.Truncate(0); err != nil {
			file.Close()
			return nil, err
		}
	}
	return file, nil
}

func TryLock(file *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}

func Unlock(file *os.File) error {
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &windows.Overlapped{})
}

// SyncDirectory 不把 Windows 不支持的目录 FlushFileBuffers 当成功证据。
// Windows 发布点由 Rename/PublishNoReplace 的 MOVEFILE_WRITE_THROUGH 承担。
func SyncDirectory(path string) error {
	handle, err := openMetadata(path, 0)
	if err != nil {
		return err
	}
	return windows.CloseHandle(handle)
}

func move(from, to string, flags uint32) error {
	release, err := PinDirectory(filepath.Dir(from))
	if err != nil {
		return err
	}
	defer release()
	releaseTarget, err := PinDirectory(filepath.Dir(to))
	if err != nil {
		return err
	}
	defer releaseTarget()
	fromName, err := nativePath(from)
	if err != nil {
		return err
	}
	toName, err := nativePath(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(fromName, toName, flags|windows.MOVEFILE_WRITE_THROUGH)
}

func Rename(from, to string) error { return move(from, to, windows.MOVEFILE_REPLACE_EXISTING) }

func RenameInRoot(root *os.Root, from, to string) error {
	return Rename(filepath.Join(root.Name(), from), filepath.Join(root.Name(), to))
}

func PublishInRoot(root *os.Root, from, to string) (bool, error) {
	return PublishNoReplace(filepath.Join(root.Name(), from), filepath.Join(root.Name(), to))
}

func OpenRootFile(root *os.Root, name string, flags int, mode os.FileMode) (*os.File, error) {
	if filepath.Base(name) != name || name == "." || name == ".." {
		return nil, ErrUnsafe
	}
	return OpenFile(filepath.Join(root.Name(), name), flags, mode)
}

func PublishNoReplace(from, to string) (bool, error) { return true, move(from, to, 0) }

func AvailableBytes(path string) (int64, error) {
	name, err := nativePath(path)
	if err != nil {
		return 0, err
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(name, &free, nil, nil); err != nil {
		return 0, err
	}
	return int64(min(free, math.MaxInt64)), nil
}

func SingleOwnedFile(file *os.File) bool { return privateHandle(windows.Handle(file.Fd()), false) }
