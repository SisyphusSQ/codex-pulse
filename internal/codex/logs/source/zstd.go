package source

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	"github.com/klauspost/compress/zstd"
)

const (
	// MaxCompressedRolloutBytes 限制单个压缩输入，避免预载正文无界增长。
	MaxCompressedRolloutBytes = 64 << 20
	// MaxDecodedRolloutBytes 限制解压输出和 decoder 内存。
	MaxDecodedRolloutBytes = 256 << 20
)

// ErrInvalidCompressedSource 表示损坏、截断或超过解压预算的来源。
var ErrInvalidCompressedSource = errors.New("invalid or oversized compressed rollout")

// decodedSnapshot 仅在一次扫描调用内持有正文，Close 立即清除；物理指纹仍由原 reader 校验。
type decodedSnapshot struct {
	data              []byte
	physical, logical Snapshot
	reader            *SnapshotReader
	readBytes         int64
}

// OpenZstdSnapshot 完整验证压缩流，提供逻辑 JSONL 字节视图，不创建磁盘副本。
// 返回的 reader 必须 Close，且只能由当前扫描 owner 使用。
func (reader *SnapshotReader) OpenZstdSnapshot(ctx context.Context, physical Snapshot) (*SnapshotReader, Snapshot, error) {
	return reader.openZstdSnapshot(ctx, physical, MaxCompressedRolloutBytes, MaxDecodedRolloutBytes)
}

func (reader *SnapshotReader) openZstdSnapshot(ctx context.Context, physical Snapshot, encodedLimit, decodedLimit int64) (*SnapshotReader, Snapshot, error) {
	if physical.Fingerprint.SizeBytes > encodedLimit {
		return nil, Snapshot{}, ErrInvalidCompressedSource
	}
	var encoded bytes.Buffer
	defer func() { clear(encoded.Bytes()) }()
	read, err := reader.read(ctx, physical, 0, nil, func(chunk []byte, _ bool) error {
		_, err := encoded.Write(chunk)
		return err
	})
	if err != nil {
		return nil, Snapshot{}, err
	}
	decoder, err := zstd.NewReader(bytes.NewReader(encoded.Bytes()), zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(uint64(decodedLimit)), zstd.WithDecoderLowmem(true))
	if err != nil {
		return nil, Snapshot{}, errors.Join(ErrInvalidCompressedSource, err)
	}
	defer decoder.Close()
	data, err := io.ReadAll(io.LimitReader(contextDecoder{ctx, decoder}, decodedLimit+1))
	if err != nil || int64(len(data)) > decodedLimit {
		clear(data)
		if ctx.Err() != nil {
			return nil, Snapshot{}, ctx.Err()
		}
		return nil, Snapshot{}, errors.Join(ErrInvalidCompressedSource, err)
	}
	logical := physical
	n := min(len(data), int(PrefixLimitBytes))
	sum := sha256.Sum256(data[:n])
	logical.Fingerprint = buildFingerprint(physical.Fingerprint.DeviceID, physical.Fingerprint.Inode, int64(len(data)), physical.Fingerprint.MTimeNS, int64(n), hex.EncodeToString(sum[:]))
	logical.Comparison = nil
	view := *reader
	view.decoded = &decodedSnapshot{data: data, physical: physical, logical: logical, reader: reader, readBytes: read.BytesRead}
	return &view, logical, nil
}

type contextDecoder struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextDecoder) Read(p []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(p)
}

// Close 清除解压视图的临时正文；普通 reader 不持有文件或正文。
func (reader *SnapshotReader) Close() {
	if reader != nil && reader.decoded != nil {
		clear(reader.decoded.data)
		reader.decoded.data = nil
	}
}

// Decoded 表示该 reader 已完整验证过压缩流，须按有界完整快照发布。
func (reader *SnapshotReader) Decoded() bool { return reader != nil && reader.decoded != nil }

func (reader *SnapshotReader) readDecoded(ctx context.Context, snapshot Snapshot, start int64, budget *int64, consume func([]byte, bool) error) (SnapshotReadResult, error) {
	v := reader.decoded
	result := SnapshotReadResult{Offset: start}
	if snapshot.Path != v.logical.Path || snapshot.Fingerprint != v.logical.Fingerprint || start < 0 || start > int64(len(v.data)) || int64(len(v.data)) != snapshot.Fingerprint.SizeBytes {
		return result, ErrInvalidSnapshot
	}
	proof, err := v.reader.read(ctx, v.physical, v.physical.Fingerprint.SizeBytes, nil, func([]byte, bool) error { return nil })
	result.BytesRead = proof.BytesRead + v.readBytes
	v.readBytes = 0
	if err != nil {
		return result, err
	}
	end := int64(len(v.data))
	if budget != nil {
		end = min(end, start+*budget)
	}
	var stopped error
	for offset := start; offset < end; {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		next := min(end, offset+int64(reader.chunkBytes))
		consumeErr := consume(v.data[offset:next], next == int64(len(v.data)))
		result.Offset, result.ContentBytes = next, next-start
		offset = next
		if consumeErr != nil {
			if !errors.Is(consumeErr, errSnapshotReadStopped) {
				return result, consumeErr
			}
			stopped = consumeErr
			break
		}
	}
	result.EOF = result.Offset == int64(len(v.data))
	if start == int64(len(v.data)) {
		if err := consume(nil, true); err != nil {
			return result, err
		}
	}
	proof, err = v.reader.read(ctx, v.physical, v.physical.Fingerprint.SizeBytes, nil, func([]byte, bool) error { return nil })
	result.BytesRead += proof.BytesRead
	if err == nil && stopped != nil {
		err = stopped
	}
	return result, err
}
