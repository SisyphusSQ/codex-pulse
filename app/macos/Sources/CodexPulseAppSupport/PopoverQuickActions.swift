import AppKit
import Foundation

public enum PopoverQuickActionKind: Hashable, Sendable {
    case openProject
    case copyPopoverScreenshot
}

@MainActor
public enum PopoverPasteboardPayload {
    public static func write(
        text: String,
        png: Data,
        to pasteboard: NSPasteboard
    ) -> Bool {
        let item = NSPasteboardItem()
        guard item.setString(text, forType: .string),
              item.setData(png, forType: .png)
        else { return false }

        pasteboard.clearContents()
        return pasteboard.writeObjects([item])
    }
}

public enum PopoverAccountSummaryAvailability: Equatable, Sendable {
    case available
    case empty
    case unavailable
}

public struct PopoverAccountSummaryPresentation: Equatable, Sendable {
    public let availability: PopoverAccountSummaryAvailability
    public let planText: String
    public let emailText: String
    public let accessibilityLabel: String

    public init(account: CodexAccountPresentation) {
        switch account.availability {
        case .available:
            self.availability = .available
        case .empty:
            self.availability = .empty
        case .unavailable:
            self.availability = .unavailable
        }
        self.planText = account.planText
        self.emailText = account.emailText
        self.accessibilityLabel = account.accessibilityLabel
    }
}

public extension OverviewPresentation {
    var popoverAccountSummary: PopoverAccountSummaryPresentation {
        PopoverAccountSummaryPresentation(account: account)
    }
}

public enum PopoverScreenshotClipboardText {
    public static let plainText = """
        Codex Pulse Popover 完整截图
        账号与套餐信息已隐藏
        """
}

@MainActor
public enum PopoverFullPageCapture {
    private static let maximumPixelCount = 32_000_000

    public static func renderPNG(
        rootView: NSView,
        scrollView: NSScrollView,
        onFailure: (String) -> Void = { _ in }
    ) -> Data? {
        guard scrollView.isDescendant(of: rootView),
              let documentView = scrollView.documentView
        else {
            onFailure("source")
            return nil
        }

        rootView.layoutSubtreeIfNeeded()
        scrollView.layoutSubtreeIfNeeded()

        let clipView = scrollView.contentView
        let viewport = rootView.convert(clipView.bounds, from: clipView)
            .intersection(rootView.bounds)
        guard viewport.width > 0, viewport.height > 0 else {
            onFailure("viewport")
            return nil
        }
        let bodyRegion = NSRect(
            x: rootView.bounds.minX,
            y: viewport.minY,
            width: rootView.bounds.width,
            height: viewport.height
        ).intersection(rootView.bounds)
        guard bodyRegion.width == rootView.bounds.width,
              bodyRegion.height == viewport.height
        else {
            onFailure("body_region")
            return nil
        }

        let originalOrigin = clipView.bounds.origin
        defer {
            clipView.scroll(to: originalOrigin)
            scrollView.reflectScrolledClipView(clipView)
            rootView.layoutSubtreeIfNeeded()
            rootView.displayIfNeeded()
        }

        var pieces: [NSBitmapImageRep] = []
        let (headerRect, footerRect) = fixedRegions(
            rootBounds: rootView.bounds,
            viewport: viewport,
            flipped: rootView.isFlipped
        )
        if headerRect.height > 0 {
            guard let header = bitmap(of: rootView, in: headerRect) else {
                onFailure("header")
                return nil
            }
            pieces.append(header)
        }

        let documentHeight = documentView.bounds.height
        if documentHeight <= viewport.height {
            guard let body = bitmap(of: rootView, in: bodyRegion) else {
                onFailure("body")
                return nil
            }
            pieces.append(body)
        } else {
            let viewportHeight = viewport.height
            let maximumOffset = documentHeight - viewportHeight
            var capturedHeight: CGFloat = 0

            while capturedHeight < documentHeight {
                let offset = min(capturedHeight, maximumOffset)
                var origin = clipView.bounds.origin
                origin.y = documentView.isFlipped
                    ? documentView.bounds.minY + offset
                    : documentView.bounds.maxY - viewportHeight - offset
                clipView.scroll(to: origin)
                scrollView.reflectScrolledClipView(clipView)
                rootView.layoutSubtreeIfNeeded()
                rootView.displayIfNeeded()

                let visibleEnd = min(documentHeight, offset + viewportHeight)
                let appendStart = max(capturedHeight, offset)
                let appendHeight = visibleEnd - appendStart
                guard appendHeight > 0 else {
                    onFailure("segment_geometry")
                    return nil
                }

                let segmentRect = rectFromVisualTop(
                    appendStart - offset,
                    height: appendHeight,
                    in: bodyRegion,
                    flipped: rootView.isFlipped
                )
                guard let segment = bitmap(of: rootView, in: segmentRect) else {
                    onFailure("segment")
                    return nil
                }
                pieces.append(segment)
                capturedHeight = visibleEnd
            }
        }

        if footerRect.height > 0 {
            guard let footer = bitmap(of: rootView, in: footerRect) else {
                onFailure("footer")
                return nil
            }
            pieces.append(footer)
        }
        var backgroundColor = NSColor.windowBackgroundColor.cgColor
        rootView.effectiveAppearance.performAsCurrentDrawingAppearance {
            backgroundColor = NSColor.windowBackgroundColor.cgColor
        }
        guard let png = assemblePNG(
            pieces,
            backgroundColor: backgroundColor,
            onFailure: onFailure
        ) else {
            return nil
        }
        return png
    }

    private static func fixedRegions(
        rootBounds: NSRect,
        viewport: NSRect,
        flipped: Bool
    ) -> (header: NSRect, footer: NSRect) {
        if flipped {
            return (
                NSRect(
                    x: rootBounds.minX,
                    y: rootBounds.minY,
                    width: rootBounds.width,
                    height: max(0, viewport.minY - rootBounds.minY)
                ),
                NSRect(
                    x: rootBounds.minX,
                    y: viewport.maxY,
                    width: rootBounds.width,
                    height: max(0, rootBounds.maxY - viewport.maxY)
                )
            )
        }
        return (
            NSRect(
                x: rootBounds.minX,
                y: viewport.maxY,
                width: rootBounds.width,
                height: max(0, rootBounds.maxY - viewport.maxY)
            ),
            NSRect(
                x: rootBounds.minX,
                y: rootBounds.minY,
                width: rootBounds.width,
                height: max(0, viewport.minY - rootBounds.minY)
            )
        )
    }

    private static func rectFromVisualTop(
        _ offset: CGFloat,
        height: CGFloat,
        in rect: NSRect,
        flipped: Bool
    ) -> NSRect {
        NSRect(
            x: rect.minX,
            y: flipped ? rect.minY + offset : rect.maxY - offset - height,
            width: rect.width,
            height: height
        )
    }

    private static func bitmap(
        of view: NSView,
        in rect: NSRect
    ) -> NSBitmapImageRep? {
        guard let bitmap = view.bitmapImageRepForCachingDisplay(in: rect) else {
            return nil
        }
        view.cacheDisplay(in: rect, to: bitmap)
        return bitmap
    }

    private static func assemblePNG(
        _ pieces: [NSBitmapImageRep],
        backgroundColor: CGColor,
        onFailure: (String) -> Void
    ) -> Data? {
        guard let first = pieces.first,
              first.pixelsWide > 0,
              first.pixelsHigh > 0,
              first.size.width > 0
        else {
            onFailure("assembly_source")
            return nil
        }

        let pixelWidth = first.pixelsWide
        guard pieces.allSatisfy({ $0.pixelsWide == pixelWidth }) else {
            let widths = pieces.map(\.pixelsWide).map(String.init).joined(separator: "_")
            onFailure("assembly_widths_\(widths)")
            return nil
        }
        let pixelHeight = pieces.reduce(0) { $0 + $1.pixelsHigh }
        guard pixelHeight > 0,
              pixelWidth <= maximumPixelCount / pixelHeight
        else {
            onFailure("assembly_size_\(pixelWidth)x\(pixelHeight)")
            return nil
        }

        let scale = CGFloat(pixelWidth) / first.size.width
        guard scale.isFinite, scale > 0 else {
            onFailure("assembly_scale")
            return nil
        }
        let imageSize = NSSize(
            width: CGFloat(pixelWidth) / scale,
            height: CGFloat(pixelHeight) / scale
        )
        guard let graphics = CGContext(
            data: nil,
            width: pixelWidth,
            height: pixelHeight,
            bitsPerComponent: 8,
            bytesPerRow: pixelWidth * 4,
            space: CGColorSpaceCreateDeviceRGB(),
            bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue
        )
        else {
            onFailure("assembly_context")
            return nil
        }
        let canvas = CGRect(x: 0, y: 0, width: pixelWidth, height: pixelHeight)
        graphics.setFillColor(backgroundColor)
        graphics.fill(canvas)
        graphics.setBlendMode(.normal)
        graphics.interpolationQuality = .none

        var cursor = pixelHeight
        for (index, piece) in pieces.enumerated() {
            cursor -= piece.pixelsHigh
            guard let image = piece.cgImage else {
                onFailure("assembly_piece_\(index)")
                return nil
            }
            graphics.draw(
                image,
                in: CGRect(
                    x: 0,
                    y: cursor,
                    width: pixelWidth,
                    height: piece.pixelsHigh
                )
            )
        }
        guard let image = graphics.makeImage() else {
            onFailure("assembly_image")
            return nil
        }
        let output = NSBitmapImageRep(cgImage: image)
        output.size = imageSize
        guard let png = output.representation(using: .png, properties: [:]) else {
            onFailure("assembly_png")
            return nil
        }
        return png
    }
}

public enum PopoverQuickActionResult: Equatable, Sendable {
    case success(title: String, message: String)
    case failure(title: String, message: String)

    public var title: String {
        switch self {
        case .success(let title, _), .failure(let title, _): title
        }
    }

    public var message: String {
        switch self {
        case .success(_, let message), .failure(_, let message): message
        }
    }

    public var isFailure: Bool {
        if case .failure = self { return true }
        return false
    }
}

public enum PopoverQuickActions {
    private static let projectURL = URL(
        string: "https://github.com/SisyphusSQ/codex-pulse"
    )!

    public static func openProject(
        using opener: (URL) -> Bool
    ) -> PopoverQuickActionResult {
        guard opener(projectURL) else {
            return .failure(
                title: "无法打开项目主页",
                message: "系统未能打开 GitHub 项目主页，请稍后再试。"
            )
        }
        return .success(
            title: "已打开项目主页",
            message: "已交给默认浏览器处理。"
        )
    }

    public static func copyPopoverScreenshot(
        png: Data?,
        writeClipboard: (String, Data) -> Bool
    ) -> PopoverQuickActionResult {
        guard let png else {
            return .failure(
                title: "无法复制 Popover 完整截图",
                message: "Popover 截图生成失败，未写入剪贴板。"
            )
        }
        guard writeClipboard(PopoverScreenshotClipboardText.plainText, png) else {
            return .failure(
                title: "无法复制 Popover 完整截图",
                message: "剪贴板写入失败，未复制任何数据。"
            )
        }
        return .success(
            title: "已复制 Popover 完整截图",
            message: "Popover 全部内容已复制，账号与套餐信息已隐藏。"
        )
    }
}
