//go:build darwin

package logs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"
)

const maxSnapshotReadChunkBytes = 4 << 20

// SnapshotReader reopens one discovered source through the confirmed Home and
// proves that the bytes still belong to the exact discovery snapshot.
type SnapshotReader struct {
	home         string
	rootIdentity rootIdentity
	chunkBytes   int
}

func NewSnapshotReader(home string, chunkBytes int) (*SnapshotReader, error) {
	return newSnapshotReader(home, nil, chunkBytes)
}

// NewConfirmedSnapshotReader requires the root to remain the exact physical
// Home confirmed by Preferences and persisted with the bootstrap job.
func NewConfirmedSnapshotReader(
	home string,
	deviceID string,
	inode int64,
	chunkBytes int,
) (*SnapshotReader, error) {
	if deviceID == "" || inode <= 0 {
		return nil, ErrInvalidHome
	}
	expected := rootIdentity{deviceID: deviceID, inode: inode}
	return newSnapshotReader(home, &expected, chunkBytes)
}

func newSnapshotReader(home string, expected *rootIdentity, chunkBytes int) (*SnapshotReader, error) {
	if !filepath.IsAbs(home) || chunkBytes <= 0 || chunkBytes > maxSnapshotReadChunkBytes {
		return nil, ErrInvalidHome
	}
	home = filepath.Clean(home)
	identity, err := (osFileSystem{}).ConfirmRoot(home, true)
	if err != nil {
		return nil, err
	}
	if expected != nil && identity != *expected {
		return nil, ErrHomeChanged
	}
	return &SnapshotReader{home: home, rootIdentity: identity, chunkBytes: chunkBytes}, nil
}

// Read starts at a previously committed offset and invokes consume
// synchronously. The final callback has eof=true, including an empty snapshot.
func (reader *SnapshotReader) Read(
	ctx context.Context,
	snapshot Snapshot,
	startOffset int64,
	consume func(chunk []byte, eof bool) error,
) (int64, error) {
	if reader == nil || reader.chunkBytes <= 0 || consume == nil {
		return startOffset, ErrInvalidSnapshot
	}
	if err := ctx.Err(); err != nil {
		return startOffset, err
	}
	if err := validateSnapshotForHome(reader.home, snapshot); err != nil ||
		snapshot.Kind == SourceKindSessionIndex || startOffset < 0 ||
		startOffset > snapshot.Fingerprint.SizeBytes {
		return startOffset, ErrInvalidSnapshot
	}
	root, err := openConfirmedSnapshotRoot(reader.home)
	if err != nil {
		return startOffset, err
	}
	defer func() { _ = root.Close() }()
	if root.Identity() != reader.rootIdentity {
		return startOffset, ErrHomeChanged
	}
	relativePath, err := filepath.Rel(reader.home, snapshot.Path)
	if err != nil || filepath.IsAbs(relativePath) {
		return startOffset, ErrUnsafeSource
	}
	fileDescriptor, err := openAtNoFollow(int(root.file.Fd()), relativePath, false)
	if err != nil {
		return startOffset, classifySourceOpenError(err)
	}
	file := os.NewFile(uintptr(fileDescriptor), relativePath)
	if file == nil {
		_ = unix.Close(fileDescriptor)
		return startOffset, ErrUnsupportedFile
	}
	defer func() { _ = file.Close() }()

	var before unix.Stat_t
	if err := unix.Fstat(fileDescriptor, &before); err != nil {
		return startOffset, err
	}
	if !snapshotMatchesStat(snapshot, before) {
		return startOffset, ErrChangedDuringScan
	}
	if err := verifySnapshotPrefix(file, snapshot); err != nil {
		return startOffset, err
	}

	offset := startOffset
	if offset == snapshot.Fingerprint.SizeBytes {
		if err := consume(nil, true); err != nil {
			return offset, err
		}
	}
	for offset < snapshot.Fingerprint.SizeBytes {
		if err := ctx.Err(); err != nil {
			return offset, err
		}
		remaining := snapshot.Fingerprint.SizeBytes - offset
		chunkSize := int64(reader.chunkBytes)
		if remaining < chunkSize {
			chunkSize = remaining
		}
		chunk := make([]byte, int(chunkSize))
		readBytes, readErr := file.ReadAt(chunk, offset)
		if int64(readBytes) != chunkSize || readErr != nil && !errors.Is(readErr, io.EOF) {
			clear(chunk)
			return offset, ErrChangedDuringScan
		}
		offset += int64(readBytes)
		eof := offset == snapshot.Fingerprint.SizeBytes
		consumeErr := consume(chunk, eof)
		clear(chunk)
		if consumeErr != nil {
			return offset, consumeErr
		}
	}

	var after unix.Stat_t
	if err := unix.Fstat(fileDescriptor, &after); err != nil {
		return offset, err
	}
	if !sameFileStat(before, after) || !snapshotMatchesStat(snapshot, after) {
		return offset, ErrChangedDuringScan
	}
	return offset, nil
}

func openConfirmedSnapshotRoot(home string) (*osScanRoot, error) {
	path, err := rootPathForOpen(home, true)
	if err != nil {
		return nil, err
	}
	root, err := openOSRoot(path)
	if err != nil {
		return nil, classifyRootOpenError(path, err)
	}
	return root, nil
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

func verifySnapshotPrefix(file *os.File, snapshot Snapshot) error {
	prefix := make([]byte, int(snapshot.Fingerprint.PrefixBytes))
	readBytes, err := file.ReadAt(prefix, 0)
	if int64(readBytes) != snapshot.Fingerprint.PrefixBytes || err != nil && !errors.Is(err, io.EOF) {
		clear(prefix)
		return ErrChangedDuringScan
	}
	digest := sha256.Sum256(prefix)
	clear(prefix)
	if hex.EncodeToString(digest[:]) != snapshot.Fingerprint.PrefixSHA256 {
		return ErrChangedDuringScan
	}
	return nil
}
