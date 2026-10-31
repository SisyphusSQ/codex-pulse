package lightindex

import (
	"context"

	logsource "github.com/SisyphusSQ/codex-pulse/internal/codex/logs/source"
	storelight "github.com/SisyphusSQ/codex-pulse/internal/store/lightindex"
)

// compressedScanIdentity 只缓存指纹，绝不跨扫描保留解压正文。
type compressedScanIdentity struct{ path, encoded, decoded string }

type decodedInspector struct{ snapshot logsource.Snapshot }

func (i decodedInspector) Inspect(context.Context, string, *logsource.Snapshot) (logsource.Snapshot, error) {
	return i.snapshot, nil
}
func (i decodedInspector) Unchanged(context.Context, string, logsource.Snapshot) (bool, error) {
	return false, nil
}

func (runtime *Runtime) scanCompressedSession(ctx context.Context, home storelight.LightHomeIdentity, session storelight.LightSessionMetadata, discoverer rolloutInspector, reader *logsource.SnapshotReader, cached *storelight.LightSessionScanSnapshot, budget *scanSliceBudget) (bool, error) {
	physical, err := discoverer.Inspect(ctx, *session.RolloutPath, nil)
	if err != nil {
		return false, err
	}
	previous := runtime.compressedScans[session.SessionID]
	if cached != nil && cached.Pending == nil && cached.Active != nil && session.ScanState == "complete" &&
		cached.Active.ParserVersion == TokenParserVersion && previous.path == physical.Path &&
		previous.encoded == physical.Fingerprint.Digest && previous.decoded == cached.Active.Identity.FingerprintSHA256 {
		if budget != nil {
			budget.unchanged++
		}
		return false, nil
	}
	view, logical, err := reader.OpenZstdSnapshot(ctx, physical)
	if err != nil {
		return false, err
	}
	defer view.Close()
	// 压缩副本不能按物理字节追加；完整验证流后分批建索引，最后原子发布。
	// 逻辑 offset/size 与普通 JSONL 相同，文件身份核对仍基于压缩源快照。
	published, err := runtime.scanSessionData(ctx, home, session, decodedInspector{logical}, view, cached, nil)
	if err == nil && published {
		if runtime.compressedScans == nil {
			runtime.compressedScans = make(map[string]compressedScanIdentity)
		}
		runtime.compressedScans[session.SessionID] = compressedScanIdentity{physical.Path, physical.Fingerprint.Digest, logical.Fingerprint.Digest}
	}
	return published, err
}
