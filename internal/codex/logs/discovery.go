package logs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
)

type Discoverer struct {
	home         string
	rootIdentity rootIdentity
	filesystem   fileSystem
}

func NewDiscoverer(home string) (*Discoverer, error) {
	return newDiscoverer(home, osFileSystem{})
}

func newDiscoverer(home string, filesystem fileSystem) (*Discoverer, error) {
	if filesystem == nil || !filepath.IsAbs(home) {
		return nil, ErrInvalidHome
	}
	home = filepath.Clean(home)
	identity, err := filesystem.ConfirmRoot(home, true)
	if err != nil {
		return nil, err
	}
	return &Discoverer{home: home, rootIdentity: identity, filesystem: filesystem}, nil
}

func (discoverer *Discoverer) Discover(ctx context.Context) (DiscoveryResult, error) {
	return discoverer.discover(ctx, nil)
}

// DiscoverAgainst additionally proves the current prefix at each prior
// snapshot's prefix length. The proof lets reconcile distinguish a short-file
// append from a rewrite followed by growth without retaining source bytes.
func (discoverer *Discoverer) DiscoverAgainst(
	ctx context.Context,
	previous []Snapshot,
) (DiscoveryResult, error) {
	if discoverer == nil || discoverer.filesystem == nil {
		return DiscoveryResult{}, ErrInvalidHome
	}
	previousByID, _, err := indexSnapshots(discoverer.home, previous)
	if err != nil {
		return DiscoveryResult{}, err
	}
	return discoverer.discover(ctx, previousByID)
}

func (discoverer *Discoverer) discover(
	ctx context.Context,
	previousByID map[string]Snapshot,
) (DiscoveryResult, error) {
	if discoverer == nil || discoverer.filesystem == nil {
		return DiscoveryResult{}, ErrInvalidHome
	}
	if err := ctx.Err(); err != nil {
		return DiscoveryResult{}, err
	}
	root, err := discoverer.filesystem.OpenRoot(discoverer.home, true)
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("%w: %v", ErrHomeChanged, err)
	}
	defer func() { _ = root.Close() }()
	if root.Identity() != discoverer.rootIdentity {
		return DiscoveryResult{}, ErrHomeChanged
	}

	result := DiscoveryResult{}
	roots := []struct {
		relativePath string
		kind         SourceKind
	}{
		{relativePath: "sessions", kind: SourceKindSession},
		{relativePath: "archived_sessions", kind: SourceKindArchivedSession},
	}
	for _, sourceRoot := range roots {
		if err := discoverer.scanDirectory(
			ctx, root, sourceRoot.relativePath, sourceRoot.kind, previousByID, &result, true,
		); err != nil {
			return DiscoveryResult{}, err
		}
	}
	if err := discoverer.scanRootFile(
		ctx, root, "session_index.jsonl", SourceKindSessionIndex, previousByID, &result,
	); err != nil {
		return DiscoveryResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return DiscoveryResult{}, err
	}

	result = removeDuplicateIdentities(result)
	sort.Slice(result.Snapshots, func(left, right int) bool {
		return result.Snapshots[left].Path < result.Snapshots[right].Path
	})
	sort.Slice(result.Issues, func(left, right int) bool {
		if result.Issues[left].Path != result.Issues[right].Path {
			return result.Issues[left].Path < result.Issues[right].Path
		}
		return result.Issues[left].Code < result.Issues[right].Code
	})
	return result, nil
}

func (discoverer *Discoverer) scanDirectory(
	ctx context.Context,
	root scanRoot,
	relativeRoot string,
	kind SourceKind,
	previousByID map[string]Snapshot,
	result *DiscoveryResult,
	allowMissing bool,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := discoverer.safePath(relativeRoot)
	if err != nil {
		return err
	}
	entries, err := root.ReadDir(relativeRoot)
	if errors.Is(err, fs.ErrNotExist) {
		if allowMissing {
			return nil
		}
		result.Issues = append(result.Issues, issueFromError(path, IssueScopeSubtree, err))
		return nil
	}
	if err != nil {
		result.Issues = append(result.Issues, issueFromError(path, IssueScopeSubtree, err))
		return nil
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		childRelative := filepath.Join(relativeRoot, entry.Name())
		childPath, err := discoverer.safePath(childRelative)
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			result.Issues = append(result.Issues, DiscoveryIssue{
				Path: childPath, Code: DiscoveryIssueUnsafeSymlink, Scope: IssueScopeSubtree,
			})
			continue
		}
		if entry.IsDir() {
			if err := discoverer.scanDirectory(
				ctx, root, childRelative, kind, previousByID, result, false,
			); err != nil {
				return err
			}
			continue
		}
		if filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		_ = discoverer.probePath(root, childRelative, childPath, kind, previousByID, result)
	}
	return nil
}

func (discoverer *Discoverer) scanRootFile(
	ctx context.Context,
	root scanRoot,
	relativePath string,
	kind SourceKind,
	previousByID map[string]Snapshot,
	result *DiscoveryResult,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := discoverer.safePath(relativePath)
	if err != nil {
		return err
	}
	issueCount := len(result.Issues)
	probeErr := discoverer.probePath(root, relativePath, path, kind, previousByID, result)
	if errors.Is(probeErr, fs.ErrNotExist) {
		result.Issues = result.Issues[:issueCount]
	}
	return nil
}

func (discoverer *Discoverer) probePath(
	root scanRoot,
	relativePath string,
	absolutePath string,
	kind SourceKind,
	previousByID map[string]Snapshot,
	result *DiscoveryResult,
) error {
	probe, err := root.Probe(relativePath, PrefixLimitBytes)
	if err != nil {
		result.Issues = append(result.Issues, issueFromError(absolutePath, IssueScopeExact, err))
		return err
	}
	defer clear(probe.prefix)
	prefixHash := sha256.Sum256(probe.prefix)
	fingerprint := buildFingerprint(
		probe.deviceID, probe.inode, probe.sizeBytes, probe.mtimeNS,
		int64(len(probe.prefix)), hex.EncodeToString(prefixHash[:]),
	)
	sourceID := sourceFileID(ProviderCodex, probe.deviceID, probe.inode)
	snapshot := Snapshot{
		SourceFileID: sourceID, Provider: ProviderCodex, Kind: kind,
		Path: absolutePath, Fingerprint: fingerprint,
	}
	if previous, found := previousByID[sourceID]; found &&
		previous.Fingerprint.PrefixBytes <= int64(len(probe.prefix)) {
		comparisonHash := sha256.Sum256(probe.prefix[:previous.Fingerprint.PrefixBytes])
		snapshot.Comparison = &PrefixComparison{
			PrefixBytes:  previous.Fingerprint.PrefixBytes,
			PrefixSHA256: hex.EncodeToString(comparisonHash[:]),
		}
	}
	result.Snapshots = append(result.Snapshots, snapshot)
	return nil
}

func (discoverer *Discoverer) safePath(relativePath string) (string, error) {
	if filepath.IsAbs(relativePath) {
		return "", ErrUnsafeHome
	}
	path := filepath.Clean(filepath.Join(discoverer.home, relativePath))
	if !pathWithin(discoverer.home, path) {
		return "", ErrUnsafeHome
	}
	return path, nil
}

func pathWithin(root, candidate string) bool {
	relativePath, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relativePath == "." || (relativePath != ".." && !filepath.IsAbs(relativePath) &&
		len(relativePath) > 0 && relativePath[:1] != string(filepath.Separator) &&
		!(len(relativePath) >= 3 && relativePath[:3] == ".."+string(filepath.Separator)))
}

func removeDuplicateIdentities(result DiscoveryResult) DiscoveryResult {
	indicesByID := make(map[string][]int, len(result.Snapshots))
	for index, snapshot := range result.Snapshots {
		indicesByID[snapshot.SourceFileID] = append(indicesByID[snapshot.SourceFileID], index)
	}
	duplicate := make(map[int]struct{})
	for _, indices := range indicesByID {
		if len(indices) < 2 {
			continue
		}
		for _, index := range indices {
			duplicate[index] = struct{}{}
			result.Issues = append(result.Issues, DiscoveryIssue{
				Path: result.Snapshots[index].Path, SourceFileID: result.Snapshots[index].SourceFileID,
				Code: DiscoveryIssueDuplicateIdentity, Scope: IssueScopeExact, Retryable: true,
			})
		}
	}
	if len(duplicate) == 0 {
		return result
	}
	filtered := make([]Snapshot, 0, len(result.Snapshots)-len(duplicate))
	for index, snapshot := range result.Snapshots {
		if _, found := duplicate[index]; !found {
			filtered = append(filtered, snapshot)
		}
	}
	result.Snapshots = filtered
	return result
}

func issueFromError(path string, scope IssueScope, err error) DiscoveryIssue {
	issue := DiscoveryIssue{Path: filepath.Clean(path), Scope: scope, Retryable: true}
	switch {
	case errors.Is(err, fs.ErrPermission):
		issue.Code = DiscoveryIssuePermission
	case errors.Is(err, ErrUnsafeSource):
		issue.Code = DiscoveryIssueUnsafeSymlink
		issue.Retryable = false
	case errors.Is(err, ErrChangedDuringScan), errors.Is(err, fs.ErrNotExist):
		issue.Code = DiscoveryIssueChangedDuringScan
	case errors.Is(err, ErrUnsupportedFile):
		issue.Code = DiscoveryIssueUnsupportedFile
	default:
		issue.Code = DiscoveryIssueIO
	}
	return issue
}
