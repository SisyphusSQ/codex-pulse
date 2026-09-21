import CodexPulseAppSupport
import CodexPulseProtocolGenerated
import SwiftUI

struct CodexAccountQuotasView: View {
    @ObservedObject var model: AppModel

    private var refreshIsRunning: Bool {
        if case .running = model.quotaRefreshState { return true }
        return false
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                pageHeaderCopy
                FeatureStateView(
                    state: model.codexAccountQuotasState,
                    emptyTitle: "还没有账号额度记录",
                    emptySystemImage: "person.2.crop.square.stack"
                ) { response in
                    accountSections(response)
                }
            }
            .padding(20)
        }
        .accessibilityIdentifier("page.account-quotas")
    }

    private var pageHeaderCopy: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(AppFeature.accountQuotas.title(localization: model.localization))
                .font(.largeTitle.bold())
            Text("查看当前账号与曾使用账号最后采集到的真实额度窗口；历史账号只展示快照，不会触发登录或网络刷新。")
                .foregroundStyle(.secondary)
        }
    }

    private var refreshButton: some View {
        Button {
            model.requestQuotaRefresh(source: "quota")
        } label: {
            if refreshIsRunning {
                HStack(spacing: 7) {
                    ProgressView().controlSize(.small)
                    Text("正在刷新")
                }
            } else {
                Label("刷新当前账号", systemImage: "arrow.clockwise")
            }
        }
        .disabled(refreshIsRunning)
        .accessibilityIdentifier("account-quota.refresh-current")
    }

    private func accountSections(
        _ response: Codexpulse_Core_V1_CodexAccountQuotasResponse
    ) -> some View {
        let currentAccounts = response.accounts.filter(\.account.current)
        let historicalAccounts = response.accounts.filter { !$0.account.current }
        return VStack(alignment: .leading, spacing: 18) {
            if !currentAccounts.isEmpty {
                accountSection(
                    title: "当前账号",
                    accounts: currentAccounts,
                    response: response,
                    showsRefresh: true
                )
            }
            if !historicalAccounts.isEmpty {
                accountSection(
                    title: "以前使用过的账号",
                    accounts: historicalAccounts,
                    response: response,
                    showsRefresh: false
                )
            }
        }
    }

    private func accountSection(
        title: String,
        accounts: [Codexpulse_Core_V1_CodexAccountQuota],
        response: Codexpulse_Core_V1_CodexAccountQuotasResponse,
        showsRefresh: Bool
    ) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack {
                Text(title)
                    .font(.title2.bold())
                Spacer()
                if showsRefresh { refreshButton }
            }
            LazyVGrid(
                columns: [GridItem(.adaptive(minimum: 300, maximum: 340), spacing: 12)],
                alignment: .leading,
                spacing: 12
            ) {
                ForEach(accounts, id: \.account.accountID) { quota in
                    CodexAccountQuotaCard(
                        quota: quota,
                        evaluatedAtMS: response.evaluatedAtMs,
                        timeZoneIdentifier: response.timeZone,
                        localization: model.localization
                    )
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}
