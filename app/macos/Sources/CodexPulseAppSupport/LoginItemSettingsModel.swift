import Combine

public enum LoginItemRegistrationStatus: String, Equatable, Sendable {
    case notRegistered
    case enabled
    case requiresApproval
    case notFound

    public var isRequested: Bool {
        switch self {
        case .enabled, .requiresApproval:
            true
        case .notRegistered, .notFound:
            false
        }
    }
}

public enum LoginItemTransition: Equatable, Sendable {
    case idle
    case registering
    case unregistering
}

public enum LoginItemActionFailure: Equatable, Sendable {
    case registrationFailed
    case unregistrationFailed
}

@MainActor
public protocol LoginItemServiceManaging: AnyObject {
    func readStatus() -> LoginItemRegistrationStatus
    func register() throws
    func unregister() throws
    func openSystemSettings()
}

@MainActor
public final class LoginItemSettingsModel: ObservableObject {
    @Published public private(set) var status: LoginItemRegistrationStatus
    @Published public private(set) var transition: LoginItemTransition = .idle
    @Published public private(set) var actionFailure: LoginItemActionFailure?

    private let service: any LoginItemServiceManaging

    public init(service: any LoginItemServiceManaging) {
        self.service = service
        status = service.readStatus()
    }

    public var isRequested: Bool {
        status.isRequested
    }

    public var isChanging: Bool {
        transition != .idle
    }

    public func refreshStatus() {
        guard !isChanging else { return }
        let previous = status
        status = service.readStatus()
        if status != previous {
            actionFailure = nil
        }
    }

    public func setRequested(_ requested: Bool) {
        guard !isChanging else { return }

        let before = service.readStatus()
        status = before
        if requested == before.isRequested {
            actionFailure = nil
            return
        }

        transition = requested ? .registering : .unregistering
        do {
            if requested {
                try service.register()
            } else {
                try service.unregister()
            }
        } catch {
            // The system status readback below is authoritative; raw platform errors stay private.
        }

        let after = service.readStatus()
        status = after
        transition = .idle
        if requested == after.isRequested {
            actionFailure = nil
        } else {
            actionFailure = requested ? .registrationFailed : .unregistrationFailed
        }
    }

    public func openSystemSettings() {
        service.openSystemSettings()
    }
}
