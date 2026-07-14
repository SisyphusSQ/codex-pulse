package logs

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
)

// HomeMetadata is the content-free structure summary shown before the user
// confirms a Codex Home. It contains filesystem identity and aggregate sizes,
// never JSONL or auth file bytes.
type HomeMetadata struct {
	Path                      string
	DeviceID                  string
	Inode                     int64
	SessionsDirectory         bool
	ArchivedSessionsDirectory bool
	SessionIndexFile          bool
	AuthFile                  bool
	JSONLFiles                int64
	JSONLBytes                int64
}

// HomeProbe performs the pre-confirmation, metadata-only Codex Home check.
// Confirmed discovery remains a separate boundary because it reads a bounded
// content prefix to build source fingerprints.
type HomeProbe struct {
	filesystem fileSystem
}

func NewHomeProbe() *HomeProbe {
	return &HomeProbe{filesystem: osFileSystem{}}
}

func newHomeProbe(filesystem fileSystem) *HomeProbe {
	return &HomeProbe{filesystem: filesystem}
}

func (probe *HomeProbe) Probe(ctx context.Context, home string) (HomeMetadata, error) {
	if probe == nil || probe.filesystem == nil || !filepath.IsAbs(home) {
		return HomeMetadata{}, ErrInvalidHome
	}
	if err := ctx.Err(); err != nil {
		return HomeMetadata{}, err
	}
	home, observed, err := canonicalProbeHome(home)
	if err != nil {
		return HomeMetadata{}, err
	}
	confirmed, err := probe.filesystem.ConfirmRoot(home, false)
	if err != nil {
		return HomeMetadata{}, err
	}
	if confirmed != observed {
		return HomeMetadata{}, ErrHomeChanged
	}
	root, err := probe.filesystem.OpenRoot(home, false)
	if err != nil {
		return HomeMetadata{}, err
	}
	defer func() { _ = root.Close() }()
	if root.Identity() != confirmed {
		return HomeMetadata{}, ErrHomeChanged
	}

	metadata := HomeMetadata{
		Path: home, DeviceID: confirmed.deviceID, Inode: confirmed.inode,
	}
	metadata.SessionsDirectory, err = probe.scanJSONLDirectory(ctx, root, "sessions", &metadata)
	if err != nil {
		return HomeMetadata{}, err
	}
	metadata.ArchivedSessionsDirectory, err = probe.scanJSONLDirectory(
		ctx, root, "archived_sessions", &metadata,
	)
	if err != nil {
		return HomeMetadata{}, err
	}
	metadata.SessionIndexFile, err = probe.probeRootFile(root, "session_index.jsonl", true, &metadata)
	if err != nil {
		return HomeMetadata{}, err
	}
	metadata.AuthFile, err = probe.probeRootFile(root, "auth.json", false, &metadata)
	if err != nil {
		return HomeMetadata{}, err
	}
	if err := ctx.Err(); err != nil {
		return HomeMetadata{}, err
	}
	return metadata, nil
}

func canonicalProbeHome(home string) (string, rootIdentity, error) {
	home = filepath.Clean(home)
	info, err := os.Lstat(home)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", rootIdentity{}, ErrInvalidHome
	case err != nil:
		return "", rootIdentity{}, err
	case info.Mode()&os.ModeSymlink != 0:
		return "", rootIdentity{}, ErrUnsafeHome
	case !info.IsDir():
		return "", rootIdentity{}, ErrInvalidHome
	}
	observed, err := identityFromFileInfo(info)
	if err != nil {
		return "", rootIdentity{}, err
	}
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil {
		return "", rootIdentity{}, err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", rootIdentity{}, err
	}
	return filepath.Clean(resolved), observed, nil
}

func (probe *HomeProbe) scanJSONLDirectory(
	ctx context.Context,
	root scanRoot,
	relativePath string,
	result *HomeMetadata,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	metadata, err := root.Metadata(relativePath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !metadata.directory {
		return false, ErrUnsupportedFile
	}
	if err := probe.scanStableJSONLDirectory(ctx, root, relativePath, metadata, result); err != nil {
		return false, err
	}
	return true, nil
}

func (probe *HomeProbe) scanStableJSONLDirectory(
	ctx context.Context,
	root scanRoot,
	relativePath string,
	before fileMetadata,
	result *HomeMetadata,
) error {
	entries, err := root.ReadDir(relativePath)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrChangedDuringScan
	}
	if err != nil {
		return err
	}
	if err := probe.scanJSONLEntries(ctx, root, relativePath, entries, result); err != nil {
		return err
	}
	afterEntries, err := root.ReadDir(relativePath)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrChangedDuringScan
	}
	if err != nil {
		return err
	}
	if !sameDirectoryEntries(entries, afterEntries) {
		return ErrChangedDuringScan
	}
	after, err := root.Metadata(relativePath)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrChangedDuringScan
	}
	if err != nil {
		return err
	}
	if !sameEntryIdentity(before, after) || !after.directory {
		return ErrChangedDuringScan
	}
	return nil
}

func (probe *HomeProbe) scanJSONLEntries(
	ctx context.Context,
	root scanRoot,
	relativePath string,
	entries []fs.DirEntry,
	result *HomeMetadata,
) error {
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		child := filepath.Join(relativePath, entry.Name())
		if entry.Type()&fs.ModeSymlink != 0 {
			return ErrUnsafeSource
		}
		metadata, err := root.Metadata(child)
		if errors.Is(err, fs.ErrNotExist) {
			return ErrChangedDuringScan
		}
		if err != nil {
			return err
		}
		if snapshot, ok := entry.(interface{ snapshotMetadata() fileMetadata }); ok &&
			!sameEntryIdentity(snapshot.snapshotMetadata(), metadata) {
			return ErrChangedDuringScan
		}
		switch {
		case metadata.directory && filepath.Ext(entry.Name()) == ".jsonl":
			return ErrUnsupportedFile
		case metadata.directory:
			if err := probe.scanStableJSONLDirectory(ctx, root, child, metadata, result); err != nil {
				return err
			}
		case filepath.Ext(entry.Name()) != ".jsonl":
			continue
		case !metadata.regular:
			return ErrUnsupportedFile
		default:
			if err := addJSONLMetadata(result, metadata.sizeBytes); err != nil {
				return err
			}
		}
	}
	return nil
}

func sameDirectoryEntries(left, right []fs.DirEntry) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Name() != right[index].Name() || left[index].Type() != right[index].Type() {
			return false
		}
		leftSnapshot, leftOK := left[index].(interface{ snapshotMetadata() fileMetadata })
		rightSnapshot, rightOK := right[index].(interface{ snapshotMetadata() fileMetadata })
		if leftOK != rightOK {
			return false
		}
		if leftOK && !sameEntryIdentity(leftSnapshot.snapshotMetadata(), rightSnapshot.snapshotMetadata()) {
			return false
		}
	}
	return true
}

func sameEntryIdentity(left, right fileMetadata) bool {
	return left.deviceID == right.deviceID && left.inode == right.inode &&
		left.regular == right.regular && left.directory == right.directory &&
		left.symlink == right.symlink
}

func (probe *HomeProbe) probeRootFile(
	root scanRoot,
	relativePath string,
	countJSONL bool,
	result *HomeMetadata,
) (bool, error) {
	metadata, err := root.Metadata(relativePath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !metadata.regular {
		return false, ErrUnsupportedFile
	}
	if countJSONL {
		if err := addJSONLMetadata(result, metadata.sizeBytes); err != nil {
			return false, err
		}
	}
	return true, nil
}

func addJSONLMetadata(result *HomeMetadata, sizeBytes int64) error {
	if result == nil || sizeBytes < 0 || result.JSONLFiles == math.MaxInt64 ||
		result.JSONLBytes > math.MaxInt64-sizeBytes {
		return ErrUnsupportedFile
	}
	result.JSONLFiles++
	result.JSONLBytes += sizeBytes
	return nil
}
