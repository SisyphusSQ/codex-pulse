import AppKit
import CodexPulseAppSupport
import Combine
import SwiftUI

@MainActor
final class StatusItemController: NSObject {
    private let statusItem: NSStatusItem
    private let popover = NSPopover()
    private let model: AppModel
    private var cancellables: Set<AnyCancellable> = []

    init(
        model: AppModel,
        onOpenOverview: @escaping @MainActor () -> Void,
        onQuit: @escaping @MainActor () -> Void
    ) {
        self.model = model
        self.statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        super.init()

        if let button = statusItem.button {
            let image = NSImage(
                systemSymbolName: "waveform.path.ecg",
                accessibilityDescription: "Codex Pulse"
            )
            image?.isTemplate = true
            button.image = image
            button.imagePosition = .imageLeading
            button.title = model.statusItemTitle
            button.target = self
            button.action = #selector(togglePopover(_:))
            button.sendAction(on: [.leftMouseUp])
        }

        popover.behavior = .transient
        popover.animates = true
        popover.contentSize = NSSize(width: 360, height: 380)
        popover.contentViewController = NSHostingController(rootView: MenuBarPopoverView(
            model: model,
            onOpenOverview: {
                self.popover.performClose(nil)
                onOpenOverview()
            },
            onQuit: onQuit
        ))

        model.$state
            .sink { [weak self] _ in
                self?.statusItem.button?.title = model.statusItemTitle
            }
            .store(in: &cancellables)
    }

    @objc private func togglePopover(_ sender: Any?) {
        guard let button = statusItem.button else { return }
        if popover.isShown {
            popover.performClose(sender)
        } else {
            popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
        }
    }

    func verifyNativeSurfacesForSmoke() -> Bool {
        guard let button = statusItem.button,
              popover.contentViewController != nil
        else { return false }
        popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
        let shown = popover.isShown
        popover.performClose(nil)
        return shown
    }
}

private struct MenuBarPopoverView: View {
    @ObservedObject var model: AppModel
    let onOpenOverview: @MainActor () -> Void
    let onQuit: @MainActor () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                Label("Codex Pulse", systemImage: "waveform.path.ecg")
                    .font(.headline)
                Spacer()
                Button {
                    model.refreshOrRestart()
                } label: {
                    Image(systemName: "arrow.clockwise")
                }
                .buttonStyle(.plain)
                .disabled(!model.canRefreshOrRestart)
                .help("刷新当前数据")
            }

            if let overview = model.presentation {
                ForEach(overview.quotaWindows.prefix(2)) { window in
                    HStack {
                        Text(window.title)
                        Spacer()
                        Text(window.remainingPercent.map { String(format: "%.0f%%", $0) } ?? "--")
                            .font(.title3.monospacedDigit().weight(.semibold))
                    }
                }
                Divider()
                LabeledContent("API 等价成本", value: metricText(overview.estimatedCost, cost: true))
                LabeledContent("Token", value: metricText(overview.totalTokens))
            } else {
                Text(model.statusItemTitle)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, minHeight: 120)
            }

            Spacer()
            Divider()
            HStack {
                Button("打开概览", action: onOpenOverview)
                Spacer()
                Button("退出", action: onQuit)
            }
        }
        .padding(18)
        .frame(width: 360, height: 380)
    }
}
