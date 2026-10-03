package reporting_srv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"slices"
	"strconv"
	"strings"

	"gorm.io/gorm"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
)

func sessionCapsule(kind, id, key, client string, s reportingv1.SessionSnapshot) (row reporting_do.SessionCapsule, err error) {
	ids := make([]string, 0, len(s.Contributions))
	for _, c := range s.Contributions {
		ids = append(ids, c.ID)
	}
	slices.Sort(ids)
	sum := sha256.Sum256([]byte(strconv.FormatInt(s.HistoryStartAtMS, 10) + "\n" + strings.Join(ids, "\n")))
	row = reporting_do.SessionCapsule{Kind: kind, ID: id, SessionKey: key, ClientID: client, Complete: s.Complete, Deleted: s.Deleted, FactsDigest: hex.EncodeToString(sum[:])}
	if s.Throughput != nil {
		b, e := json.Marshal(s.Throughput)
		if e != nil {
			return row, e
		}
		row.Throughput = new(string(b))
	}
	if s.CacheUsage != nil {
		b, e := json.Marshal(s.CacheUsage)
		if e != nil {
			return row, e
		}
		row.CacheUsage = new(string(b))
	}
	return
}

// WarmCapsules 分批重建派生摘要，不覆盖上报事务已写入的较新摘要。
func (s *Reporting) WarmCapsules(ctx context.Context, limit int) (processed int, err error) {
	for range limit {
		row, err := s.repository.MissingSourceCapsule(ctx)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			break
		}
		if err != nil {
			return processed, err
		}
		var snapshot reportingv1.SessionSnapshot
		if err = json.Unmarshal([]byte(row.Payload), &snapshot); err != nil {
			return processed, err
		}
		capsule, err := sessionCapsule("source", row.ID, row.SessionKey, row.ClientID, snapshot)
		if err != nil {
			return processed, err
		}
		if err = s.repository.WarmCapsule(ctx, capsule); err != nil {
			return processed, err
		}
		processed++
	}
	for range limit {
		row, err := s.repository.MissingCanonicalCapsule(ctx)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			break
		}
		if err != nil {
			return processed, err
		}
		var snapshot reportingv1.SessionSnapshot
		if err = json.Unmarshal([]byte(row.Payload), &snapshot); err != nil {
			return processed, err
		}
		capsule, err := sessionCapsule("canonical", row.SessionKey, row.SessionKey, "", snapshot)
		if err != nil {
			return processed, err
		}
		if err = s.repository.WarmCapsule(ctx, capsule); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}
