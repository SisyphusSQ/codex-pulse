import CodexPulseAppSupport
import CodexPulseProtocolGenerated
import SwiftUI

struct CodexAccountQuotaCard: View {
    let quota: Codexpulse_Core_V1_CodexAccountQuota
    let evaluatedAtMS: Int64
    let timeZoneIdentifier: String
    let localization: AppLocalization

    private var account: Codexpulse_Core_V1_CodexSubscriptionAccount { quota.account }

    private var accountPresentation: CodexSubscriptionAccountRowPresentation {
        CodexSubscriptionAccountRowPresentation(account, localization: localization)
    }

    private var windows: [Codexpulse_Core_V1_CodexAccountQuotaWindow] {
        CodexAccountQuotaWindowDisplayResolver.displayWindows(quota.windows)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            accountSummary
            windowContent
        }
        .frame(maxWidth: 340, alignment: .leading)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("account-quota.\(account.accountID)")
    }

    private var accountSummary: some View {
        HStack(spacing: 11) {
            Image(systemName: "person.crop.circle.fill")
                .font(.title2)
                .foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: 3) {
                Text(account.current ? "当前账号" : "历史快照")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                HStack(alignment: .firstTextBaseline, spacing: 7) {
                    Text(accountPresentation.planText)
                        .font(.headline)
                    Text(accountPresentation.title)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
                if account.hasDisplayEmail, account.displayEmail != accountPresentation.title {
                    Text(account.displayEmail)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            }
            Spacer(minLength: 8)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 11)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .stroke(.primary.opacity(0.08), lineWidth: 1)
        }
    }

    @ViewBuilder
    private var windowContent: some View {
        if windows.isEmpty {
            SectionCard(title: "额度") {
                HStack(spacing: 10) {
                    Image(systemName: "gauge.open.with.lines.needle.33percent")
                        .foregroundStyle(.secondary)
                    VStack(alignment: .leading, spacing: 2) {
                        Text("尚未采集到可信额度")
                            .font(.subheadline.weight(.medium))
                        Text("刷新当前账号后，真实额度窗口会显示在这里。")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        } else {
            VStack(alignment: .leading, spacing: 10) {
                ForEach(Array(windows.enumerated()), id: \.offset) { _, window in
                    accountQuotaWindow(window)
                }
            }
        }
    }

    private func accountQuotaWindow(
        _ window: Codexpulse_Core_V1_CodexAccountQuotaWindow
    ) -> some View {
        let presentation = CodexAccountQuotaWindowCardPresentation(
            window,
            current: account.current,
            evaluatedAtMS: evaluatedAtMS,
            timeZoneIdentifier: timeZoneIdentifier,
            localization: localization
        )
        let progress = QuotaProgressPresentation(
            remainingPercent: presentation.remainingPercent,
            localization: localization
        )
        return SectionCard(title: presentation.title) {
            HStack(alignment: .firstTextBaseline) {
                Text(presentation.metricTitle)
                    .foregroundStyle(.secondary)
                Spacer()
                Text(progress.percentText)
                    .font(.system(size: 28, weight: .semibold, design: .rounded))
                    .monospacedDigit()
                    .foregroundStyle(quotaLevelColor(progress.level))
            }
            if presentation.remainingPercent != nil {
                ProgressView(value: progress.fraction)
                    .tint(quotaLevelColor(progress.level))
                    .accessibilityLabel(presentation.metricTitle)
                    .accessibilityValue(progress.accessibilityValue)
            }
            if let resetRemainingText = presentation.resetRemainingText {
                KeyValueRow(key: "距离重置", value: resetRemainingText)
            }
            KeyValueRow(key: "重置时间", value: presentation.resetTimeText)
                .accessibilityIdentifier(
                    "account-quota.window.\(account.accountID).\(presentation.id).reset-time"
                )
            KeyValueRow(key: "数据状态", value: presentation.dataStatusText)
            if let lastCollectedText = presentation.lastCollectedText {
                KeyValueRow(key: "最近采集", value: lastCollectedText)
            }
            if let notice = presentation.notice {
                Text(notice)
                    .font(.caption)
                    .foregroundStyle(.orange)
            }
        }
    }
}
