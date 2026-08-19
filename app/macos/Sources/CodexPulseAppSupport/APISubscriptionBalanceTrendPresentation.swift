import CodexPulseProtocolGenerated
import Foundation

public enum APISubscriptionBalanceTrendDirection: Equatable, Sendable {
    case increase
    case decrease
    case unchanged
}

public struct APISubscriptionBalanceTrendPointPresentation: Identifiable, Equatable, Sendable {
    public let observedAtMs: Int64
    public let date: Date
    public let total: Double
    public let totalText: String
    public let grantedText: String
    public let toppedUpText: String

    public var id: Int64 { observedAtMs }
}

public struct APISubscriptionBalanceTrendPresentation: Sendable {
    public let currency: String
    public let points: [APISubscriptionBalanceTrendPointPresentation]
    public let direction: APISubscriptionBalanceTrendDirection
    public let yDomain: ClosedRange<Double>

    public init?(series: Codexpulse_Core_V1_APISubscriptionCurrencyBalanceSeries) {
        guard !series.currency.isEmpty, !series.points.isEmpty else { return nil }
        let points = series.points.compactMap { point
            -> APISubscriptionBalanceTrendPointPresentation? in
            guard point.observedAtMs > 0,
                  let total = Double(point.total), total.isFinite,
                  Double(point.granted)?.isFinite == true,
                  Double(point.toppedUp)?.isFinite == true
            else { return nil }
            return APISubscriptionBalanceTrendPointPresentation(
                observedAtMs: point.observedAtMs,
                date: Date(timeIntervalSince1970: Double(point.observedAtMs) / 1_000),
                total: total,
                totalText: point.total,
                grantedText: point.granted,
                toppedUpText: point.toppedUp
            )
        }.sorted { $0.observedAtMs < $1.observedAtMs }
        guard points.count == series.points.count,
              let first = points.first,
              let last = points.last,
              let minimum = points.map(\.total).min(),
              let maximum = points.map(\.total).max()
        else { return nil }

        currency = series.currency
        self.points = points
        direction = if last.total < first.total {
            .decrease
        } else if last.total > first.total {
            .increase
        } else {
            .unchanged
        }
        let span = maximum - minimum
        let padding = span > 0 ? span * 0.1 : max(abs(maximum) * 0.05, 0.01)
        yDomain = (minimum - padding)...(maximum + padding)
    }

    public func nearest(to selectedDate: Date?) -> APISubscriptionBalanceTrendPointPresentation? {
        guard let selectedDate else { return nil }
        let selectedAtMs = selectedDate.timeIntervalSince1970 * 1_000
        return points.min { left, right in
            let leftDistance = abs(Double(left.observedAtMs) - selectedAtMs)
            let rightDistance = abs(Double(right.observedAtMs) - selectedAtMs)
            if leftDistance == rightDistance {
                return left.observedAtMs < right.observedAtMs
            }
            return leftDistance < rightDistance
        }
    }
}
