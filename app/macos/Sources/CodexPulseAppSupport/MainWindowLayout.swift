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

public enum SessionsProjectsSplitLayout {
    public static let detailMinimumWidth = 340.0
    public static let detailWidthFraction = 1.0 / 3.0

    public static func initialDividerPosition(
        availableWidth: Double,
        listMinimumWidth: Double,
        dividerThickness: Double
    ) -> Double? {
        let availableWidth = max(0, availableWidth)
        let dividerThickness = max(0, dividerThickness)
        guard availableWidth >= listMinimumWidth + detailMinimumWidth + dividerThickness else {
            return nil
        }
        let detailWidth = max(detailMinimumWidth, availableWidth * detailWidthFraction)
        return availableWidth - detailWidth - dividerThickness
    }
}
