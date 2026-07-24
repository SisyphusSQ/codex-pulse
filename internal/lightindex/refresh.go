package lightindex

const TokenParserVersion = "codex-token-count-model-v2"

type HomeIdentity struct {
	Path     string
	DeviceID string
	Inode    int64
}

type RolloutFileIdentity struct {
	Path         string
	DeviceID     string
	Inode        int64
	SizeBytes    int64
	MTimeNS      int64
	PrefixBytes  int64
	PrefixSHA256 string
	Comparison   *PrefixComparison
}

type PrefixComparison struct {
	PrefixBytes  int64
	PrefixSHA256 string
}

type ScanCheckpoint struct {
	Home           HomeIdentity
	File           RolloutFileIdentity
	ParserVersion  string
	DurableOffset  int64
	Complete       bool
	HighWater      TokenTotals
	LatestEventMS  int64
	ScanGeneration int64
}

type RefreshKind string

const (
	RefreshReuse   RefreshKind = "reuse"
	RefreshAppend  RefreshKind = "append"
	RefreshRebuild RefreshKind = "rebuild"
	RefreshDefer   RefreshKind = "defer"
)

type RefreshDecision struct {
	Kind        RefreshKind
	StartOffset int64
	Reason      string
}

func DecideRefresh(
	previous *ScanCheckpoint,
	currentHome HomeIdentity,
	currentFile RolloutFileIdentity,
	parserVersion string,
) RefreshDecision {
	if previous == nil {
		return rebuildDecision("new_file")
	}
	if !sameHome(previous.Home, currentHome) {
		return RefreshDecision{
			Kind: RefreshDefer, StartOffset: previous.DurableOffset, Reason: "home_generation_changed",
		}
	}
	if previous.ParserVersion != parserVersion {
		return rebuildDecision("parser_version_changed")
	}
	if previous.File.Path != currentFile.Path {
		return rebuildDecision("path_changed")
	}
	if previous.File.DeviceID != currentFile.DeviceID || previous.File.Inode != currentFile.Inode {
		return rebuildDecision("file_identity_changed")
	}
	if currentFile.SizeBytes < 0 || previous.DurableOffset < 0 ||
		currentFile.SizeBytes < previous.File.SizeBytes || currentFile.SizeBytes < previous.DurableOffset {
		return rebuildDecision("file_truncated")
	}
	if !samePrefix(previous.File, currentFile) {
		return rebuildDecision("prefix_changed")
	}
	if currentFile.MTimeNS < previous.File.MTimeNS {
		return rebuildDecision("mtime_rollback")
	}
	if currentFile.SizeBytes == previous.File.SizeBytes {
		if currentFile.MTimeNS != previous.File.MTimeNS {
			return rebuildDecision("same_size_rewrite")
		}
		if previous.Complete && previous.DurableOffset == currentFile.SizeBytes {
			return RefreshDecision{
				Kind: RefreshReuse, StartOffset: previous.DurableOffset, Reason: "unchanged_complete",
			}
		}
		return RefreshDecision{
			Kind: RefreshAppend, StartOffset: previous.DurableOffset, Reason: "resume_incomplete_tail",
		}
	}
	return RefreshDecision{
		Kind: RefreshAppend, StartOffset: previous.DurableOffset, Reason: "file_grew",
	}
}

func sameHome(left, right HomeIdentity) bool {
	return left.Path != "" && left.Path == right.Path &&
		left.DeviceID != "" && left.DeviceID == right.DeviceID &&
		left.Inode > 0 && left.Inode == right.Inode
}

func samePrefix(previous, current RolloutFileIdentity) bool {
	prefixBytes := current.PrefixBytes
	prefixSHA256 := current.PrefixSHA256
	if current.Comparison != nil {
		prefixBytes = current.Comparison.PrefixBytes
		prefixSHA256 = current.Comparison.PrefixSHA256
	}
	return previous.PrefixBytes >= 0 && previous.PrefixBytes == prefixBytes &&
		previous.PrefixSHA256 != "" && previous.PrefixSHA256 == prefixSHA256
}

func rebuildDecision(reason string) RefreshDecision {
	return RefreshDecision{Kind: RefreshRebuild, StartOffset: 0, Reason: reason}
}
