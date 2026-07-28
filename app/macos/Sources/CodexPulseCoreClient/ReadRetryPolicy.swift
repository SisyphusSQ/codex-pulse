import GRPCCore

private func taskSleepNanoseconds(_ duration: Duration) -> UInt64 {
    let components = duration.components
    let seconds = max(
        0,
        Double(components.seconds) + Double(components.attoseconds) / 1e18
    )
    return UInt64(seconds * 1e9)
}

public struct ReadRetryPolicy: Sendable {
    public static let transportDefault = ReadRetryPolicy(maximumAttempts: 2)
    public static let none = ReadRetryPolicy(maximumAttempts: 1)

    public let maximumAttempts: Int
    public let backoff: Duration

    public init(maximumAttempts: Int, backoff: Duration = .milliseconds(50)) {
        self.maximumAttempts = max(1, maximumAttempts)
        self.backoff = backoff
    }

    func execute<Output: Sendable>(
        _ operation: @Sendable () async throws -> Output
    ) async throws -> Output {
        var attempt = 1
        while true {
            do {
                return try await operation()
            } catch let error as RPCError
                where error.code == .unavailable && attempt < maximumAttempts {
                try Task.checkCancellation()
                attempt += 1
                try await Task.sleep(nanoseconds: taskSleepNanoseconds(backoff))
            } catch {
                throw error
            }
        }
    }
}
