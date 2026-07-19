package lightindex

import (
	"context"
	"errors"

	"github.com/SisyphusSQ/codex-pulse/internal/codex/logs"
	"github.com/SisyphusSQ/codex-pulse/internal/indexer"
	"github.com/SisyphusSQ/codex-pulse/internal/store"
)

type DeepIndexResult struct {
	SessionID       string `json:"session_id"`
	LoadedTurnCount int    `json:"loaded_turn_count"`
	Reused          bool   `json:"reused"`
}

// DeepIndexSession runs the existing strict rollout parser for one explicitly
// requested session. It persists only safe session/Turn projections; local
// quota observations are deliberately skipped and no source bytes are stored.
func (runtime *Runtime) DeepIndexSession(ctx context.Context, sessionID string) (DeepIndexResult, error) {
	if runtime == nil || runtime.repository == nil || ctx == nil || sessionID == "" {
		return DeepIndexResult{}, errors.New("invalid deep session index request")
	}
	runtime.deepMu.Lock()
	defer runtime.deepMu.Unlock()
	state, err := runtime.repository.LightIndexState(ctx)
	if err != nil {
		return DeepIndexResult{}, err
	}
	sessions, err := runtime.repository.ListLightSessions(ctx)
	if err != nil {
		return DeepIndexResult{}, err
	}
	var session *store.LightSessionMetadata
	for index := range sessions {
		if sessions[index].SessionID == sessionID {
			session = &sessions[index]
			break
		}
	}
	if session == nil || session.RolloutPath == nil {
		return DeepIndexResult{}, store.ErrNotFound
	}
	discoverer, err := logs.NewConfirmedDiscoverer(state.Home.Path, state.Home.DeviceID, state.Home.Inode)
	if err != nil {
		return DeepIndexResult{}, err
	}
	snapshot, err := discoverer.Inspect(ctx, *session.RolloutPath, nil)
	if err != nil {
		return DeepIndexResult{}, err
	}
	if file, lookupErr := runtime.repository.SourceFile(ctx, snapshot.SourceFileID); lookupErr == nil &&
		file.State == store.SourceFileActive && file.SessionID != nil && *file.SessionID == sessionID &&
		file.CurrentPath == snapshot.Path && file.DeviceID == snapshot.Fingerprint.DeviceID &&
		file.Inode == snapshot.Fingerprint.Inode && file.SizeBytes == snapshot.Fingerprint.SizeBytes &&
		file.MTimeNS == snapshot.Fingerprint.MTimeNS && file.ParsedOffset == snapshot.Fingerprint.SizeBytes &&
		file.ParserVersion == logs.ParserVersion {
		return runtime.deepIndexResult(ctx, sessionID, true)
	} else if lookupErr != nil && !errors.Is(lookupErr, store.ErrNotFound) {
		return DeepIndexResult{}, lookupErr
	}
	ingester, err := indexer.New(runtime.repository)
	if err != nil {
		return DeepIndexResult{}, err
	}
	stream, err := ingester.Open(ctx, indexer.OpenRequest{
		Action: logs.ReconcileAction{Kind: logs.ChangeAdded, Current: &snapshot},
		AtMS:   runtime.clock().UnixMilli(), SkipQuotaFacts: true,
	})
	if err != nil {
		return DeepIndexResult{}, err
	}
	cursor, err := stream.Cursor()
	if err != nil {
		return DeepIndexResult{}, err
	}
	reader, err := logs.NewConfirmedSnapshotReader(
		state.Home.Path, state.Home.DeviceID, state.Home.Inode, DefaultTokenScanChunkBytes,
	)
	if err != nil {
		return DeepIndexResult{}, err
	}
	offset := cursor.CommittedOffset
	atMS := runtime.clock().UnixMilli()
	_, err = reader.Read(ctx, snapshot, offset, func(chunk []byte, eof bool) error {
		result, feedErr := stream.Feed(ctx, chunk, eof, atMS)
		if feedErr != nil {
			return feedErr
		}
		offset = result.ReadOffset
		atMS++
		return nil
	})
	if err != nil {
		return DeepIndexResult{}, err
	}
	return runtime.deepIndexResult(ctx, sessionID, false)
}

func (runtime *Runtime) deepIndexResult(ctx context.Context, sessionID string, reused bool) (DeepIndexResult, error) {
	snapshot, err := runtime.repository.SessionAnalytics(ctx, store.SessionAnalyticsDetailFilter{
		SessionID: sessionID, TurnLimit: 50,
	})
	if err != nil {
		return DeepIndexResult{}, err
	}
	return DeepIndexResult{SessionID: sessionID, LoadedTurnCount: len(snapshot.Turns), Reused: reused}, nil
}
