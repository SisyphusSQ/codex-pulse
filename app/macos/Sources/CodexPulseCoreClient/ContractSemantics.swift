import CodexPulseProtocolGenerated
import GRPCCore
import GRPCProtobuf
import SwiftProtobuf

public enum NumericState: Equatable, Sendable {
    case known(value: Int64, unit: String)
    case unknown(reason: String, unit: String)
    case absent(unit: String)

    public init(_ value: Codexpulse_Core_V1_NumericValue) {
        if value.hasValue {
            self = .known(value: value.value, unit: value.unit)
        } else if value.hasUnknownReason {
            self = .unknown(reason: value.unknownReason, unit: value.unit)
        } else {
            self = .absent(unit: value.unit)
        }
    }
}

public enum ResponseDisposition: Equatable, Sendable {
    case complete
    case partial
    case unavailable
    case unsupported(String)

    public init(status: String) {
        switch status {
        case "complete": self = .complete
        case "partial": self = .partial
        case "unavailable": self = .unavailable
        default: self = .unsupported(status)
        }
    }
}

public enum BootstrapState: Equatable, Sendable {
    case normal
    case recovery(Codexpulse_Core_V1_MigrationRecoverySnapshot)
    case unsupported(String)

    public init(_ response: Codexpulse_Core_V1_BootstrapResponse) {
        switch response.mode {
        case "normal": self = .normal
        case "recovery" where response.hasRecovery: self = .recovery(response.recovery)
        default: self = .unsupported(response.mode)
        }
    }

    public static func == (lhs: Self, rhs: Self) -> Bool {
        switch (lhs, rhs) {
        case (.normal, .normal): return true
        case (.recovery(let lhs), .recovery(let rhs)):
            return lhs.version == rhs.version && lhs.phase == rhs.phase && lhs.stage == rhs.stage &&
                lhs.code == rhs.code && lhs.currentVersion == rhs.currentVersion &&
                lhs.targetVersion == rhs.targetVersion && lhs.failedVersion == rhs.failedVersion
        case (.unsupported(let lhs), .unsupported(let rhs)): return lhs == rhs
        default: return false
        }
    }
}

public enum RecoveryTransition: Equatable, Sendable {
    case remainInRecovery(phase: String)
    case restartRequired

    public init(_ receipt: Codexpulse_Core_V1_MigrationRecoveryReceipt) {
        self = receipt.restartRequired ? .restartRequired : .remainInRecovery(phase: receipt.phase)
    }
}

public struct CoreErrorDetail: Error, Equatable, Sendable {
    public let code: String
    public let messageKey: String
    public let field: String?
    public let retryable: Bool

    public init(_ detail: Codexpulse_Core_V1_ErrorDetail) {
        self.code = detail.code
        self.messageKey = detail.messageKey
        self.field = detail.hasField ? detail.field : nil
        self.retryable = detail.retryable
    }

    public static func decode(from error: any Error) -> Self? {
        guard let rpcError = error as? RPCError,
              let status = try? rpcError.unpackGoogleRPCStatus()
        else { return nil }
        return decode(fromGoogleStatus: status)
    }

    public static func decode(fromGoogleStatus status: GoogleRPCStatus?) -> Self? {
        guard let status else { return nil }
        for wrapped in status.details {
            guard let any = wrapped.any,
                  let detail = try? Codexpulse_Core_V1_ErrorDetail(unpackingAny: any)
            else { continue }
            return Self(detail)
        }
        return nil
    }
}
