//go:build darwin

package source

import (
	"math"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

func openSnapshotFile(root *osScanRoot, relative string) (*os.File, func() error, error) {
	fd, err := openAtNoFollow(int(root.file.Fd()), relative, false)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), relative)
	if file == nil {
		unix.Close(fd)
		return nil, nil, ErrUnsupportedFile
	}
	return file, file.Close, nil
}

func snapshotFileStat(file *os.File) (unix.Stat_t, error) {
	var stat unix.Stat_t
	err := unix.Fstat(int(file.Fd()), &stat)
	return stat, err
}
func snapshotMatchesStat(snapshot Snapshot, stat unix.Stat_t) bool {
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Size < 0 || stat.Ino > math.MaxInt64 {
		return false
	}
	mtimeNS := stat.Mtim.Sec*1_000_000_000 + stat.Mtim.Nsec
	return snapshot.Fingerprint.DeviceID == strconv.FormatUint(uint64(uint32(stat.Dev)), 10) &&
		snapshot.Fingerprint.Inode == int64(stat.Ino) && snapshot.Fingerprint.SizeBytes == stat.Size &&
		snapshot.Fingerprint.MTimeNS == mtimeNS
}
