import Foundation

@MainActor
public final class CodexCardAccountRetryScheduler {
    public var isScheduled: Bool { timer != nil }

    private var timer: Timer?
    private var generation: UInt64 = 0

    public init() {}

    @discardableResult
    public func schedule(
        after delay: TimeInterval,
        action: @escaping @MainActor @Sendable () -> Void
    ) -> Bool {
        guard timer == nil else { return false }

        generation &+= 1
        let scheduledGeneration = generation
        let timer = Timer(timeInterval: delay, repeats: false) { [weak self] _ in
            MainActor.assumeIsolated {
                self?.fire(generation: scheduledGeneration, action: action)
            }
        }
        self.timer = timer
        RunLoop.main.add(timer, forMode: .common)
        return true
    }

    public func cancel() {
        timer?.invalidate()
        timer = nil
        generation &+= 1
    }

    private func fire(
        generation scheduledGeneration: UInt64,
        action: @MainActor @Sendable () -> Void
    ) {
        guard generation == scheduledGeneration else { return }
        action()
        guard generation == scheduledGeneration else { return }
        timer = nil
    }
}
