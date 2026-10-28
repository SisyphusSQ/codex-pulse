package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/SisyphusSQ/codex-pulse/internal/platformfs"
)

const sessionIndexFilename = "session_index.jsonl"

type rootVersion struct {
	deviceID string
	inode    uint64
}

// IndexFile 绑定用户已确认的 Codex Home 物理身份，只访问根级 session index。
type IndexFile struct {
	home            string
	root            rootVersion
	syncDirectory   func(string) error
	syncRoot        func(int) error
	afterFinalCheck func()
}

// IndexRead 是一次完整、无副作用的 session index 读取。
type IndexRead struct {
	Content []byte
	Version FileVersion
}

// IndexBackupReport 描述成功发布的私有 index backup 或 absence marker。
type IndexBackupReport struct {
	Path          string
	SourceExisted bool
	Bytes         int64
}

// OpenIndexFile 确认绝对、非 reparse/symlink Home 的物理身份。
func OpenIndexFile(home string) (*IndexFile, error) {
	return openIndexFile(home, nil)
}

// OpenConfirmedIndexFile 将 recency hint 与 Preferences 已确认的物理 Home 绑定。
func OpenConfirmedIndexFile(home, deviceID string, inode int64) (*IndexFile, error) {
	if deviceID == "" || inode <= 0 {
		return nil, ErrInvalidHome
	}
	expected := rootVersion{deviceID: deviceID, inode: uint64(inode)}
	return openIndexFile(home, &expected)
}

func openIndexFile(home string, expected *rootVersion) (*IndexFile, error) {
	if !filepath.IsAbs(home) || filepath.Clean(home) != home {
		return nil, ErrInvalidHome
	}
	file, version, err := openRoot(home)
	if err != nil {
		return nil, err
	}
	if expected != nil && version != *expected {
		_ = file.Close()
		return nil, ErrHomeChanged
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close confirmed Codex home: %w", err)
	}
	return &IndexFile{
		home: home, root: version, syncDirectory: syncDirectoryPath, syncRoot: syncRootDescriptor,
	}, nil
}

// Read 重新确认 Home，并以读取前后元数据证明同一 index 版本。
func (indexFile *IndexFile) Read(ctx context.Context) (IndexRead, error) {
	if err := ctx.Err(); err != nil {
		return IndexRead{}, err
	}
	root, err := indexFile.openRoot()
	if err != nil {
		return IndexRead{}, err
	}
	defer root.Close()
	return readIndexRoot(ctx, root)
}

// Backup 只在版本精确匹配时发布原字节备份或 absence marker。
func (indexFile *IndexFile) Backup(
	ctx context.Context,
	expected FileVersion,
	destination string,
) (IndexBackupReport, error) {
	if !validFileVersion(expected) {
		return IndexBackupReport{}, ErrInvalidPlan
	}
	if indexFile == nil || indexFile.syncDirectory == nil {
		return IndexBackupReport{}, ErrInvalidPlan
	}
	current, err := indexFile.Read(ctx)
	if err != nil {
		return IndexBackupReport{}, err
	}
	if current.Version != expected {
		clear(current.Content)
		return IndexBackupReport{}, ErrPlanDrift
	}
	defer clear(current.Content)
	destination, err = validatePrivateDestination(destination)
	if err != nil {
		return IndexBackupReport{}, err
	}
	if err := publishPrivateBytesWithSync(destination, current.Content, indexFile.syncDirectory); err != nil {
		return IndexBackupReport{}, err
	}
	return IndexBackupReport{
		Path: destination, SourceExisted: expected.Exists, Bytes: int64(len(current.Content)),
	}, nil
}

// Append 仅向已确认的 index 版本追加修复，不重写既有历史或 Session JSONL。
func (indexFile *IndexFile) Append(
	ctx context.Context,
	expected FileVersion,
	entries []Entry,
) (FileVersion, error) {
	if !validFileVersion(expected) || indexFile == nil || indexFile.syncRoot == nil {
		return FileVersion{}, ErrInvalidPlan
	}
	payload, err := canonicalEntries(entries)
	if err != nil {
		return FileVersion{}, err
	}
	if err := ctx.Err(); err != nil {
		return FileVersion{}, err
	}
	root, err := indexFile.openRoot()
	if err != nil {
		return FileVersion{}, err
	}
	defer root.Close()

	var proof appendProof
	if expected.Exists {
		proof, err = indexFile.appendExistingIndexRoot(ctx, root, expected, payload)
	} else {
		if err := validateAppendSize(0, len(payload)); err != nil {
			return FileVersion{}, err
		}
		proof, err = indexFile.createIndexWithAppendRoot(ctx, root, payload)
	}
	if err != nil {
		return FileVersion{}, err
	}
	read, err := indexFile.Read(ctx)
	if err != nil {
		return FileVersion{}, err
	}
	defer clear(read.Content)
	if read.Version.SizeBytes != proof.sizeBytes || read.Version.SHA256 != proof.sha256 {
		return FileVersion{}, ErrPlanDrift
	}
	return read.Version, nil
}

func appendedContentProof(prefix, payload []byte) appendProof {
	digest := sha256.New()
	_, _ = digest.Write(prefix)
	_, _ = digest.Write(payload)
	return appendProof{
		sizeBytes: int64(len(prefix) + len(payload)),
		sha256:    hex.EncodeToString(digest.Sum(nil)),
	}
}

func appendAndSync(ctx context.Context, file *os.File, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	written, err := file.Write(payload)
	if err != nil {
		return fmt.Errorf("append session index: %w", err)
	}
	if written != len(payload) {
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync session index: %w", err)
	}
	return ctx.Err()
}

func canonicalEntries(entries []Entry) ([]byte, error) {
	if len(entries) == 0 {
		return nil, ErrInvalidPlan
	}
	var payload []byte
	for _, entry := range entries {
		if _, err := uuid.Parse(entry.ID); err != nil || entry.ThreadName == "" ||
			len(entry.ThreadName) > maxThreadNameBytes {
			return nil, ErrInvalidPlan
		}
		updatedAt, err := time.Parse(time.RFC3339Nano, entry.UpdatedAt)
		if err != nil || updatedAt.UnixMilli() < 0 {
			return nil, ErrInvalidPlan
		}
		line, err := json.Marshal(entry)
		if err != nil {
			return nil, fmt.Errorf("encode session index entry: %w", err)
		}
		if len(line) > maxIndexLineBytes {
			return nil, ErrInvalidPlan
		}
		if int64(len(payload))+int64(len(line))+1 > maxIndexBytes {
			return nil, ErrIndexTooLarge
		}
		payload = append(payload, line...)
		payload = append(payload, '\n')
	}
	return payload, nil
}

func validateAppendSize(currentSize int64, payloadSize int) error {
	if currentSize < 0 || payloadSize < 0 {
		return ErrInvalidPlan
	}
	if currentSize > maxIndexBytes || int64(payloadSize) > int64(maxIndexBytes)-currentSize {
		return ErrIndexTooLarge
	}
	return nil
}

func validatePrivateDestination(destination string) (string, error) {
	if !filepath.IsAbs(destination) || filepath.Clean(destination) != destination {
		return "", ErrInvalidBackup
	}
	parent := filepath.Dir(destination)
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !platformfs.PrivateMode(parent, info, 0o700) {
		return "", ErrInvalidBackup
	}
	if _, err := os.Lstat(destination); err == nil || !errors.Is(err, os.ErrNotExist) {
		return "", ErrInvalidBackup
	}
	return destination, nil
}

func publishPrivateBytesWithSync(
	destination string,
	content []byte,
	syncParent func(string) error,
) error {
	if syncParent == nil {
		return ErrInvalidBackup
	}
	file, err := platformfs.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create session index backup: %w", err)
	}
	written, err := file.Write(content)
	if err != nil || written != len(content) {
		writeErr := err
		if writeErr == nil {
			writeErr = io.ErrShortWrite
		}
		return errors.Join(
			fmt.Errorf("write session index backup: %w", writeErr),
			file.Close(),
			removeAndSyncPrivateFile(destination, syncParent),
		)
	}
	if err := file.Sync(); err != nil {
		return errors.Join(
			fmt.Errorf("sync session index backup: %w", err),
			file.Close(),
			removeAndSyncPrivateFile(destination, syncParent),
		)
	}
	if err := file.Close(); err != nil {
		return errors.Join(
			fmt.Errorf("close session index backup: %w", err),
			removeAndSyncPrivateFile(destination, syncParent),
		)
	}
	parent := filepath.Dir(destination)
	if err := syncParent(parent); err != nil {
		return errors.Join(
			fmt.Errorf("sync session index backup directory: %w", err),
			removeAndSyncPrivateFile(destination, syncParent),
		)
	}
	return nil
}

func removeAndSyncPrivateFile(path string, syncParent func(string) error) error {
	removeErr := os.Remove(path)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	return errors.Join(removeErr, syncParent(filepath.Dir(path)))
}

type appendProof struct {
	sizeBytes int64
	sha256    string
}
