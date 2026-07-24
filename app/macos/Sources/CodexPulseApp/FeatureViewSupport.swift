import CodexPulseAppSupport
import CodexPulseProtocolGenerated
import SwiftUI

struct RuntimeAwarePage<Content: View>: View {
    @ObservedObject var model: AppModel
    @ViewBuilder let content: () -> Content

    var body: some View {
        switch model.state {
        case .idle, .loading:
            PageProgressView(title: "正在连接本地数据…")
        case .recovery:
            ContentUnavailableView {
                Label("正在修复本地数据", systemImage: "wrench.and.screwdriver")
            } description: {
                Text("修复完成后会自动恢复页面内容。")
            } actions: {
                Button("重试") { model.retryRecovery() }
            }
        case .restartRequired:
            ContentUnavailableView {
                Label("需要重新连接本地数据", systemImage: "arrow.triangle.2.circlepath")
            } description: {
                Text("数据修复已经完成，重新连接后即可继续。")
            } actions: {
                Button("重新连接") { model.restartCore() }
            }
        case .shuttingDown:
            PageProgressView(title: "正在安全退出…")
        case .stopped:
            ContentUnavailableView("本地数据服务已停止", systemImage: "stop.circle")
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
                Label("本地数据暂时不可用", systemImage: "bolt.slash")
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
                    content(previous)
                } else {
                    PageProgressView(title: "正在加载…")
                }
            case .ready(let value):
                content(value)
            case .partial(let value, _):
                content(value)
            case .stale(let value, _):
                content(value)
            case .empty:
                ContentUnavailableView(emptyTitle, systemImage: emptySystemImage)
            case .unavailable(let notice):
                ContentUnavailableView {
                    Label("当前数据不可用", systemImage: "exclamationmark.icloud")
                } description: {
                    Text(notice.retryable ? "请稍后重试。" : "当前版本无法读取这部分数据。")
                }
            case .cancelled(let previous):
                if let previous {
                    content(previous)
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
        .padding(16)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .stroke(.quaternary, lineWidth: 1)
        }
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

enum TokenBreakdownViewStyle {
    case columns
    case compact
}

struct TokenBreakdownView: View {
    let tokens: TokenBreakdownPresentation
    var style: TokenBreakdownViewStyle = .columns

    var body: some View {
        switch style {
        case .columns:
            HStack(alignment: .top, spacing: 0) {
                VStack(alignment: .leading, spacing: 3) {
                    Text("输入")
                        .foregroundStyle(.secondary)
                    Text(metricText(tokens.input))
                        .font(.headline)
                        .monospacedDigit()
                    Text("缓存 \(metricText(tokens.cachedInput))")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)

                Divider()

                VStack(alignment: .leading, spacing: 3) {
                    Text("输出")
                        .foregroundStyle(.secondary)
                    Text(metricText(tokens.output))
                        .font(.headline)
                        .monospacedDigit()
                    Text("推理 \(metricText(tokens.reasoning))")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
                .padding(.leading, 12)
                .frame(maxWidth: .infinity, alignment: .leading)

                Divider()

                VStack(alignment: .leading, spacing: 3) {
                    Text("总量")
                        .foregroundStyle(.secondary)
                    Text(metricText(tokens.total))
                        .font(.headline)
                        .monospacedDigit()
                }
                .padding(.leading, 12)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .font(.caption)

        case .compact:
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 10) {
                    Text("输入")
                    Text(metricText(tokens.input)).monospacedDigit()
                    Text("输出")
                    Text(metricText(tokens.output)).monospacedDigit()
                    Text("总量")
                    Text(metricText(tokens.total)).monospacedDigit()
                }
                Text("缓存 \(metricText(tokens.cachedInput)) · 推理 \(metricText(tokens.reasoning))")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
            .font(.caption)
            .foregroundStyle(.secondary)
        }
    }
}

struct StatusPill: View {
    let text: String

    var body: some View {
        Text(ProductCopy.status(text))
            .font(.caption.weight(.medium))
            .padding(.horizontal, 7)
            .padding(.vertical, 3)
            .background(statusColor.opacity(0.14), in: Capsule())
            .foregroundStyle(statusColor)
            .accessibilityLabel("状态：\(ProductCopy.status(text))")
    }

    private var statusColor: Color {
        let normalized = text.lowercased()
        if ["healthy", "current", "succeeded", "complete", "ready", "normal"].contains(normalized) {
            return .green
        }
        if ["failed", "critical", "error", "blocked", "unavailable"].contains(normalized) {
            return .red
        }
        if ["warning", "stale", "degraded", "partial", "interrupted"].contains(normalized) {
            return .orange
        }
        return .secondary
    }
}

func numericText(_ value: Codexpulse_Core_V1_NumericValue) -> String {
    guard value.hasValue else { return "--" }
    if value.unit == "tokens" { return TokenQuantityFormatter.string(value.value) }
    return value.value.formatted()
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
    return "其他"
}
