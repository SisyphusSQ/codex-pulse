import Foundation

public struct MainWindowContentSize: Equatable, Sendable {
    public let width: Double
    public let height: Double

    public init(width: Double, height: Double) {
        self.width = width
        self.height = height
    }
}

public enum MainWindowLayout {
    public static let preferredContentSize = MainWindowContentSize(width: 1_440, height: 900)

    public static func initialContentSize(
        visibleFrameWidth: Double,
        visibleFrameHeight: Double,
        frameChromeWidth: Double,
        frameChromeHeight: Double
    ) -> MainWindowContentSize {
        MainWindowContentSize(
            width: min(
                preferredContentSize.width,
                max(0, visibleFrameWidth - max(0, frameChromeWidth))
            ),
            height: min(
                preferredContentSize.height,
                max(0, visibleFrameHeight - max(0, frameChromeHeight))
            )
        )
    }
}
