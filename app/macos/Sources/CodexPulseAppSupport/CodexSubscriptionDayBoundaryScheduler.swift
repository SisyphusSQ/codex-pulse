import Foundation

@MainActor
public final class CodexSubscriptionDayBoundaryScheduler {
    public private(set) var deadline: Date?
    public private(set) var timeZoneIdentifier: String?

    private var timer: Timer?
    private var generation: UInt64 = 0

    public init() {}

    @discardableResult
    public func schedule(
        at deadline: Date,
        timeZoneIdentifier: String,
        action: @escaping @MainActor @Sendable () -> Void
    ) -> Bool {
        if timer != nil,
           self.deadline == deadline,
           self.timeZoneIdentifier == timeZoneIdentifier
        {
            return false
        }

        cancel()
        generation &+= 1
        let scheduledGeneration = generation
        self.deadline = deadline
        self.timeZoneIdentifier = timeZoneIdentifier
        let timer = Timer(fire: deadline, interval: 0, repeats: false) { [weak self] _ in
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
        deadline = nil
        timeZoneIdentifier = nil
        generation &+= 1
    }

    private func fire(
        generation scheduledGeneration: UInt64,
        action: @MainActor @Sendable () -> Void
    ) {
        guard generation == scheduledGeneration else { return }
        timer = nil
        deadline = nil
        timeZoneIdentifier = nil
        action()
    }
}
