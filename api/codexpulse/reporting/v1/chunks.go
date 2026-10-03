package reportingv1

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
)

func validChunk(s SessionSnapshot) bool {
	c := s.Chunk
	if s.Deleted || c.Index < 0 || c.Count < 2 || c.Count > MaxSnapshotChunks || c.Index >= c.Count || len(c.Digest) != 64 || c.Contributions < 0 || c.Invocations < 0 || c.Contributions+c.Invocations > MaxSnapshotFacts || len(s.Contributions) > c.Contributions || len(s.Invocations) > c.Invocations {
		return false
	}
	_, err := hex.DecodeString(c.Digest)
	return err == nil
}

// SplitSnapshot 保留全局事实 ID、revision 与完整 capsule，正文独立且可持久重试。
func SplitSnapshot(s SessionSnapshot) ([]SessionSnapshot, error) {
	if err := ValidateSnapshot(s); err != nil {
		return nil, err
	}
	if len(s.Contributions) == 0 {
		s.Contributions = []Contribution{}
	}
	body, err := json.Marshal(s)
	if err != nil || len(body) > MaxSnapshotBytes {
		return nil, ErrInvalid
	}
	if len(s.Contributions)+len(s.Invocations) <= MaxContributions && len(body) < MaxBodyBytes-1024 {
		return []SessionSnapshot{s}, nil
	}
	hash := sha256.Sum256(body)
	const size = 1000
	count := (len(s.Contributions) + len(s.Invocations) + size - 1) / size
	if count < 2 || count > MaxSnapshotChunks {
		return nil, ErrInvalid
	}
	parts := make([]SessionSnapshot, count)
	totalBytes := 0
	for index := range count {
		part := s
		part.Chunk = &SnapshotChunk{Index: index, Count: count, Digest: hex.EncodeToString(hash[:]), Contributions: len(s.Contributions), Invocations: len(s.Invocations)}
		start, end := index*size, min((index+1)*size, len(s.Contributions)+len(s.Invocations))
		part.Contributions = s.Contributions[min(start, len(s.Contributions)):min(end, len(s.Contributions))]
		part.Invocations = s.Invocations[max(0, start-len(s.Contributions)):max(0, end-len(s.Contributions))]
		body, err := json.Marshal(part)
		totalBytes += len(body)
		if err != nil || len(body) > MaxBodyBytes-1024 || totalBytes > MaxSnapshotBytes {
			return nil, ErrInvalid
		}
		parts[index] = part
	}
	return parts, nil
}

// AssembleSnapshot 同时验证清单、元数据、计数、完整摘要及完整 capsule。
func AssembleSnapshot(parts []SessionSnapshot) (SessionSnapshot, error) {
	if len(parts) == 0 || parts[0].Chunk == nil {
		return SessionSnapshot{}, ErrInvalid
	}
	parts = slices.Clone(parts)
	slices.SortFunc(parts, func(a, b SessionSnapshot) int {
		if a.Chunk == nil || b.Chunk == nil {
			return 0
		}
		return a.Chunk.Index - b.Chunk.Index
	})
	manifest := *parts[0].Chunk
	if len(parts) != manifest.Count {
		return SessionSnapshot{}, ErrInvalid
	}
	full := parts[0]
	full.Chunk, full.Contributions, full.Invocations = nil, nil, nil
	metadata, _ := json.Marshal(full)
	totalBytes := 0
	for index, part := range parts {
		if part.Chunk == nil || !validChunk(part) {
			return SessionSnapshot{}, ErrInvalid
		}
		m := *part.Chunk
		m.Index = 0
		if m != manifest || part.Chunk.Index != index {
			return SessionSnapshot{}, ErrInvalid
		}
		encoded, err := json.Marshal(part)
		totalBytes += len(encoded)
		if err != nil || totalBytes > MaxSnapshotBytes {
			return SessionSnapshot{}, ErrInvalid
		}
		c, i := part.Contributions, part.Invocations
		part.Chunk, part.Contributions, part.Invocations = nil, nil, nil
		encoded, _ = json.Marshal(part)
		if !bytes.Equal(encoded, metadata) {
			return SessionSnapshot{}, ErrInvalid
		}
		full.Contributions = append(full.Contributions, c...)
		full.Invocations = append(full.Invocations, i...)
	}
	if len(full.Contributions) != manifest.Contributions || len(full.Invocations) != manifest.Invocations {
		return SessionSnapshot{}, ErrInvalid
	}
	// Preserve the existing empty-array encoding used by exporters.
	if full.Contributions == nil {
		full.Contributions = []Contribution{}
	}
	encoded, err := json.Marshal(full)
	if err != nil || len(encoded) > MaxSnapshotBytes {
		return SessionSnapshot{}, ErrInvalid
	}
	hash := sha256.Sum256(encoded)
	if hex.EncodeToString(hash[:]) != manifest.Digest {
		return SessionSnapshot{}, ErrInvalid
	}
	return full, ValidateSnapshot(full)
}
