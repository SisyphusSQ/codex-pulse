import CodexPulseProtocolGenerated
import Foundation

public enum InvalidationStreamError: Error, Equatable, Sendable {
    case invalidContractVersion(String)
    case invalidTransition
    case readinessTimeout
}

public enum InvalidationStreamState: Equatable, Sendable {
    case stopped
    case connecting
    case ready
    case suspending
    case sleeping
    case waking
    case reconnecting
    case failed
}

public struct InvalidationStreamMetrics: Equatable, Sendable {
    public var reconnectCount: Int = 0
    public var sequenceGapCount: Int = 0
    public var lastReconnectMilliseconds: Double?

    public init() {}
}

public actor InvalidationStreamController {
    public typealias EventHandler = @Sendable (Codexpulse_Core_V1_QueryInvalidationEvent) async -> Void
    public typealias StreamConsumer = @Sendable (
        _ domains: [String],
        _ afterSequence: UInt64,
        _ onReady: @Sendable @escaping () async -> Void,
        _ onEvent: @Sendable @escaping (Codexpulse_Core_V1_QueryInvalidationEvent) async throws -> Void
    ) async throws -> Void
    public typealias LifecycleNotifier = @Sendable (_ event: LifecycleEvent) async throws -> Void
    public typealias TerminalFailureHandler = @Sendable () async -> Void

    private let domains: [String]
    private let eventHandler: EventHandler
    private let streamConsumer: StreamConsumer
    private let lifecycleNotifier: LifecycleNotifier
    private let terminalFailureHandler: TerminalFailureHandler
    private let maximumReconnectAttempts: Int
    private var streamTask: Task<Void, Never>?
    private var streamGeneration: UInt64 = 0
    private var desiredRunning = false
    private var reconnectAttempts = 0
    private var reconnectStartedAt: ContinuousClock.Instant?
    private var lastSequence: UInt64 = 0
    private var state: InvalidationStreamState = .stopped
    private var metrics = InvalidationStreamMetrics()

    public init(
        client: CoreClient,
        domains: [String],
        maximumReconnectAttempts: Int = 3,
        onTerminalFailure: @escaping TerminalFailureHandler = {},
        onEvent: @escaping EventHandler
    ) {
        self.domains = domains
        self.maximumReconnectAttempts = max(0, maximumReconnectAttempts)
        self.eventHandler = onEvent
        self.terminalFailureHandler = onTerminalFailure
        self.streamConsumer = { domains, afterSequence, onReady, onEvent in
            try await client.consumeInvalidations(
                domains: domains,
                afterSequence: afterSequence,
                onReady: onReady,
                onEvent: onEvent
            )
        }
        self.lifecycleNotifier = { event in
            _ = try await client.notifyLifecycle(event)
        }
    }

    public init(
        domains: [String],
        maximumReconnectAttempts: Int = 3,
        consumeInvalidations: @escaping StreamConsumer,
        notifyLifecycle: @escaping LifecycleNotifier = { _ in },
        onTerminalFailure: @escaping TerminalFailureHandler = {},
        onEvent: @escaping EventHandler
    ) {
        self.domains = domains
        self.maximumReconnectAttempts = max(0, maximumReconnectAttempts)
        self.eventHandler = onEvent
        self.streamConsumer = consumeInvalidations
        self.lifecycleNotifier = notifyLifecycle
        self.terminalFailureHandler = onTerminalFailure
    }

    public func start() {
        guard !desiredRunning, state == .stopped || state == .failed else { return }
        desiredRunning = true
        reconnectAttempts = 0
        state = .connecting
        streamGeneration &+= 1
        launchStream(generation: streamGeneration)
    }

    public func stop() {
        desiredRunning = false
        streamGeneration &+= 1
        streamTask?.cancel()
        streamTask = nil
        state = .stopped
    }

    public func prepareForSleep(sendLifecycle: Bool = true) async throws {
        guard desiredRunning,
              state == .connecting || state == .ready || state == .reconnecting
        else { throw InvalidationStreamError.invalidTransition }
        desiredRunning = false
        streamGeneration &+= 1
        let generation = streamGeneration
        streamTask?.cancel()
        streamTask = nil
        state = .suspending
        do {
            if sendLifecycle {
                try await lifecycleNotifier(.systemWillSleep)
            }
            guard generation == streamGeneration, state == .suspending else {
                throw InvalidationStreamError.invalidTransition
            }
            state = .sleeping
        } catch {
            if generation == streamGeneration, state == .suspending { state = .failed }
            throw error
        }
    }

    public func resumeAfterWake(sendLifecycle: Bool = true) async throws {
        guard state == .sleeping, !desiredRunning else {
            throw InvalidationStreamError.invalidTransition
        }
        streamGeneration &+= 1
        let generation = streamGeneration
        state = .waking
        do {
            if sendLifecycle {
                try await lifecycleNotifier(.systemDidWake)
            }
            guard generation == streamGeneration, state == .waking else {
                throw InvalidationStreamError.invalidTransition
            }
        } catch {
            if generation == streamGeneration, state == .waking { state = .sleeping }
            throw error
        }
        reconnectStartedAt = ContinuousClock().now
        metrics.reconnectCount += 1
        reconnectAttempts = 0
        desiredRunning = true
        state = .reconnecting
        launchStream(generation: generation)
    }

    public func waitUntilReady(timeout: Duration = .seconds(5)) async throws {
        let clock = ContinuousClock()
        let deadline = clock.now.advanced(by: timeout)
        while clock.now < deadline {
            if state == .ready { return }
            if state == .failed { throw InvalidationStreamError.readinessTimeout }
            try await Task.sleep(for: .milliseconds(10))
        }
        throw InvalidationStreamError.readinessTimeout
    }

    public func snapshot() -> (InvalidationStreamState, InvalidationStreamMetrics) {
        (state, metrics)
    }

    private func launchStream(generation: UInt64) {
        let streamConsumer = streamConsumer
        let domains = domains
        let afterSequence = lastSequence
        let controller = self
        streamTask = Task {
            do {
                try await streamConsumer(
                    domains,
                    afterSequence,
                    { await controller.streamBecameReady(generation: generation) },
                    { event in try await controller.accept(event, generation: generation) }
                )
                await controller.streamEnded(error: nil, generation: generation)
            } catch {
                await controller.streamEnded(error: error, generation: generation)
            }
        }
    }

    private func streamBecameReady(generation: UInt64) {
        guard desiredRunning, generation == streamGeneration else { return }
        state = .ready
        if let reconnectStartedAt {
            metrics.lastReconnectMilliseconds = milliseconds(
                from: reconnectStartedAt,
                to: ContinuousClock().now
            )
            self.reconnectStartedAt = nil
        }
    }

    private func accept(
        _ event: Codexpulse_Core_V1_QueryInvalidationEvent,
        generation: UInt64
    ) async throws {
        guard desiredRunning, generation == streamGeneration else { throw CancellationError() }
        guard event.version == CodexPulseTransportContract.invalidationVersion else {
            throw InvalidationStreamError.invalidContractVersion(event.version)
        }
        if lastSequence > 0, event.sequence > lastSequence + 1 {
            metrics.sequenceGapCount += 1
        }
        if event.sequence > lastSequence { lastSequence = event.sequence }
        await eventHandler(event)
    }

    private func streamEnded(error: (any Error)?, generation: UInt64) async {
        guard generation == streamGeneration else { return }
        streamTask = nil
        guard desiredRunning else { return }
        if error is InvalidationStreamError {
            desiredRunning = false
            state = .failed
            await terminalFailureHandler()
            return
        }
        guard reconnectAttempts < maximumReconnectAttempts else {
            desiredRunning = false
            state = .failed
            await terminalFailureHandler()
            return
        }
        reconnectAttempts += 1
        metrics.reconnectCount += 1
        reconnectStartedAt = ContinuousClock().now
        state = .reconnecting
        try? await Task.sleep(for: .milliseconds(50 * reconnectAttempts))
        guard desiredRunning, generation == streamGeneration else { return }
        launchStream(generation: generation)
    }

    private func milliseconds(
        from start: ContinuousClock.Instant,
        to end: ContinuousClock.Instant
    ) -> Double {
        let components = start.duration(to: end).components
        return Double(components.seconds) * 1_000 + Double(components.attoseconds) / 1e15
    }
}
