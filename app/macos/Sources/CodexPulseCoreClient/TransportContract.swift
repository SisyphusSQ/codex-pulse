import GRPCCore

public enum CodexPulseTransportContract {
    public static let version = "core-rpc-v1"
    public static let transport = "grpc+unix"
    public static let invalidationVersion = "query-invalidation-v2"
    public static let maximumMessageBytes = 16 * 1024 * 1024

    static let clientServiceConfig = ServiceConfig(methodConfig: [
        MethodConfig(
            names: [MethodConfig.Name(service: "")],
            maxRequestMessageBytes: maximumMessageBytes,
            maxResponseMessageBytes: maximumMessageBytes
        ),
    ])
}
