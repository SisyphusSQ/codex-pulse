import CodexPulseAppSupport
import ServiceManagement

@MainActor
final class MainAppLoginItemService: LoginItemServiceManaging {
    private let service: SMAppService

    init(service: SMAppService = .mainApp) {
        self.service = service
    }

    func readStatus() -> LoginItemRegistrationStatus {
        switch service.status {
        case .notRegistered:
            .notRegistered
        case .enabled:
            .enabled
        case .requiresApproval:
            .requiresApproval
        case .notFound:
            .notFound
        @unknown default:
            .notFound
        }
    }

    func register() throws {
        try service.register()
    }

    func unregister() throws {
        try service.unregister()
    }

    func openSystemSettings() {
        SMAppService.openSystemSettingsLoginItems()
    }
}
