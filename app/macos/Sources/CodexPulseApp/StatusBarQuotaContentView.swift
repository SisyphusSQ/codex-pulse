import AppKit
import CodexPulseAppSupport

@MainActor
final class StatusBarQuotaContentView: NSView {
    private let horizontalPadding: CGFloat = 5
    private let iconDiameter: CGFloat = 18
    private let iconTextSpacing: CGFloat = 6
    private var summary: StatusBarQuotaPresentation?
    private var fallbackText = "Codex Pulse --"
    private var style: StatusBarStyle = .ringSummary

    var hasSummary: Bool { summary != nil }

    var preferredWidth: CGFloat {
        guard let summary else {
            return min(132, max(72, textWidth(fallbackText, font: fallbackFont) + horizontalPadding * 2))
        }
        let summaryWidth = max(
            textWidth(summary.remainingText, font: primaryFont),
            textWidth(summary.usageText, font: secondaryFont)
        )
        return ceil(horizontalPadding * 2 + iconDiameter + iconTextSpacing + summaryWidth)
    }

    @discardableResult
    func update(
        summary: StatusBarQuotaPresentation?,
        fallbackText: String,
        style: StatusBarStyle
    ) -> Bool {
        guard self.summary != summary || self.fallbackText != fallbackText || self.style != style else {
            return false
        }
        self.summary = summary
        self.fallbackText = fallbackText
        self.style = style
        invalidateIntrinsicContentSize()
        needsDisplay = true
        return true
    }

    override var intrinsicContentSize: NSSize {
        NSSize(width: preferredWidth, height: NSStatusBar.system.thickness)
    }

    override func hitTest(_ point: NSPoint) -> NSView? {
        nil
    }

    override func draw(_ dirtyRect: NSRect) {
        super.draw(dirtyRect)
        guard let summary else {
            drawFallback()
            return
        }

        let iconRect = NSRect(
            x: horizontalPadding,
            y: floor(bounds.midY - iconDiameter / 2),
            width: iconDiameter,
            height: iconDiameter
        )
        switch style {
        case .ringSummary:
            drawRing(in: iconRect, summary: summary)
        case .openRingSummary:
            drawOpenRing(in: iconRect, summary: summary)
        case .gaugeSummary:
            drawGauge(in: iconRect, summary: summary)
        }
        drawSummary(summary, leadingX: iconRect.maxX + iconTextSpacing)
    }

    private var primaryFont: NSFont { .systemFont(ofSize: 9.5, weight: .semibold) }
    private var secondaryFont: NSFont { .systemFont(ofSize: 9, weight: .semibold) }
    private var fallbackFont: NSFont { .systemFont(ofSize: 11, weight: .medium) }

    private func drawFallback() {
        let attributes: [NSAttributedString.Key: Any] = [
            .font: fallbackFont,
            .foregroundColor: NSColor.secondaryLabelColor,
        ]
        let size = fallbackText.size(withAttributes: attributes)
        fallbackText.draw(
            at: NSPoint(x: max(horizontalPadding, bounds.midX - size.width / 2), y: bounds.midY - size.height / 2),
            withAttributes: attributes
        )
    }

    private func drawSummary(_ summary: StatusBarQuotaPresentation, leadingX: CGFloat) {
        let primaryAttributes: [NSAttributedString.Key: Any] = [
            .font: primaryFont,
            .foregroundColor: detailColor(summary),
        ]
        let secondaryAttributes: [NSAttributedString.Key: Any] = [
            .font: secondaryFont,
            .foregroundColor: detailColor(summary),
        ]
        let primaryHeight = summary.remainingText.size(withAttributes: primaryAttributes).height
        let secondaryHeight = summary.usageText.size(withAttributes: secondaryAttributes).height
        let rowOverlap: CGFloat = 1
        let totalHeight = primaryHeight + secondaryHeight - rowOverlap
        let bottomY = floor(bounds.midY - totalHeight / 2)
        summary.usageText.draw(at: NSPoint(x: leadingX, y: bottomY), withAttributes: secondaryAttributes)
        summary.remainingText.draw(
            at: NSPoint(x: leadingX, y: bottomY + secondaryHeight - rowOverlap),
            withAttributes: primaryAttributes
        )
    }

    private func drawRing(in rect: NSRect, summary: StatusBarQuotaPresentation) {
        let ringRect = rect.insetBy(dx: 2.1, dy: 2.1)
        let track = NSBezierPath(ovalIn: ringRect)
        trackColor.setStroke()
        track.lineWidth = 4.2
        track.stroke()
        drawProgressArc(
            center: NSPoint(x: ringRect.midX, y: ringRect.midY),
            radius: ringRect.width / 2,
            startAngle: 90,
            sweepAngle: 360,
            percent: summary.remainingPercent,
            color: accentColor(summary),
            lineWidth: 4.2
        )
    }

    private func drawOpenRing(in rect: NSRect, summary: StatusBarQuotaPresentation) {
        let center = NSPoint(x: rect.midX, y: rect.midY)
        let radius = rect.width / 2 - 2.1
        let track = NSBezierPath()
        track.appendArc(
            withCenter: center,
            radius: radius,
            startAngle: 220,
            endAngle: -40,
            clockwise: true
        )
        trackColor.setStroke()
        track.lineWidth = 4.2
        track.lineCapStyle = .round
        track.stroke()
        drawProgressArc(
            center: center,
            radius: radius,
            startAngle: 220,
            sweepAngle: 260,
            percent: summary.remainingPercent,
            color: accentColor(summary),
            lineWidth: 4.2
        )
    }

    private func drawGauge(in rect: NSRect, summary: StatusBarQuotaPresentation) {
        let center = NSPoint(x: rect.midX, y: rect.minY + 4.2)
        let radius = rect.width / 2 - 1.8
        let track = NSBezierPath()
        track.appendArc(
            withCenter: center,
            radius: radius,
            startAngle: 180,
            endAngle: 0,
            clockwise: true
        )
        trackColor.setStroke()
        track.lineWidth = 3.5
        track.lineCapStyle = .round
        track.stroke()

        drawProgressArc(
            center: center,
            radius: radius,
            startAngle: 180,
            sweepAngle: 180,
            percent: summary.remainingPercent,
            color: accentColor(summary),
            lineWidth: 3.5
        )

        guard let percent = summary.remainingPercent else { return }
        let progress = CGFloat(max(0, min(100, percent))) / 100
        let angle = (180 - progress * 180) * .pi / 180
        let needleEnd = NSPoint(
            x: center.x + cos(angle) * (radius - 1.5),
            y: center.y + sin(angle) * (radius - 1.5)
        )
        let needle = NSBezierPath()
        needle.move(to: center)
        needle.line(to: needleEnd)
        detailColor(summary).setStroke()
        needle.lineWidth = 1.6
        needle.lineCapStyle = .round
        needle.stroke()
        detailColor(summary).setFill()
        NSBezierPath(ovalIn: NSRect(x: center.x - 1.5, y: center.y - 1.5, width: 3, height: 3)).fill()
    }

    private func drawProgressArc(
        center: NSPoint,
        radius: CGFloat,
        startAngle: CGFloat,
        sweepAngle: CGFloat,
        percent: Double?,
        color: NSColor,
        lineWidth: CGFloat
    ) {
        guard let percent else { return }
        let progress = CGFloat(max(0, min(100, percent))) / 100
        guard progress > 0 else { return }
        let progressPath = NSBezierPath()
        if progress >= 1, sweepAngle == 360 {
            progressPath.appendOval(in: NSRect(
                x: center.x - radius,
                y: center.y - radius,
                width: radius * 2,
                height: radius * 2
            ))
        } else {
            progressPath.appendArc(
                withCenter: center,
                radius: radius,
                startAngle: startAngle,
                endAngle: startAngle - sweepAngle * progress,
                clockwise: true
            )
        }
        color.setStroke()
        progressPath.lineWidth = lineWidth
        progressPath.lineCapStyle = .round
        progressPath.stroke()
    }

    private var trackColor: NSColor {
        NSColor.labelColor.withAlphaComponent(0.18)
    }

    private func accentColor(_ summary: StatusBarQuotaPresentation) -> NSColor {
        guard summary.freshness == "fresh" else { return .secondaryLabelColor }
        switch QuotaRemainingLevel(remainingPercent: summary.remainingPercent) {
        case .healthy: return NSColor.systemGreen
        case .warning: return NSColor.systemYellow
        case .critical: return NSColor.systemRed
        case .unavailable: return NSColor.secondaryLabelColor
        }
    }

    private func detailColor(_ summary: StatusBarQuotaPresentation) -> NSColor {
        switch summary.freshness {
        case "fresh": .labelColor
        case "stale": .secondaryLabelColor
        default: .tertiaryLabelColor
        }
    }

    private func textWidth(_ text: String, font: NSFont) -> CGFloat {
        text.size(withAttributes: [.font: font]).width
    }
}
