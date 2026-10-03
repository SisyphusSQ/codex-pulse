package reporting_srv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	jsonv1 "encoding/json"
	"encoding/json/v2"
	"errors"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	access_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/access_dto"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func (s *Reporting) acceptChunk(ctx context.Context, p access_dto.Principal, part reportingv1.SessionSnapshot, now int64) error {
	key := reportingv1.Key(p.ID, part.Provider, part.HomeID, part.SessionID)
	previous, err := s.repository.Source(ctx, key)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if previous.ID != "" && previous.Revision >= part.Revision {
		if previous.Revision == part.Revision {
			var accepted reportingv1.SessionSnapshot
			if err := json.Unmarshal([]byte(previous.Payload), &accepted); err != nil {
				return err
			}
			body, err := jsonv1.Marshal(accepted)
			if err != nil {
				return err
			}
			hash := sha256.Sum256(body)
			if hex.EncodeToString(hash[:]) != part.Chunk.Digest {
				return utils.ErrConflict
			}
		}
		return nil
	}
	if err := s.repository.DeleteChunks(ctx, p.ID, key, part.Revision-1); err != nil {
		return err
	}
	metadata, err := s.repository.ChunkMetadata(ctx, p.ID, key, part.Revision)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(part)
	if err != nil {
		return err
	}
	if len(metadata) > 0 {
		first, err := s.repository.Chunk(ctx, p.ID, key, part.Revision, metadata[0].ChunkIndex)
		if err != nil {
			return err
		}
		var staged reportingv1.SessionSnapshot
		if err := json.Unmarshal([]byte(first.Payload), &staged); err != nil {
			return err
		}
		if staged.Chunk == nil || staged.Chunk.Count != part.Chunk.Count || staged.Chunk.Digest != part.Chunk.Digest || staged.Chunk.Contributions != part.Chunk.Contributions || staged.Chunk.Invocations != part.Chunk.Invocations {
			return utils.ErrConflict
		}
		for _, meta := range metadata {
			if meta.ChunkIndex != part.Chunk.Index {
				continue
			}
			previous, err := s.repository.Chunk(ctx, p.ID, key, part.Revision, part.Chunk.Index)
			if err != nil {
				return err
			}
			if previous.Payload != string(payload) {
				return utils.ErrConflict
			}
			return nil
		}
	}
	count, size, err := s.repository.ChunkBudget(ctx, p.ID)
	if err != nil {
		return err
	}
	if count >= 2048 || size+int64(len(payload)) > 256<<20 {
		return ErrReportingBudget
	}
	row := reporting_do.SnapshotChunk{ClientID: p.ID, SourceKey: key, Revision: part.Revision, ChunkIndex: part.Chunk.Index, Payload: string(payload), PayloadBytes: int64(len(payload)), ReceivedAtMS: now}
	if err := s.repository.SaveChunk(ctx, row); err != nil {
		return err
	}
	sizeForSnapshot := int64(len(payload))
	for _, meta := range metadata {
		sizeForSnapshot += meta.PayloadBytes
	}
	if sizeForSnapshot > reportingv1.MaxSnapshotBytes {
		return ErrReportingBudget
	}
	if len(metadata)+1 < part.Chunk.Count {
		return nil
	}
	rows, err := s.repository.Chunks(ctx, p.ID, key, part.Revision)
	if err != nil {
		return err
	}
	parts := make([]reportingv1.SessionSnapshot, 0, len(rows))
	for _, row := range rows {
		var value reportingv1.SessionSnapshot
		if err := json.Unmarshal([]byte(row.Payload), &value, json.RejectUnknownMembers(true)); err != nil {
			return err
		}
		parts = append(parts, value)
	}
	full, err := reportingv1.AssembleSnapshot(parts)
	if err != nil {
		return utils.ErrBadParamInput
	}
	if err := validateStableIDs(reportingv1.Batch{Sessions: []reportingv1.SessionSnapshot{full}}); err != nil {
		return err
	}
	if err := s.acceptSession(ctx, p, full); err != nil {
		return err
	}
	return s.repository.DeleteChunks(ctx, p.ID, key, part.Revision)
}
