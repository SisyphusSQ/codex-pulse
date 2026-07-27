import CodexPulseProtocolGenerated
import GRPCCore

public enum CodexPulseTransportContract {
    public static let version = "core-rpc-v2"
    public static let transport = "grpc+unix"
    public static let invalidationVersion = "query-invalidation-v2"
    public static let maximumMessageBytes = 16 * 1024 * 1024

    public static func validateHandshake(
        _ response: Codexpulse_Core_V1_HandshakeResponse
    ) throws {
        guard response.contractVersion == version else {
            throw CoreClientError.incompatibleContract(
                expected: version,
                actual: response.contractVersion
            )
        }
        guard response.transport == transport else {
            throw CoreClientError.incompatibleTransport(
                expected: transport,
                actual: response.transport
            )
        }
    }

    static let clientServiceConfig = ServiceConfig(methodConfig: [
        MethodConfig(
            names: [MethodConfig.Name(service: "")],
            maxRequestMessageBytes: maximumMessageBytes,
            maxResponseMessageBytes: maximumMessageBytes
        ),
    ])
}
