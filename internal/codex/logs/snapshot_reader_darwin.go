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
	readAt       func(*os.File, []byte, int64) (int, error)
}

// SnapshotReadResult 区分实际磁盘IO、交给consumer的内容推进量与真实snapshot EOF。
type SnapshotReadResult struct {
	Offset       int64
	BytesRead    int64
	ContentBytes int64
	EOF          bool
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
	return &SnapshotReader{
		home: home, rootIdentity: identity, chunkBytes: chunkBytes,
		readAt: func(file *os.File, buffer []byte, offset int64) (int, error) {
			return file.ReadAt(buffer, offset)
		},
	}, nil
}

// Read starts at a previously committed offset and invokes consume
// synchronously. The final callback has eof=true, including an empty snapshot.
func (reader *SnapshotReader) Read(
	ctx context.Context,
	snapshot Snapshot,
	startOffset int64,
	consume func(chunk []byte, eof bool) error,
) (int64, error) {
	result, err := reader.read(ctx, snapshot, startOffset, nil, consume)
	return result.Offset, err
}

// ReadLimited 把prefix proof与正文读取都计入maxBytes；budget停止时不会发送伪EOF。
func (reader *SnapshotReader) ReadLimited(
	ctx context.Context,
	snapshot Snapshot,
	startOffset int64,
	maxBytes int64,
	consume func(chunk []byte, eof bool) error,
) (SnapshotReadResult, error) {
	if maxBytes <= 0 {
		return SnapshotReadResult{Offset: startOffset}, ErrInvalidSnapshot
	}
	return reader.read(ctx, snapshot, startOffset, &maxBytes, consume)
}

func (reader *SnapshotReader) read(
	ctx context.Context,
	snapshot Snapshot,
	startOffset int64,
	maxBytes *int64,
	consume func(chunk []byte, eof bool) error,
) (SnapshotReadResult, error) {
	result := SnapshotReadResult{Offset: startOffset}
	if reader == nil || reader.chunkBytes <= 0 || reader.readAt == nil || consume == nil {
		return result, ErrInvalidSnapshot
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := validateSnapshotForHome(reader.home, snapshot); err != nil ||
		snapshot.Kind == SourceKindSessionIndex || startOffset < 0 ||
		startOffset > snapshot.Fingerprint.SizeBytes {
		return result, ErrInvalidSnapshot
	}
	if maxBytes != nil && *maxBytes < snapshot.Fingerprint.PrefixBytes {
		return result, ErrSnapshotBudgetTooSmall
	}
	root, err := openConfirmedSnapshotRoot(reader.home)
	if err != nil {
		return result, err
	}
	defer func() { _ = root.Close() }()
	if root.Identity() != reader.rootIdentity {
		return result, ErrHomeChanged
	}
	relativePath, err := filepath.Rel(reader.home, snapshot.Path)
	if err != nil || filepath.IsAbs(relativePath) {
		return result, ErrUnsafeSource
	}
	fileDescriptor, err := openAtNoFollow(int(root.file.Fd()), relativePath, false)
	if err != nil {
		return result, classifySourceOpenError(err)
	}
	file := os.NewFile(uintptr(fileDescriptor), relativePath)
	if file == nil {
		_ = unix.Close(fileDescriptor)
		return result, ErrUnsupportedFile
	}
	defer func() { _ = file.Close() }()

	var before unix.Stat_t
	if err := unix.Fstat(fileDescriptor, &before); err != nil {
		return result, err
	}
	if !snapshotMatchesStat(snapshot, before) {
		return result, ErrChangedDuringScan
	}
	prefix, proofBytes, proofErr := reader.readAndVerifySnapshotPrefix(file, snapshot)
	result.BytesRead = proofBytes
	if proofErr != nil {
		clear(prefix)
		return result, proofErr
	}
	defer clear(prefix)

	offset := startOffset
	var controlledStop error
	if offset == snapshot.Fingerprint.SizeBytes {
		result.EOF = true
		if err := consume(nil, true); err != nil {
			return result, err
		}
	}
	for offset < snapshot.Fingerprint.SizeBytes {
		if err := ctx.Err(); err != nil {
			result.Offset = offset
			result.ContentBytes = offset - startOffset
			return result, err
		}
		remaining := snapshot.Fingerprint.SizeBytes - offset
		chunkSize := int64(reader.chunkBytes)
		if remaining < chunkSize {
			chunkSize = remaining
		}
		cached := offset < int64(len(prefix))
		if cached {
			prefixRemaining := int64(len(prefix)) - offset
			if prefixRemaining < chunkSize {
				chunkSize = prefixRemaining
			}
		} else if maxBytes != nil {
			budgetRemaining := *maxBytes - result.BytesRead
			if budgetRemaining == 0 {
				break
			}
			if budgetRemaining < chunkSize {
				chunkSize = budgetRemaining
			}
		}
		chunk := make([]byte, int(chunkSize))
		if cached {
			copy(chunk, prefix[offset:offset+chunkSize])
		} else {
			readBytes, readErr := reader.readAt(file, chunk, offset)
			result.BytesRead += int64(readBytes)
			if int64(readBytes) != chunkSize || readErr != nil && !errors.Is(readErr, io.EOF) {
				clear(chunk)
				result.Offset = offset
				result.ContentBytes = offset - startOffset
				return result, ErrChangedDuringScan
			}
		}
		offset += chunkSize
		eof := offset == snapshot.Fingerprint.SizeBytes
		result.Offset = offset
		result.ContentBytes = offset - startOffset
		result.EOF = eof
		consumeErr := consume(chunk, eof)
		clear(chunk)
		if consumeErr != nil {
			if errors.Is(consumeErr, errSnapshotReadStopped) {
				controlledStop = consumeErr
				break
			}
			return result, consumeErr
		}
	}

	var after unix.Stat_t
	if err := unix.Fstat(fileDescriptor, &after); err != nil {
		return result, err
	}
	if !sameFileStat(before, after) || !snapshotMatchesStat(snapshot, after) {
		return result, ErrChangedDuringScan
	}
	result.Offset = offset
	result.ContentBytes = offset - startOffset
	result.EOF = offset == snapshot.Fingerprint.SizeBytes
	if controlledStop != nil {
		return result, controlledStop
	}
	return result, nil
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

func (reader *SnapshotReader) readAndVerifySnapshotPrefix(
	file *os.File,
	snapshot Snapshot,
) ([]byte, int64, error) {
	prefix := make([]byte, int(snapshot.Fingerprint.PrefixBytes))
	readBytes, err := reader.readAt(file, prefix, 0)
	if int64(readBytes) != snapshot.Fingerprint.PrefixBytes || err != nil && !errors.Is(err, io.EOF) {
		return prefix, int64(readBytes), ErrChangedDuringScan
	}
	digest := sha256.Sum256(prefix)
	if hex.EncodeToString(digest[:]) != snapshot.Fingerprint.PrefixSHA256 {
		return prefix, int64(readBytes), ErrChangedDuringScan
	}
	return prefix, int64(readBytes), nil
}
