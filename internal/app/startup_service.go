package app

import "runtime"

type StartupService struct {
	recovery *migrationRecoveryController
}

func newStartupService(recovery *migrationRecoveryController) *StartupService {
	return &StartupService{recovery: recovery}
}

func (service *StartupService) Bootstrap() BootstrapInfo {
	info := BootstrapInfo{
		Name: appName, Locale: defaultLocale, Platform: runtime.GOOS, Mode: ApplicationModeNormal,
	}
	if service != nil && service.recovery != nil {
		snapshot := service.recovery.Snapshot()
		info.Mode = ApplicationModeRecovery
		info.Recovery = &snapshot
	}
	return info
}
