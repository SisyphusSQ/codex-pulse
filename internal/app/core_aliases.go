package app

import "github.com/SisyphusSQ/codex-pulse/internal/core"

// These aliases keep the existing runtime implementation source-compatible
// while its transport-neutral facade lives in internal/core.
type Service = core.Service
type ServiceConfig = core.ServiceConfig
type QueryObserver = core.QueryObserver
type ContractInfo = core.ContractInfo
type MethodInfo = core.MethodInfo
type MethodKind = core.MethodKind
type QuotaRefreshReceipt = core.QuotaRefreshReceipt
type SettingsUpdateRequest = core.SettingsUpdateRequest
type SettingsOnlineUpdate = core.SettingsOnlineUpdate
type SettingsRefreshUpdate = core.SettingsRefreshUpdate
type SettingsUpdatesUpdate = core.SettingsUpdatesUpdate
type SettingsUIUpdate = core.SettingsUIUpdate
type SettingsUpdateResult = core.SettingsUpdateResult
type SettingsUpdateReceipt = core.SettingsUpdateReceipt
type HomeSwitchStrategy = core.HomeSwitchStrategy
type HomeSwitchPlanRequest = core.HomeSwitchPlanRequest
type HomeSwitchPlanReceipt = core.HomeSwitchPlanReceipt
type HomeSwitchResult = core.HomeSwitchResult
type HomeSwitchReceipt = core.HomeSwitchReceipt
type RuntimeAction = core.RuntimeAction
type RuntimeActionReceipt = core.RuntimeActionReceipt
type RepairDryRunReceipt = core.RepairDryRunReceipt
type HealthProjectionLevel = core.HealthProjectionLevel
type HealthProjectionFailure = core.HealthProjectionFailure
type HealthComponentStatus = core.HealthComponentStatus
type HealthProjectionResponse = core.HealthProjectionResponse

const (
	CoreContractVersion = core.ContractVersion
	MethodQuery         = core.MethodQuery
	MethodCommand       = core.MethodCommand

	SettingsUpdateApplied           = core.SettingsUpdateApplied
	SettingsUpdateReconcileRequired = core.SettingsUpdateReconcileRequired
	HomeSwitchIndependentDatabase   = core.HomeSwitchIndependentDatabase
	HomeSwitchClearAndRebuild       = core.HomeSwitchClearAndRebuild
	HomeSwitchCompletedResult       = core.HomeSwitchCompletedResult
	HomeSwitchRolledBackResult      = core.HomeSwitchRolledBackResult
	HomeSwitchRecoveryRequired      = core.HomeSwitchRecoveryRequired
	RuntimeActionPauseBackfill      = core.RuntimeActionPauseBackfill
	RuntimeActionPauseAll           = core.RuntimeActionPauseAll
	RuntimeActionResume             = core.RuntimeActionResume
	RuntimeActionReconcile          = core.RuntimeActionReconcile

	HealthProjectionHealthy  = core.HealthProjectionHealthy
	HealthProjectionBusy     = core.HealthProjectionBusy
	HealthProjectionPaused   = core.HealthProjectionPaused
	HealthProjectionDegraded = core.HealthProjectionDegraded
	HealthProjectionBlocked  = core.HealthProjectionBlocked

	HealthProjectionFailureNone     = core.HealthProjectionFailureNone
	HealthProjectionFailureSnapshot = core.HealthProjectionFailureSnapshot
	HealthProjectionFailureEvaluate = core.HealthProjectionFailureEvaluate
	HealthProjectionFailurePersist  = core.HealthProjectionFailurePersist
	HealthProjectionFailurePanic    = core.HealthProjectionFailurePanic
)

var (
	ErrCoreService = core.ErrService
	ErrCoreQuery   = core.ErrQuery
)

func NewService(config ServiceConfig) (*Service, error) {
	return core.NewService(config)
}
