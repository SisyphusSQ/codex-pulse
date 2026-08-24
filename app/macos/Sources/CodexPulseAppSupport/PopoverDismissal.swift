import AppKit

public enum PopoverEventOrigin: Sendable {
    case popoverWindow
    case statusItemWindow
    case otherApplicationWindow
    case unknownWindow
}

public enum PopoverDismissDecision: Sendable, Equatable {
    case ignore
    case dismiss
    case dismissAndConsume
}

public enum PopoverDismissRule {
    public static let escapeKeyCode: UInt16 = 53

    public static let localEventMask: NSEvent.EventTypeMask = [
        .leftMouseDown,
        .rightMouseDown,
        .otherMouseDown,
        .keyDown,
    ]

    public static let globalEventMask: NSEvent.EventTypeMask = [
        .leftMouseDown,
        .rightMouseDown,
        .otherMouseDown,
    ]

    public static func origin(
        eventWindow: NSWindow?,
        popoverWindow: NSWindow?,
        statusItemWindow: NSWindow?,
        hitsStatusItem: Bool = false
    ) -> PopoverEventOrigin {
        if hitsStatusItem {
            return .statusItemWindow
        }
        guard let eventWindow else { return .unknownWindow }
        if eventWindow === popoverWindow {
            return .popoverWindow
        }
        if eventWindow === statusItemWindow {
            return .statusItemWindow
        }
        return .otherApplicationWindow
    }

    public static func hitsStatusItem(
        screenPoint: NSPoint,
        statusItemFrame: NSRect
    ) -> Bool {
        statusItemFrame.contains(screenPoint)
    }

    public static func decideLocalMouse(
        origin: PopoverEventOrigin,
        isRightOrOther: Bool
    ) -> PopoverDismissDecision {
        switch origin {
        case .popoverWindow:
            return .ignore
        case .statusItemWindow:
            return isRightOrOther ? .dismiss : .ignore
        case .otherApplicationWindow:
            return .dismiss
        case .unknownWindow:
            return .ignore
        }
    }

    public static func decideLocalKey(isEscape: Bool) -> PopoverDismissDecision {
        isEscape ? .dismissAndConsume : .ignore
    }

    public static func decideGlobalMouse(
        hitsStatusItem: Bool = false
    ) -> PopoverDismissDecision {
        hitsStatusItem ? .ignore : .dismiss
    }

    public static func shouldSuppressNextShow(
        decision: PopoverDismissDecision,
        isStatusItemClickInProgress: Bool
    ) -> Bool {
        switch decision {
        case .ignore:
            return false
        case .dismiss, .dismissAndConsume:
            return isStatusItemClickInProgress
        }
    }

    public static func decideMenuTracking(
        belongsToApplicationMainMenu: Bool
    ) -> PopoverDismissDecision {
        belongsToApplicationMainMenu ? .dismiss : .ignore
    }

    public static func decideResignActive(
        hasModalSession: Bool,
        isNativeAcceptanceSmoke: Bool
    ) -> PopoverDismissDecision {
        if hasModalSession || isNativeAcceptanceSmoke {
            return .ignore
        }
        return .dismiss
    }
}

@MainActor
public protocol PopoverEventMonitoring: AnyObject {
    func addLocal(
        matching mask: NSEvent.EventTypeMask,
        handler: @escaping (NSEvent) -> NSEvent?
    ) -> Any?
    func addGlobal(
        matching mask: NSEvent.EventTypeMask,
        handler: @escaping (NSEvent) -> Void
    ) -> Any?
    func remove(_ token: Any)
}

// AppKit guarantees that local and global monitor handlers run on the main
// thread. Swift 6 imports the callbacks as Sendable while NSEvent itself is
// explicitly non-Sendable, so keep the event on that callback and assert the
// executor before entering the UI-owned handler.
private final class LocalPopoverEventHandlerBox: @unchecked Sendable {
    let handler: (NSEvent) -> NSEvent?

    init(_ handler: @escaping (NSEvent) -> NSEvent?) {
        self.handler = handler
    }

    func call(_ event: NSEvent) -> NSEvent? {
        MainActor.preconditionIsolated()
        return handler(event)
    }
}

private final class GlobalPopoverEventHandlerBox: @unchecked Sendable {
    let handler: (NSEvent) -> Void

    init(_ handler: @escaping (NSEvent) -> Void) {
        self.handler = handler
    }

    func call(_ event: NSEvent) {
        MainActor.preconditionIsolated()
        handler(event)
    }
}

@MainActor
public final class SystemPopoverEventMonitor: PopoverEventMonitoring {
    public init() {}

    public func addLocal(
        matching mask: NSEvent.EventTypeMask,
        handler: @escaping (NSEvent) -> NSEvent?
    ) -> Any? {
        let box = LocalPopoverEventHandlerBox(handler)
        return NSEvent.addLocalMonitorForEvents(matching: mask) { event in
            box.call(event)
        }
    }

    public func addGlobal(
        matching mask: NSEvent.EventTypeMask,
        handler: @escaping (NSEvent) -> Void
    ) -> Any? {
        let box = GlobalPopoverEventHandlerBox(handler)
        return NSEvent.addGlobalMonitorForEvents(matching: mask) { event in
            box.call(event)
        }
    }

    public func remove(_ token: Any) {
        NSEvent.removeMonitor(token)
    }
}

@MainActor
public final class PopoverDismissMonitorSession {
    private let eventMonitor: any PopoverEventMonitoring
    private var localToken: Any?
    private var globalToken: Any?

    public init(eventMonitor: any PopoverEventMonitoring) {
        self.eventMonitor = eventMonitor
    }

    public var liveTokenCount: Int {
        (localToken == nil ? 0 : 1) + (globalToken == nil ? 0 : 1)
    }

    public func install(
        localMask: NSEvent.EventTypeMask,
        globalMask: NSEvent.EventTypeMask,
        localHandler: @escaping (NSEvent) -> NSEvent?,
        globalHandler: @escaping (NSEvent) -> Void
    ) -> Bool {
        remove()
        guard let localToken = eventMonitor.addLocal(
            matching: localMask,
            handler: localHandler
        ) else {
            return false
        }
        self.localToken = localToken
        guard let globalToken = eventMonitor.addGlobal(
            matching: globalMask,
            handler: globalHandler
        ) else {
            remove()
            return false
        }
        self.globalToken = globalToken
        return true
    }

    public func remove() {
        if let token = localToken {
            eventMonitor.remove(token)
            localToken = nil
        }
        if let token = globalToken {
            eventMonitor.remove(token)
            globalToken = nil
        }
    }
}
