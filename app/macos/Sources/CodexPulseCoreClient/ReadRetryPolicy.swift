import GRPCCore

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
                try await Task.sleep(for: backoff)
            } catch {
                throw error
            }
        }
    }
}
