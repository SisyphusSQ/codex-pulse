// Package logs discovers Codex JSONL sources without parsing or persisting their contents.
package logs

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"path/filepath"
	"strings"
)

const (
	ProviderCodex    = "codex"
	PrefixLimitBytes = int64(4096)
)

var (
	ErrInvalidHome            = errors.New("invalid Codex home")
	ErrUnsafeHome             = errors.New("unsafe Codex home")
	ErrHomeChanged            = errors.New("confirmed Codex home changed")
	ErrUnsafeSource           = errors.New("unsafe source path")
	ErrChangedDuringScan      = errors.New("source changed during scan")
	ErrUnsupportedFile        = errors.New("unsupported source file")
	ErrInvalidSnapshot        = errors.New("invalid source snapshot")
	ErrSnapshotBudgetTooSmall = errors.New("snapshot read budget is smaller than required proof")
	errSnapshotReadStopped    = errors.New("snapshot read stopped by consumer")
)

// StopSnapshotRead 请求在当前chunk提交后协作式停止；reader仍会完成读后identity校验。
func StopSnapshotRead(cause error) error {
	if cause == nil {
		return errSnapshotReadStopped
	}
	return errors.Join(errSnapshotReadStopped, cause)
}

type SourceKind string

const (
	SourceKindSession         SourceKind = "session"
	SourceKindArchivedSession SourceKind = "archived_session"
	SourceKindSessionIndex    SourceKind = "session_index"
)

// Fingerprint contains only filesystem metadata and a bounded content digest.
// It never retains the sampled bytes.
type Fingerprint struct {
	DeviceID     string
	Inode        int64
	SizeBytes    int64
	MTimeNS      int64
	PrefixBytes  int64
	PrefixSHA256 string
	Digest       string
}

// Snapshot is a side-effect-free observation of one allowlisted Codex source.
type Snapshot struct {
	SourceFileID string
	Provider     string
	Kind         SourceKind
	Path         string
	Fingerprint  Fingerprint
	Comparison   *PrefixComparison
}

// PrefixComparison proves what the current file contains at the previous
// snapshot's prefix length. It is transient reconcile evidence, not source data.
type PrefixComparison struct {
	PrefixBytes  int64
	PrefixSHA256 string
}

type DiscoveryIssueCode string

const (
	DiscoveryIssuePermission        DiscoveryIssueCode = "permission"
	DiscoveryIssueIO                DiscoveryIssueCode = "io"
	DiscoveryIssueUnsafeSymlink     DiscoveryIssueCode = "unsafe_symlink"
	DiscoveryIssueUnsupportedFile   DiscoveryIssueCode = "unsupported_file"
	DiscoveryIssueChangedDuringScan DiscoveryIssueCode = "changed_during_scan"
	DiscoveryIssueDuplicateIdentity DiscoveryIssueCode = "duplicate_identity"
)

type IssueScope string

const (
	IssueScopeExact   IssueScope = "exact"
	IssueScopeSubtree IssueScope = "subtree"
)

// DiscoveryIssue is intentionally allowlisted: raw filesystem errors and file
// contents are not part of this contract.
type DiscoveryIssue struct {
	Path         string
	SourceFileID string
	Code         DiscoveryIssueCode
	Scope        IssueScope
	Retryable    bool
}

type DiscoveryResult struct {
	Snapshots []Snapshot
	Issues    []DiscoveryIssue
}

type ChangeKind string

const (
	ChangeAdded      ChangeKind = "added"
	ChangeUnchanged  ChangeKind = "unchanged"
	ChangeGrown      ChangeKind = "grown"
	ChangeTruncated  ChangeKind = "truncated"
	ChangeMoved      ChangeKind = "moved"
	ChangeReplaced   ChangeKind = "replaced"
	ChangeDeleted    ChangeKind = "deleted"
	ChangeUnreadable ChangeKind = "unreadable"
)

type ReconcileAction struct {
	Kind        ChangeKind
	Previous    *Snapshot
	Current     *Snapshot
	Issue       *DiscoveryIssue
	PathChanged bool
}

type ReconcilePlan struct {
	Actions []ReconcileAction
}

func buildFingerprint(
	deviceID string,
	inode int64,
	sizeBytes int64,
	mtimeNS int64,
	prefixBytes int64,
	prefixSHA256 string,
) Fingerprint {
	fingerprint := Fingerprint{
		DeviceID: deviceID, Inode: inode, SizeBytes: sizeBytes, MTimeNS: mtimeNS,
		PrefixBytes: prefixBytes, PrefixSHA256: prefixSHA256,
	}
	fingerprint.Digest = fingerprintDigest(fingerprint)
	return fingerprint
}

func sourceFileID(provider, deviceID string, inode int64) string {
	hasher := sha256.New()
	writeDigestString(hasher, provider)
	writeDigestString(hasher, deviceID)
	writeDigestInt64(hasher, inode)
	return provider + ":" + hex.EncodeToString(hasher.Sum(nil))
}

func fingerprintDigest(fingerprint Fingerprint) string {
	hasher := sha256.New()
	writeDigestString(hasher, fingerprint.DeviceID)
	writeDigestInt64(hasher, fingerprint.Inode)
	writeDigestInt64(hasher, fingerprint.SizeBytes)
	writeDigestInt64(hasher, fingerprint.MTimeNS)
	writeDigestInt64(hasher, fingerprint.PrefixBytes)
	writeDigestString(hasher, fingerprint.PrefixSHA256)
	return hex.EncodeToString(hasher.Sum(nil))
}

func writeDigestString(hasher hash.Hash, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = hasher.Write(length[:])
	_, _ = hasher.Write([]byte(value))
}

func writeDigestInt64(hasher hash.Hash, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	_, _ = hasher.Write(encoded[:])
}

func validateSnapshot(snapshot Snapshot) error {
	fingerprint := snapshot.Fingerprint
	if snapshot.Provider != ProviderCodex || !validSourceKind(snapshot.Kind) ||
		snapshot.SourceFileID == "" || !filepath.IsAbs(snapshot.Path) || filepath.Clean(snapshot.Path) != snapshot.Path ||
		fingerprint.DeviceID == "" || fingerprint.Inode < 0 || fingerprint.SizeBytes < 0 ||
		fingerprint.MTimeNS < 0 || fingerprint.PrefixBytes < 0 ||
		fingerprint.PrefixBytes > fingerprint.SizeBytes || fingerprint.PrefixBytes > PrefixLimitBytes ||
		!validSHA256(fingerprint.PrefixSHA256) || !validSHA256(fingerprint.Digest) {
		return ErrInvalidSnapshot
	}
	if snapshot.SourceFileID != sourceFileID(snapshot.Provider, fingerprint.DeviceID, fingerprint.Inode) ||
		fingerprint.Digest != fingerprintDigest(fingerprint) {
		return ErrInvalidSnapshot
	}
	if snapshot.Comparison != nil &&
		(snapshot.Comparison.PrefixBytes < 0 || snapshot.Comparison.PrefixBytes > fingerprint.PrefixBytes ||
			!validSHA256(snapshot.Comparison.PrefixSHA256) ||
			(snapshot.Comparison.PrefixBytes == fingerprint.PrefixBytes &&
				snapshot.Comparison.PrefixSHA256 != fingerprint.PrefixSHA256)) {
		return ErrInvalidSnapshot
	}
	return nil
}

func normalizeConfirmedHome(home string) (string, error) {
	if !filepath.IsAbs(home) {
		return "", ErrInvalidHome
	}
	return filepath.Clean(home), nil
}

func validateSnapshotForHome(confirmedHome string, snapshot Snapshot) error {
	if err := validateSnapshot(snapshot); err != nil {
		return err
	}
	if !snapshotPathAllowed(confirmedHome, snapshot.Path, snapshot.Kind) {
		return ErrInvalidSnapshot
	}
	return nil
}

func snapshotPathAllowed(confirmedHome, path string, kind SourceKind) bool {
	switch kind {
	case SourceKindSessionIndex:
		return path == filepath.Join(confirmedHome, "session_index.jsonl")
	case SourceKindSession:
		return jsonlPathWithin(filepath.Join(confirmedHome, "sessions"), path)
	case SourceKindArchivedSession:
		return jsonlPathWithin(filepath.Join(confirmedHome, "archived_sessions"), path)
	default:
		return false
	}
}

func jsonlPathWithin(root, path string) bool {
	return path != root && pathWithin(root, path) && filepath.Ext(path) == ".jsonl"
}

func validSourceKind(kind SourceKind) bool {
	switch kind {
	case SourceKindSession, SourceKindArchivedSession, SourceKindSessionIndex:
		return true
	default:
		return false
	}
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == hex.EncodeToString(decoded)
}

func validIssue(issue DiscoveryIssue) bool {
	if !filepath.IsAbs(issue.Path) || filepath.Clean(issue.Path) != issue.Path {
		return false
	}
	if (issue.Code == DiscoveryIssueDuplicateIdentity) != (issue.SourceFileID != "") {
		return false
	}
	if issue.SourceFileID != "" &&
		(!strings.HasPrefix(issue.SourceFileID, ProviderCodex+":") ||
			!validSHA256(strings.TrimPrefix(issue.SourceFileID, ProviderCodex+":"))) {
		return false
	}
	switch issue.Code {
	case DiscoveryIssuePermission, DiscoveryIssueIO, DiscoveryIssueUnsafeSymlink,
		DiscoveryIssueUnsupportedFile, DiscoveryIssueChangedDuringScan,
		DiscoveryIssueDuplicateIdentity:
	default:
		return false
	}
	return issue.Scope == IssueScopeExact || issue.Scope == IssueScopeSubtree
}

func validIssueForHome(confirmedHome string, issue DiscoveryIssue) bool {
	if !validIssue(issue) {
		return false
	}
	if issue.Path == filepath.Join(confirmedHome, "session_index.jsonl") {
		return issue.Scope == IssueScopeExact
	}
	for _, root := range []string{
		filepath.Join(confirmedHome, "sessions"),
		filepath.Join(confirmedHome, "archived_sessions"),
	} {
		if issue.Path == root {
			return issue.Scope == IssueScopeSubtree
		}
		if pathWithin(root, issue.Path) {
			return issue.Scope == IssueScopeSubtree || filepath.Ext(issue.Path) == ".jsonl"
		}
	}
	return false
}
