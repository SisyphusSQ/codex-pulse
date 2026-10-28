package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

type indexFileStatus struct {
	deviceID   string
	inode      int64
	size       int64
	mtimeNS    int64
	changeNS   int64
	attributes uint32
	regular    bool
}

func readOpenedIndex(ctx context.Context, file *os.File) (IndexRead, error) {
	if err := ctx.Err(); err != nil {
		return IndexRead{}, err
	}
	before, err := statIndexFile(file)
	if err != nil {
		return IndexRead{}, fmt.Errorf("stat session index: %w", err)
	}
	if before.size > maxIndexBytes {
		return IndexRead{}, ErrIndexTooLarge
	}
	if !before.regular || before.size < 0 || before.mtimeNS < 0 {
		return IndexRead{}, ErrUnsafeIndex
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return IndexRead{}, fmt.Errorf("seek session index: %w", err)
	}
	content, err := io.ReadAll(io.LimitReader(file, maxIndexBytes+1))
	if err != nil {
		return IndexRead{}, fmt.Errorf("read session index: %w", err)
	}
	if len(content) > maxIndexBytes {
		clear(content)
		return IndexRead{}, ErrIndexTooLarge
	}
	after, err := statIndexFile(file)
	if err != nil {
		clear(content)
		return IndexRead{}, fmt.Errorf("restat session index: %w", err)
	}
	if before != after || int64(len(content)) != before.size {
		clear(content)
		return IndexRead{}, ErrPlanDrift
	}
	digest := sha256.Sum256(content)
	return IndexRead{Content: content, Version: FileVersion{
		Exists: true, DeviceID: before.deviceID, Inode: before.inode, SizeBytes: before.size,
		MTimeNS: before.mtimeNS, SHA256: hex.EncodeToString(digest[:]),
	}}, nil
}
