import Combine
import CodexPulseAppSupport
import Foundation

@MainActor
final class StatusBarDisplayPreferences: ObservableObject {
    private enum Key {
        static let style = "statusBar.style"
        static let showCostSummary = "statusBar.showCostSummary"
        static let showProjectRanking = "statusBar.showProjectRanking"
    }

    private let defaults: UserDefaults

    @Published var style: StatusBarStyle {
        didSet { defaults.set(style.rawValue, forKey: Key.style) }
    }

    @Published var showCostSummary: Bool {
        didSet { defaults.set(showCostSummary, forKey: Key.showCostSummary) }
    }

    @Published var showProjectRanking: Bool {
        didSet { defaults.set(showProjectRanking, forKey: Key.showProjectRanking) }
    }

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        let storedStyle = defaults.string(forKey: Key.style)
        self.style = StatusBarStyle.resolve(storedValue: storedStyle)
        self.showCostSummary = defaults.object(forKey: Key.showCostSummary) as? Bool ?? true
        self.showProjectRanking = defaults.object(forKey: Key.showProjectRanking) as? Bool ?? true
        if storedStyle != style.rawValue { defaults.set(style.rawValue, forKey: Key.style) }
    }
}
