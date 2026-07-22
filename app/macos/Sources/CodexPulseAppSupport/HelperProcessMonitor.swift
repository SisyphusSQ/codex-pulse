import Darwin
import Dispatch

public protocol HelperProcessMonitoring: Sendable {
    func cancel()
}

public final class DispatchHelperProcessMonitor: HelperProcessMonitoring, @unchecked Sendable {
    private let source: DispatchSourceProcess

    public init(processID: Int32, onExit: @escaping @Sendable () -> Void) {
        let source = DispatchSource.makeProcessSource(
            identifier: pid_t(processID),
            eventMask: .exit,
            queue: .global(qos: .utility)
        )
        self.source = source
        source.setEventHandler(handler: onExit)
        source.resume()
    }

    public func cancel() {
        source.cancel()
    }

    deinit {
        source.cancel()
    }
}
