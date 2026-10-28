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

// WindowsFileInfo 保留完整 128 位文件 ID 与读取前后所需的原生元数据。
type WindowsFileInfo struct {
	VolumeID   uint64
	FileID     [16]byte
	SizeBytes  int64
	MTimeNS    int64
	ChangeNS   int64
	Attributes uint32
}

// InspectHandle 不读取正文，只查询磁盘对象的原生身份、大小与变化时刻。
func InspectHandle(handle windows.Handle) (WindowsFileInfo, error) {
	var id struct {
		VolumeID uint64
		FileID   [16]byte
	}
	var basic struct {
		Created, Accessed, Written, Changed int64
		Attributes                          uint32
		Padding                             uint32
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return WindowsFileInfo{}, err
	}
	if err := windows.GetFileInformationByHandleEx(handle, windows.FileIdInfo, (*byte)(unsafe.Pointer(&id)), uint32(unsafe.Sizeof(id))); err != nil {
		return WindowsFileInfo{}, err
	}
	if err := windows.GetFileInformationByHandleEx(handle, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&basic)), uint32(unsafe.Sizeof(basic))); err != nil {
		return WindowsFileInfo{}, err
	}
	const unixEpochTicks = 116444736000000000
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || basic.Written < unixEpochTicks || basic.Changed < unixEpochTicks ||
		basic.Written-unixEpochTicks > math.MaxInt64/100 || basic.Changed-unixEpochTicks > math.MaxInt64/100 {
		return WindowsFileInfo{}, ErrUnsafe
	}
	return WindowsFileInfo{
		VolumeID: id.VolumeID, FileID: id.FileID,
		SizeBytes: int64(uint64(info.FileSizeHigh)<<32 | uint64(info.FileSizeLow)),
		MTimeNS:   (basic.Written - unixEpochTicks) * 100, ChangeNS: (basic.Changed - unixEpochTicks) * 100,
		Attributes: info.FileAttributes,
	}, nil
}

// BoundFile 在关闭文件时同时释放其父路径组件，不把校验与读取拆为可替换路径。
type BoundFile struct {
	*os.File
	release func()
}

func (file *BoundFile) Close() error {
	if file == nil || file.File == nil {
		return nil
	}
	err := file.File.Close()
	file.File = nil
	file.release()
	return err
}

// ReadRoot 将对 Agent 来源的访问绑定在真实且无 reparse 的目录；不要求来源采用 Pulse ACL。
type ReadRoot struct {
	*BoundFile
	Path string
}

func OpenReadRoot(path string) (*ReadRoot, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrUnsafe
	}
	file, err := openBoundSource(path, 0, true, false)
	if err != nil {
		return nil, err
	}
	return &ReadRoot{BoundFile: file, Path: path}, nil
}

func (root *ReadRoot) child(relative string, flags int, directory, metadataOnly bool) (*BoundFile, error) {
	if root == nil || root.File == nil || filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" || strings.Contains(relative, ":") {
		return nil, ErrUnsafe
	}
	relative = filepath.Clean(relative)
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || (relative == "." && !directory) {
		return nil, ErrUnsafe
	}
	return openBoundSource(filepath.Join(root.Path, relative), flags, directory, metadataOnly)
}

func (root *ReadRoot) OpenFile(relative string, flags int) (*BoundFile, error) {
	return root.child(relative, flags, false, false)
}

func (root *ReadRoot) OpenDirectory(relative string) (*BoundFile, error) {
	return root.child(relative, 0, true, false)
}

func (root *ReadRoot) OpenMetadata(relative string) (*BoundFile, error) {
	return root.child(relative, 0, false, true)
}

func openBoundSource(path string, flags int, directory, metadataOnly bool) (*BoundFile, error) {
	release, err := pinPath(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	closeWithError := func(err error) (*BoundFile, error) { release(); return nil, err }
	name, err := nativePath(path)
	if err != nil {
		return closeWithError(err)
	}
	access := uint32(windows.FILE_READ_ATTRIBUTES)
	if !metadataOnly {
		access |= windows.GENERIC_READ
	}
	if flags&(os.O_RDWR|os.O_WRONLY) != 0 {
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
	share := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE)
	if directory {
		access |= windows.FILE_LIST_DIRECTORY
		share &^= windows.FILE_SHARE_DELETE
	}
	attributes := uint32(windows.FILE_FLAG_OPEN_REPARSE_POINT | windows.FILE_FLAG_BACKUP_SEMANTICS)
	if flags&os.O_CREATE != 0 {
		attributes |= windows.FILE_FLAG_WRITE_THROUGH
	}
	if flags&os.O_SYNC != 0 {
		attributes |= windows.FILE_FLAG_WRITE_THROUGH
	}
	handle, err := windows.CreateFile(name, access, share, nil, disposition, attributes, 0)
	if err != nil {
		return closeWithError(err)
	}
	file := os.NewFile(uintptr(handle), path)
	info, inspectErr := InspectHandle(handle)
	fileType, typeErr := windows.GetFileType(handle)
	if inspectErr != nil || typeErr != nil || fileType != windows.FILE_TYPE_DISK ||
		(!metadataOnly && (info.Attributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) != directory) {
		file.Close()
		return closeWithError(errors.Join(ErrUnsafe, inspectErr, typeErr))
	}
	if flags&os.O_TRUNC != 0 {
		if err := file.Truncate(0); err != nil {
			file.Close()
			return closeWithError(err)
		}
	}
	return &BoundFile{File: file, release: release}, nil
}
