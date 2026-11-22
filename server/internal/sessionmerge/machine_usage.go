package sessionmerge

import (
	"encoding/json/v2"
	"slices"
	"strings"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	reporting_do "github.com/SisyphusSQ/codex-pulse/server/internal/models/do/mysql/reporting_do"
	reporting_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/reporting_dto"
)

// DecodeSource 解码已解引用的完整快照。调用事实保留严格校验，但不参与仲裁。
func DecodeSource(row reporting_do.SessionSource) (reporting_dto.SourceSnapshot, error) {
	var snapshot reportingv1.SessionSnapshot
	if err := json.UnmarshalRead(strings.NewReader(row.Payload), &snapshot, json.RejectUnknownMembers(true)); err != nil {
		return reporting_dto.SourceSnapshot{}, err
	}
	snapshot.Invocations = nil
	return reporting_dto.SourceSnapshot{ID: row.ID, ClientID: row.ClientID, Snapshot: snapshot}, nil
}

// MachineUsageSource 以原有的来源 ID 顺序在同机器内仲裁，日期和模型筛选只作用于仲裁后的事实。
func MachineUsageSource(sessionKey, clientID string, sources []reporting_dto.SourceSnapshot) (*reporting_do.MachineUsageSource, error) {
	var own []reporting_dto.SourceSnapshot
	for _, source := range sources {
		if source.ClientID == clientID {
			own = append(own, source)
		}
	}
	slices.SortFunc(own, func(a, b reporting_dto.SourceSnapshot) int { return strings.Compare(a.ID, b.ID) })
	decision := DecideSnapshot(own, nil)
	if decision.Deleted {
		return nil, nil
	}
	snapshot := decision.Source.Snapshot
	row := &reporting_do.MachineUsageSource{
		SessionKey: sessionKey, ClientID: clientID, SourceID: decision.Source.ID,
		SessionID: snapshot.SessionID, Title: snapshot.Title, ProjectID: reportingv1.Key(clientID, "codex", snapshot.ProjectID),
		CollectedAtMS: snapshot.CollectedAtMS, Complete: snapshot.Complete, Conflict: decision.Conflict,
	}
	untimed := map[string]int64{}
	for _, fact := range snapshot.Contributions {
		at := fact.ObservedAtMS
		if at == nil {
			model := "unknown"
			if fact.Model != nil && *fact.Model != "" {
				model = *fact.Model
			}
			untimed[model]++
			continue
		}
		if row.FirstObservedAtMS == nil || *at < *row.FirstObservedAtMS {
			row.FirstObservedAtMS = at
		}
		if row.LastObservedAtMS == nil || *at > *row.LastObservedAtMS {
			row.LastObservedAtMS = at
		}
	}
	if len(untimed) != 0 {
		body, err := json.Marshal(untimed)
		if err != nil {
			return nil, err
		}
		row.UntimedModels = new(string(body))
	}
	return row, nil
}
