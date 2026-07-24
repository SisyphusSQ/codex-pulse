package core

import (
	"errors"
	"reflect"

	healthmodel "github.com/SisyphusSQ/codex-pulse/internal/health"
	basequery "github.com/SisyphusSQ/codex-pulse/internal/query"
)

type HealthProjectionLevel string

const (
	HealthProjectionHealthy  HealthProjectionLevel = "healthy"
	HealthProjectionBusy     HealthProjectionLevel = "busy"
	HealthProjectionPaused   HealthProjectionLevel = "paused"
	HealthProjectionDegraded HealthProjectionLevel = "degraded"
	HealthProjectionBlocked  HealthProjectionLevel = "blocked"
)

type HealthProjectionFailure string

const (
	HealthProjectionFailureNone     HealthProjectionFailure = "none"
	HealthProjectionFailureSnapshot HealthProjectionFailure = "snapshot"
	HealthProjectionFailureEvaluate HealthProjectionFailure = "evaluate"
	HealthProjectionFailurePersist  HealthProjectionFailure = "persist"
	HealthProjectionFailurePanic    HealthProjectionFailure = "panic"
)

type HealthComponentStatus struct {
	Component      string                `json:"component"`
	Level          HealthProjectionLevel `json:"level"`
	Evidence       string                `json:"evidence"`
	Reason         string                `json:"reason"`
	Impact         string                `json:"impact"`
	Protection     string                `json:"protection"`
	RecoveryAction string                `json:"recoveryAction"`
}

type HealthProjectionResponse struct {
	HasValue      bool                    `json:"hasValue"`
	Stale         bool                    `json:"stale"`
	Failure       HealthProjectionFailure `json:"failure"`
	EvaluatedAtMS basequery.NumericValue  `json:"evaluatedAtMs"`
	Level         *HealthProjectionLevel  `json:"level"`
	Primary       *HealthComponentStatus  `json:"primary"`
	Components    []HealthComponentStatus `json:"components"`
}

func mapHealthProjection(value healthmodel.Projection) (HealthProjectionResponse, error) {
	evaluatedAt, err := basequery.UnknownNumeric(basequery.NumericMilliseconds, basequery.UnknownNeverLoaded)
	if err != nil {
		return HealthProjectionResponse{}, err
	}
	response := HealthProjectionResponse{
		HasValue: value.HasValue, Stale: value.Stale,
		Failure: HealthProjectionFailure(value.Failure), EvaluatedAtMS: evaluatedAt,
		Components: []HealthComponentStatus{},
	}
	if !validHealthProjectionFailure(response.Failure) {
		return HealthProjectionResponse{}, errors.New("health projection failure is invalid")
	}
	if !value.HasValue {
		if !value.Stale || value.Failure == healthmodel.FailureNone || value.EvaluatedAtMS != 0 ||
			!reflect.DeepEqual(value.Result, healthmodel.Result{}) {
			return HealthProjectionResponse{}, errors.New("empty health projection contains a result")
		}
		return response, nil
	}
	if value.Stale != (value.Failure != healthmodel.FailureNone) {
		return HealthProjectionResponse{}, errors.New("health projection stale state is inconsistent")
	}
	if value.EvaluatedAtMS < 0 || value.EvaluatedAtMS > basequery.JavaScriptMaxSafeInteger ||
		len(value.Result.Components) != 7 {
		return HealthProjectionResponse{}, errors.New("health projection result is invalid")
	}
	evaluatedAt, err = basequery.KnownNumeric(value.EvaluatedAtMS, basequery.NumericMilliseconds)
	if err != nil {
		return HealthProjectionResponse{}, err
	}
	level := HealthProjectionLevel(value.Result.Level)
	if !validHealthProjectionLevel(level) {
		return HealthProjectionResponse{}, errors.New("health projection level is invalid")
	}
	response.EvaluatedAtMS = evaluatedAt
	response.Level = &level
	response.Components = make([]HealthComponentStatus, 0, len(value.Result.Components))
	var expectedPrimary *healthmodel.ComponentStatus
	expectedLevel := healthmodel.LevelHealthy
	for index, component := range value.Result.Components {
		if component.Component != healthProjectionComponentOrder[index] {
			return HealthProjectionResponse{}, errors.New("health projection component order is invalid")
		}
		mapped, mapErr := mapHealthComponent(component)
		if mapErr != nil {
			return HealthProjectionResponse{}, mapErr
		}
		response.Components = append(response.Components, mapped)
		if healthProjectionLevelRank(component.Level) > healthProjectionLevelRank(expectedLevel) {
			expectedLevel = component.Level
			copyOfComponent := component
			expectedPrimary = &copyOfComponent
		}
	}
	if value.Result.Level != expectedLevel || !reflect.DeepEqual(value.Result.Primary, expectedPrimary) {
		return HealthProjectionResponse{}, errors.New("health projection primary is inconsistent")
	}
	if value.Result.Primary != nil {
		primary, mapErr := mapHealthComponent(*value.Result.Primary)
		if mapErr != nil {
			return HealthProjectionResponse{}, mapErr
		}
		response.Primary = &primary
	}
	return response, nil
}

var healthProjectionComponentOrder = []healthmodel.Component{
	healthmodel.ComponentLocalIndex, healthmodel.ComponentLiveQueue,
	healthmodel.ComponentHistoryBackfill, healthmodel.ComponentOnlineQuota,
	healthmodel.ComponentStorage, healthmodel.ComponentRuntime, healthmodel.ComponentUpdater,
}

func healthProjectionLevelRank(value healthmodel.Level) int {
	switch value {
	case healthmodel.LevelBlocked:
		return 5
	case healthmodel.LevelPaused:
		return 4
	case healthmodel.LevelDegraded:
		return 3
	case healthmodel.LevelBusy:
		return 2
	case healthmodel.LevelHealthy:
		return 1
	default:
		return 0
	}
}

func mapHealthComponent(value healthmodel.ComponentStatus) (HealthComponentStatus, error) {
	mapped := HealthComponentStatus{
		Component: string(value.Component), Level: HealthProjectionLevel(value.Level),
		Evidence: string(value.Evidence), Reason: string(value.Reason), Impact: string(value.Impact),
		Protection: string(value.Protection), RecoveryAction: string(value.RecoveryAction),
	}
	if !validHealthComponent(mapped) {
		return HealthComponentStatus{}, errors.New("health projection component is invalid")
	}
	return mapped, nil
}

func validHealthComponent(value HealthComponentStatus) bool {
	if !validHealthProjectionLevel(value.Level) {
		return false
	}
	switch value.Component {
	case "local_index", "live_queue", "history_backfill", "online_quota", "storage", "runtime", "updater":
	default:
		return false
	}
	switch value.Evidence {
	case "known", "unknown", "not_configured":
	default:
		return false
	}
	return validHealthReason(value.Reason) && validHealthImpact(value.Impact) &&
		validHealthProtection(value.Protection) && validHealthRecoveryAction(value.RecoveryAction)
}

func validHealthReason(value string) bool {
	switch value {
	case "healthy", "not_configured", "index_paused", "index_draining", "index_reconciling",
		"system_sleeping", "backfill_paused", "live_queue_stalled", "backfill_stalled",
		"auth_required", "disk_low", "cpu_pressure", "memory_pressure", "updater_unavailable",
		"updater_unknown", "metrics_stale", "source_timeout", "source_unavailable",
		"source_permission", "source_corrupt", "source_stale", "job_interrupted", "job_failed",
		"job_cancelled", "store_busy", "store_disk_full", "store_read_only", "store_permission",
		"store_io", "store_corrupt", "store_unavailable", "store_unknown", "wal_pressure",
		"pricing_unavailable", "pricing_invalid", "runtime_unknown", "lifecycle_unknown",
		"source_unknown", "source_failure_streak":
		return true
	default:
		return false
	}
}

func validHealthImpact(value string) bool {
	switch value {
	case "none", "indexing_stopped", "indexing_paused", "live_data_delayed", "history_incomplete",
		"online_quota_unavailable", "storage_at_risk", "runtime_at_risk", "update_checks_unavailable":
		return true
	default:
		return false
	}
}

func validHealthProtection(value string) bool {
	switch value {
	case "none", "writes_stopped", "auto_retry_stopped", "user_pause_retained", "retry_backoff", "observation_only":
		return true
	default:
		return false
	}
}

func validHealthRecoveryAction(value string) bool {
	switch value {
	case "none", "retry", "check_source", "grant_permission", "free_space", "repair_store":
		return true
	default:
		return false
	}
}

func validHealthProjectionLevel(value HealthProjectionLevel) bool {
	switch value {
	case HealthProjectionHealthy, HealthProjectionBusy, HealthProjectionPaused,
		HealthProjectionDegraded, HealthProjectionBlocked:
		return true
	default:
		return false
	}
}

func validHealthProjectionFailure(value HealthProjectionFailure) bool {
	switch value {
	case HealthProjectionFailureNone, HealthProjectionFailureSnapshot, HealthProjectionFailureEvaluate,
		HealthProjectionFailurePersist, HealthProjectionFailurePanic:
		return true
	default:
		return false
	}
}
