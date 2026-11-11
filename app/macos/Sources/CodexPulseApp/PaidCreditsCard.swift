import CodexPulseAppSupport
import CodexPulseProtocolGenerated
import SwiftUI

struct PaidCreditsCard: View {
    let credit: Codexpulse_Core_V1_PaidCredits
    let localization: AppLocalization

    var body: some View {
        SectionCard(title: credit.limitID == "codex" ? "Credits" : "Credits · \(credit.limitID)") {
            KeyValueRow(key: "可用余额", value: PaidCreditsPresentation.balance(credit, localization: localization))
                .accessibilityIdentifier("credits.balance")
            if credit.observedAtMs > 0 {
                KeyValueRow(key: "最后观测", value: timestampText(credit.observedAtMs))
            }
            if credit.freshness == "conflict" {
                Text("同一时刻的余额来源冲突，余额保持未知。")
                    .font(.caption).foregroundStyle(.secondary)
            } else if !credit.hasBalance && !(credit.hasUnlimited && credit.unlimited) {
                Text("尚未取得官方余额。")
                    .font(.caption).foregroundStyle(.secondary)
            } else if credit.freshness != "fresh" {
                Text("余额暂未更新，保留最后成功观测。")
                    .font(.caption).foregroundStyle(.secondary)
            }
            KeyValueRow(key: "今日 / 近7天 / 近30天消耗", value: "--")
            Text("官方暂未提供实际消耗统计，余额变化不等于实际扣费。")
                .font(.caption).foregroundStyle(.secondary)
            if !credit.history.isEmpty {
                DisclosureGroup("近30天余额历史") {
                    ForEach(Array(credit.history.enumerated()), id: \.offset) { _, point in
                        KeyValueRow(key: timestampText(point.observedAtMs), value: PaidCreditsPresentation.amount(point.hasBalance ? point.balance : nil, unlimited: point.unlimited, localization: localization))
                    }
                    Text("每天保留最后一次有效观测（UTC），采集空档不补值。")
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
        }
    }
}
