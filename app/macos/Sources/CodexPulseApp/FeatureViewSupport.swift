import CodexPulseAppSupport
import CodexPulseProtocolGenerated
import SwiftUI

struct RuntimeAwarePage<Content: View>: View {
    @ObservedObject var model: AppModel
    @ViewBuilder let content: () -> Content

    var body: some View {
        switch model.state {
        case .idle, .loading:
            PageProgressView(title: "正在连接核心组件…")
        case .recovery(let phase, let stage, _):
            ContentUnavailableView {
                Label("核心组件需要恢复", systemImage: "wrench.and.screwdriver")
            } description: {
                Text("阶段：\(phase) · 步骤：\(stage)")
            } actions: {
                Button("重试恢复") { model.retryRecovery() }
            }
        case .restartRequired:
            ContentUnavailableView {
                Label("需要重新启动核心组件", systemImage: "arrow.triangle.2.circlepath")
            } description: {
                Text("恢复已完成，重新建立认证 UDS 连接后才能继续。")
            } actions: {
                Button("重新启动") { model.restartCore() }
            }
        case .shuttingDown:
            PageProgressView(title: "正在安全退出…")
        case .stopped:
            ContentUnavailableView("核心组件已停止", systemImage: "stop.circle")
        case .cancelled:
            ContentUnavailableView("连接已取消", systemImage: "xmark.circle")
        case .overview, .partial, .stale, .unavailable:
            VStack(spacing: 0) {
                runtimeBanner
                content()
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        }
    }

    @ViewBuilder
    private var runtimeBanner: some View {
        switch model.state {
        case .partial, .stale:
            EmptyView()
        case .unavailable(let notice):
            HStack {
                Label("核心组件不可用（\(notice.code)）", systemImage: "bolt.slash")
                    .foregroundStyle(.red)
                Spacer()
                if notice.retryable { Button("重新连接") { model.restartCore() } }
            }
            .padding(.horizontal, 18)
            .padding(.vertical, 8)
            .background(Color.red.opacity(0.08))
        default:
            EmptyView()
        }
    }
}

struct FeatureStateView<Value: Sendable, Content: View>: View {
    let state: FeatureLoadState<Value>
    let emptyTitle: String
    let emptySystemImage: String
    @ViewBuilder let content: (Value) -> Content

    var body: some View {
        Group {
            switch state {
            case .idle:
                ContentUnavailableView(emptyTitle, systemImage: emptySystemImage)
            case .loading(let previous):
                if let previous {
                    VStack(spacing: 0) {
                        StateBanner(title: "正在刷新，暂时保留上次结果", systemImage: "arrow.clockwise", color: .blue)
                        content(previous)
                    }
                } else {
                    PageProgressView(title: "正在加载…")
                }
            case .ready(let value):
                content(value)
            case .partial(let value, let notices):
                VStack(spacing: 0) {
                    StateBanner(
                        title: notices.isEmpty ? "结果不完整" : "结果不完整（\(notices.count) 项事实受影响）",
                        systemImage: "exclamationmark.triangle",
                        color: .orange
                    )
                    content(value)
                }
            case .stale(let value, _):
                VStack(spacing: 0) {
                    StateBanner(title: "这是上次成功结果，当前刷新失败", systemImage: "clock.arrow.circlepath", color: .orange)
                    content(value)
                }
            case .empty:
                ContentUnavailableView(emptyTitle, systemImage: emptySystemImage)
            case .unavailable(let notice):
                ContentUnavailableView {
                    Label("当前数据不可用", systemImage: "exclamationmark.icloud")
                } description: {
                    Text("错误代码：\(notice.code)\(notice.retryable ? "，可以重试" : "")")
                }
            case .cancelled(let previous):
                if let previous {
                    VStack(spacing: 0) {
                        StateBanner(title: "新请求已取消，保留上次结果", systemImage: "xmark.circle", color: .secondary)
                        content(previous)
                    }
                } else {
                    ContentUnavailableView("加载已取消", systemImage: "xmark.circle")
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
}

struct PageProgressView: View {
    let title: String

    var body: some View {
        VStack(spacing: 12) {
            ProgressView().controlSize(.large)
            Text(title).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityElement(children: .combine)
    }
}

struct SectionCard<Content: View>: View {
    let title: String
    @ViewBuilder let content: () -> Content

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(title).font(.headline)
            content()
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(14)
        .background(.quaternary.opacity(0.32), in: RoundedRectangle(cornerRadius: 12))
    }
}

struct KeyValueRow: View {
    let key: String
    let value: String

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            Text(key).foregroundStyle(.secondary)
            Spacer(minLength: 16)
            Text(value).multilineTextAlignment(.trailing).textSelection(.enabled)
        }
        .accessibilityElement(children: .combine)
    }
}

struct StatusPill: View {
    let text: String

    var body: some View {
        Text(text.isEmpty ? "unknown" : text)
            .font(.caption.weight(.medium))
            .padding(.horizontal, 7)
            .padding(.vertical, 3)
            .background(statusColor.opacity(0.14), in: Capsule())
            .foregroundStyle(statusColor)
            .accessibilityLabel("状态：\(text.isEmpty ? "未知" : text)")
    }

    private var statusColor: Color {
        let normalized = text.lowercased()
        if ["healthy", "current", "succeeded", "complete", "ready", "normal"].contains(normalized) { return .green }
        if ["failed", "critical", "error", "blocked", "unavailable"].contains(normalized) { return .red }
        if ["warning", "stale", "degraded", "partial", "interrupted"].contains(normalized) { return .orange }
        return .secondary
    }
}

func numericText(_ value: Codexpulse_Core_V1_NumericValue) -> String {
    value.hasValue ? value.value.formatted() : "--"
}

func costText(_ value: Codexpulse_Core_V1_NumericValue) -> String {
    guard value.hasValue else { return "--" }
    return String(format: "$%.2f", Double(value.value) / 1_000_000)
}

func bytesText(_ value: Codexpulse_Core_V1_NumericValue) -> String {
    guard value.hasValue else { return "--" }
    return ByteCountFormatter.string(fromByteCount: value.value, countStyle: .file)
}

func timestampText(_ value: Codexpulse_Core_V1_NumericValue) -> String {
    guard value.hasValue else { return "--" }
    return timestampText(value.value)
}

func timestampText(_ milliseconds: Int64) -> String {
    Date(timeIntervalSince1970: TimeInterval(milliseconds) / 1_000)
        .formatted(date: .abbreviated, time: .shortened)
}

func attributionText(_ value: Codexpulse_Core_V1_AttributionValue) -> String {
    if value.hasDisplayName, !value.displayName.isEmpty { return value.displayName }
    if value.hasID, !value.id.isEmpty { return value.id }
    return "未知"
}
