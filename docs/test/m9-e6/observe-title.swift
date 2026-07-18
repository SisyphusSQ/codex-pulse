import ApplicationServices
import Foundation

private var observedNotification: String?

private func copyString(_ element: AXUIElement, _ attribute: CFString) -> String? {
    var value: CFTypeRef?
    guard AXUIElementCopyAttributeValue(element, attribute, &value) == .success else { return nil }
    return value as? String
}

private func findStatusItem(_ element: AXUIElement, depth: Int = 0) -> AXUIElement? {
    guard depth < 8 else { return nil }
    if copyString(element, kAXRoleAttribute as CFString) == "AXMenuBarItem",
       copyString(element, kAXTitleAttribute as CFString)?.contains("Codex Pulse") == true {
        return element
    }
    var value: CFTypeRef?
    guard AXUIElementCopyAttributeValue(element, kAXChildrenAttribute as CFString, &value) == .success,
          let children = value as? [AXUIElement] else { return nil }
    for child in children {
        if let match = findStatusItem(child, depth: depth + 1) { return match }
    }
    return nil
}

private let callback: AXObserverCallback = { _, _, notification, _ in
    let name = notification as String
    if name == kAXTitleChangedNotification as String || name == kAXAnnouncementRequestedNotification as String {
        observedNotification = name
    }
}

guard CommandLine.arguments.count == 4,
      let pid = pid_t(CommandLine.arguments[1]) else {
    fputs("usage: observe-title PID CONTINUE_FILE OUTPUT_FILE\n", stderr)
    exit(2)
}
let continueFile = CommandLine.arguments[2]
let outputFile = CommandLine.arguments[3]
let app = AXUIElementCreateApplication(pid)
var statusItem: AXUIElement?
let discoveryDeadline = Date().addingTimeInterval(5)
while statusItem == nil && Date() < discoveryDeadline {
    statusItem = findStatusItem(app)
    if statusItem == nil { RunLoop.current.run(until: Date().addingTimeInterval(0.05)) }
}
guard let statusItem else {
    fputs("AXMenuBarItem was not found\n", stderr)
    exit(1)
}
var observer: AXObserver?
guard AXObserverCreate(pid, callback, &observer) == .success, let observer else {
    fputs("AXObserverCreate failed\n", stderr)
    exit(1)
}
let registrations = [
    AXObserverAddNotification(observer, statusItem, kAXTitleChangedNotification as CFString, nil),
    AXObserverAddNotification(observer, statusItem, kAXAnnouncementRequestedNotification as CFString, nil),
    AXObserverAddNotification(observer, app, kAXAnnouncementRequestedNotification as CFString, nil),
]
guard registrations.contains(.success) else {
    fputs("AXObserverAddNotification failed: \(registrations)\n", stderr)
    exit(1)
}
CFRunLoopAddSource(CFRunLoopGetCurrent(), AXObserverGetRunLoopSource(observer), .defaultMode)
guard FileManager.default.createFile(atPath: continueFile, contents: Data()) else {
    fputs("continue signal creation failed\n", stderr)
    exit(1)
}
let notificationDeadline = Date().addingTimeInterval(5)
while observedNotification == nil && Date() < notificationDeadline {
    RunLoop.current.run(until: Date().addingTimeInterval(0.05))
}
guard let observedNotification else {
    fputs("AX title/announcement notification was not observed\n", stderr)
    exit(1)
}
try "\(observedNotification)\n".write(toFile: outputFile, atomically: true, encoding: .utf8)
