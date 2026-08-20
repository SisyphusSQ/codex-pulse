import CodexPulseAppSupport
import CodexPulseProtocolGenerated
import SwiftUI

func localizedCopy(
    _ value: String,
    localization: AppLocalization = AppLocalizationRegistry.shared.current
) -> String {
    localization.textValue(value)
}

func quotaLevelColor(_ level: QuotaLevel) -> Color {
    switch level {
    case .healthy: .green
    case .warning: .yellow
    case .critical: .red
    case .unavailable: .secondary
    }
}

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

struct SectionCard<Content: View>: View {
    let title: String
    let fillsProposedHeight: Bool
    @ViewBuilder let content: () -> Content

    init(
        title: String,
        fillsProposedHeight: Bool = false,
        @ViewBuilder content: @escaping () -> Content
    ) {
        self.title = title
        self.fillsProposedHeight = fillsProposedHeight
        self.content = content
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(localizedCopy(title)).font(.headline)
            content()
        }
        .frame(
            maxWidth: .infinity,
            maxHeight: fillsProposedHeight ? .infinity : nil,
            alignment: .topLeading
        )
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
            Text(localizedCopy(key)).foregroundStyle(.secondary)
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
    var localization: AppLocalization = AppLocalizationRegistry.shared.current

    var body: some View {
        switch style {
        case .columns:
            HStack(alignment: .top, spacing: 0) {
                VStack(alignment: .leading, spacing: 3) {
                    Text(verbatim: localization.textValue("输入"))
                        .foregroundStyle(.secondary)
                    Text(metricText(tokens.input, localization: localization))
                        .font(.headline)
                        .monospacedDigit()
                    Text(localization.format(
                        "token.breakdown.cached",
                        metricText(tokens.cachedInput, localization: localization)
                    ))
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
                .frame(maxWidth: .infinity, alignment: .leading)

                Divider()

                VStack(alignment: .leading, spacing: 3) {
                    Text(verbatim: localization.textValue("输出"))
                        .foregroundStyle(.secondary)
                    Text(metricText(tokens.output, localization: localization))
                        .font(.headline)
                        .monospacedDigit()
                    Text(localization.format(
                        "token.breakdown.reasoning",
                        metricText(tokens.reasoning, localization: localization)
                    ))
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
                .padding(.leading, 12)
                .frame(maxWidth: .infinity, alignment: .leading)

                Divider()

                VStack(alignment: .leading, spacing: 3) {
                    Text(verbatim: localization.textValue("总量"))
                        .foregroundStyle(.secondary)
                    Text(metricText(tokens.total, localization: localization))
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
                    Text(verbatim: localization.textValue("输入"))
                    Text(metricText(tokens.input, localization: localization)).monospacedDigit()
                    Text(verbatim: localization.textValue("输出"))
                    Text(metricText(tokens.output, localization: localization)).monospacedDigit()
                    Text(verbatim: localization.textValue("总量"))
                    Text(metricText(tokens.total, localization: localization)).monospacedDigit()
                }
                Text(localization.format(
                    "token.breakdown.cachedReasoning",
                    metricText(tokens.cachedInput, localization: localization),
                    metricText(tokens.reasoning, localization: localization)
                ))
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
        let localization = AppLocalizationRegistry.shared.current
        Text(ProductCopy.status(text))
            .font(.caption.weight(.medium))
            .padding(.horizontal, 7)
            .padding(.vertical, 3)
            .background(statusColor.opacity(0.14), in: Capsule())
            .foregroundStyle(statusColor)
            .accessibilityLabel(localization.format("status.label", ProductCopy.status(text)))
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
    let localization = AppLocalizationRegistry.shared.current
    if value.unit == "tokens" {
        return TokenQuantityFormatter.string(value.value, localization: localization)
    }
    return localization.number(value.value)
}

func costText(_ value: Codexpulse_Core_V1_NumericValue) -> String {
    guard value.hasValue else { return "--" }
    return String(
        format: "$%.2f",
        locale: AppLocalizationRegistry.shared.current.locale,
        Double(value.value) / 1_000_000
    )
}

func bytesText(_ value: Codexpulse_Core_V1_NumericValue) -> String {
    guard value.hasValue else { return "--" }
    let localization = AppLocalizationRegistry.shared.current
    let units = ["B", "KB", "MB", "GB", "TB"]
    var amount = Double(value.value)
    var unitIndex = 0
    while amount >= 1_024, unitIndex < units.count - 1 {
        amount /= 1_024
        unitIndex += 1
    }
    let number = String(format: "%.1f", locale: localization.locale, amount)
    return "\(number) \(units[unitIndex])"
}

func timestampText(_ value: Codexpulse_Core_V1_NumericValue) -> String {
    guard value.hasValue else { return "--" }
    return timestampText(value.value)
}

func timestampText(_ milliseconds: Int64) -> String {
    let formatter = DateFormatter()
    formatter.locale = AppLocalizationRegistry.shared.current.locale
    formatter.dateStyle = .medium
    formatter.timeStyle = .short
    return formatter.string(from: Date(timeIntervalSince1970: TimeInterval(milliseconds) / 1_000))
}

func attributionText(_ value: Codexpulse_Core_V1_AttributionValue) -> String {
    if value.hasDisplayName, !value.displayName.isEmpty { return value.displayName }
    let localizedOther = AppLocalizationRegistry.shared.current.textValue("其他")
    if localizedOther == "其他" { return "其他" }
    return localizedOther
}
