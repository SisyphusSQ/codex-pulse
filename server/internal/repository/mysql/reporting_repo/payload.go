package reporting_repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	reporting_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/reporting_dto"
)

const payloadPartBytes = 256 << 10

// ResolvePayload 只解引用内部清单；旧内联 JSON 保持原样。调用方须先关闭流式 rows。
func ResolvePayload(ctx context.Context, db *gorm.DB, payload string) (string, error) {
	if !strings.HasPrefix(payload, `{"payload_ref":`) {
		return payload, nil
	}
	ref, err := parsePayloadReference(payload)
	if err != nil {
		return "", err
	}
	var parts []reporting_do.PayloadPart
	if err := db.WithContext(ctx).Where("payload_key = ?", ref.PayloadKey).Order("part_index").Limit(257).Find(&parts).Error; err != nil {
		return "", err
	}
	return assemblePayload(ref, parts)
}

// ResolvePayloadParts 校验有序流已经读出的分片，不在流式 rows 上再查询数据库。
func ResolvePayloadParts(payload string, parts []reporting_do.PayloadPart) (string, error) {
	if !strings.HasPrefix(payload, `{"payload_ref":`) {
		return payload, nil
	}
	ref, err := parsePayloadReference(payload)
	if err != nil {
		return "", err
	}
	return assemblePayload(ref, parts)
}

func parsePayloadReference(payload string) (reporting_dto.PayloadReference, error) {
	var ref reporting_dto.PayloadReference
	if err := json.Unmarshal([]byte(payload), &ref, json.RejectUnknownMembers(true)); err != nil {
		return ref, err
	}
	if len(ref.PayloadKey) != 64 || ref.Bytes < 0 || ref.Bytes > reportingv1.MaxSnapshotBytes || ref.Parts != (ref.Bytes+payloadPartBytes-1)/payloadPartBytes {
		return ref, errors.New("invalid stored snapshot manifest")
	}
	return ref, nil
}

func assemblePayload(ref reporting_dto.PayloadReference, parts []reporting_do.PayloadPart) (string, error) {
	var out strings.Builder
	out.Grow(ref.Bytes)
	if len(parts) != ref.Parts {
		return "", errors.New("missing stored snapshot parts")
	}
	for i, part := range parts {
		if part.PartIndex != i || len(part.Body) != min(payloadPartBytes, ref.Bytes-i*payloadPartBytes) {
			return "", errors.New("invalid stored snapshot part")
		}
		out.Write(part.Body)
	}
	value := out.String()
	sum := sha256.Sum256([]byte(value))
	if hex.EncodeToString(sum[:]) != ref.PayloadKey {
		return "", errors.New("stored snapshot digest mismatch")
	}
	return value, nil
}

// StagePayload 每片单独提交；未发布清单不能进入业务查询。
func (r *Reporting) StagePayload(ctx context.Context, value string, complete, deleted bool) (string, error) {
	if len(value) > reportingv1.MaxSnapshotBytes {
		return "", errors.New("stored snapshot exceeds budget")
	}
	sum := sha256.Sum256([]byte(value))
	key := hex.EncodeToString(sum[:])
	count := (len(value) + payloadPartBytes - 1) / payloadPartBytes
	for i := range count {
		part := reporting_do.PayloadPart{PayloadKey: key, PartIndex: i, Body: []byte(value[i*payloadPartBytes : min((i+1)*payloadPartBytes, len(value))]), CreatedAtMS: time.Now().UnixMilli()}
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := r.engine.DB(c).Clauses(clause.OnConflict{DoNothing: true}).Create(&part).Error
		cancel()
		if err != nil {
			return "", err
		}
	}
	b, err := json.Marshal(reporting_dto.PayloadReference{PayloadKey: key, Parts: count, Bytes: len(value), Complete: complete, Deleted: deleted})
	return string(b), err
}

// PublishedUsage 同时兼容未迁移旧会话与新版本；查询不能直接扫描暂存表。
const PublishedUsage = `(SELECT u.* FROM pulse_usage u WHERE NOT EXISTS (SELECT 1 FROM pulse_session_publications p WHERE p.session_key=u.session_key) UNION ALL SELECT v.session_key,v.contribution_id,v.position,v.observed_at_ms,v.model,v.input_tokens,v.cached_tokens,v.cache_write_tokens,v.output_tokens,v.reasoning_tokens,v.total_tokens,v.cost_micro_usd,v.reported_charge_micro_usd,v.pricing_version,v.pricing_mode,v.input_price,v.cached_price,v.cache_write_price,v.output_price,v.cost_status FROM pulse_usage_versions v JOIN pulse_session_publications p ON p.session_key=v.session_key AND p.generation=v.generation) AS u`

func (r *Reporting) setPayloadOwner(ctx context.Context, id, payload string) error {
	var ref reporting_dto.PayloadReference
	if strings.HasPrefix(payload, `{"payload_ref":`) {
		if err := json.Unmarshal([]byte(payload), &ref); err != nil {
			return err
		}
		return r.engine.DB(ctx).Save(&reporting_do.PayloadOwner{ID: id, PayloadKey: ref.PayloadKey}).Error
	}
	return r.engine.DB(ctx).Where("id = ?", id).Delete(&reporting_do.PayloadOwner{}).Error
}

// CollectVersions 仅回收无发布指针的派生版本；处理租约期间不碰正在构建的数据。
func (r *Reporting) CollectVersions(ctx context.Context) error {
	db := r.engine.DB(ctx)
	active := db.Model(&reporting_do.PublicationJob{}).Select("id").Where("lease_until_ms > ?", time.Now().UnixMilli())
	var rows []reporting_do.UsageVersion
	if err := db.Where("generation NOT IN (SELECT generation FROM pulse_session_publications)").Where("NOT EXISTS (?)", active).Limit(250).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) > 0 {
		return db.Where("NOT EXISTS (?)", active).Delete(&rows).Error
	}
	var parts []reporting_do.PayloadPart
	if err := db.Select("payload_key,part_index").Where("created_at_ms < ?", time.Now().Add(-time.Hour).UnixMilli()).Where("payload_key NOT IN (SELECT payload_key FROM pulse_payload_owners)").Where("NOT EXISTS (?)", active).Limit(16).Find(&parts).Error; err != nil {
		return err
	}
	if len(parts) > 0 {
		return db.Where("payload_key NOT IN (SELECT payload_key FROM pulse_payload_owners)").Where("NOT EXISTS (?)", active).Delete(&parts).Error
	}
	return nil
}
