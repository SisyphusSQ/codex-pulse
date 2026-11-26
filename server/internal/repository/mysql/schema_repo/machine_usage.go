package schema_repo

import (
	"context"

	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	reporting_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/reporting_dto"
	"github.com/SisyphusSQ/codex-pulse/server/internal/repository/mysql/reporting_repo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/sessionmerge"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func (s *Schema) backfillMachineUsage(ctx context.Context) error {
	const sessionBatch = 64
	db := s.engine.DB(ctx)
	sources := reporting_repo.NewReporting(s.engine)
	after := ""
	for {
		var keys []string
		if err := db.Model(&reporting_do.Session{}).Where("provider = ? AND id > ?", "codex", after).
			Order("id").Limit(sessionBatch).Pluck("id", &keys).Error; err != nil {
			return err
		}
		if len(keys) == 0 {
			return nil
		}
		var projections []reporting_do.MachineUsageSource
		own := map[string][]reporting_dto.SourceSnapshot{}
		key, size := "", 0
		flush := func() error {
			for client, copies := range own {
				source, err := sessionmerge.MachineUsageSource(key, client, copies)
				if err != nil {
					return err
				}
				if source != nil {
					projections = append(projections, *source)
				}
			}
			clear(own)
			size = 0
			return nil
		}
		err := sources.StreamSourcesForSessions(ctx, keys, func(row reporting_do.SessionSource) error {
			if key != row.SessionKey {
				if err := flush(); err != nil {
					return err
				}
				key = row.SessionKey
			}
			size += len(row.Payload)
			if size > 64<<20 || len(own[row.ClientID]) >= 128 {
				return utils.ErrRequestBudget
			}
			source, err := sessionmerge.DecodeSource(row)
			if err != nil {
				return err
			}
			own[row.ClientID] = append(own[row.ClientID], source)
			return nil
		})
		if err != nil {
			return err
		}
		if err := flush(); err != nil {
			return err
		}
		// 在同一连接读完正文流，再写入这些会话的轻量投影。
		if len(projections) != 0 {
			if err := db.CreateInBatches(projections, sessionBatch).Error; err != nil {
				return err
			}
		}
		after = keys[len(keys)-1]
	}
}
