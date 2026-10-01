import CodexPulseAppSupport
import SwiftUI

struct ReportingSettingsSection: View {
    @ObservedObject var settings: ReportingSettingsModel
    @State private var confirmsClear = false

    var body: some View {
        Section("多机中心") {
            Text("上报用量、配额、reset、TPS，以及账号 ID、邮箱、项目名和会话标题/原始 Session ID。原始 JSONL、正文、工具内容、API key、Cookie 和 Agent 凭据留在本机。")
                .font(.caption).foregroundStyle(.secondary)
            TextField("中心地址", text: $settings.endpoint, prompt: Text("https://pulse.example"))
                .textFieldStyle(.roundedBorder).accessibilityIdentifier("reporting.endpoint")
            Toggle("允许私网 HTTP（LAN / Tailscale / 环回）", isOn: $settings.allowHTTP)
                .accessibilityIdentifier("reporting.allow-http")
            Text("HTTP 许可只在重新配对时生效；公网使用 HTTPS。不会自动降级，Helper 保留证书校验与私网目标检查。")
                .font(.caption).foregroundStyle(.secondary)
            SecureField("采集设备配对码", text: $settings.code)
                .textFieldStyle(.roundedBorder).accessibilityIdentifier("reporting.code")
            Button(settings.status?.clientID.isEmpty == false ? "重新配对中心" : "配对中心") {
                Task { await settings.pair() }
            }.disabled(settings.endpoint.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || settings.code.isEmpty)
                .accessibilityIdentifier("reporting.pair")
            Toggle("启用上报", isOn: $settings.enabled)
                .disabled(settings.status?.clientID.isEmpty != false).accessibilityIdentifier("reporting.enabled")
            Stepper("同步间隔：\(settings.intervalSeconds) 秒", value: $settings.intervalSeconds, in: 15...3600, step: 15)
            Toggle("补传本机全部已索引历史", isOn: $settings.allHistory)
                .disabled(settings.historyLocked).accessibilityIdentifier("reporting.all-history")
            if !settings.allHistory {
                DatePicker("首次补传起点", selection: $settings.historyStart, in: Date(timeIntervalSince1970: 0)...Date(), displayedComponents: [.date, .hourAndMinute])
                    .disabled(settings.historyLocked)
            }
            Text("首次起点在开始导出后固定；改变范围需签发新设备码重新配对。范围受限时生命周期 TPS 保持未知。关闭上报保留队列，退出 App 停止采集和发送，下次启动增量补采。")
                .font(.caption).foregroundStyle(.secondary)
            HStack {
                Button("保存同步设置") { Task { await settings.configure() } }
                    .disabled(settings.status == nil || settings.status?.clientID.isEmpty != false)
                    .accessibilityIdentifier("reporting.save")
                Button("立即增量补传") { Task { await settings.syncNow() } }
                    .disabled(settings.status?.enabled != true).accessibilityIdentifier("reporting.sync-now")
                Button("刷新状态") { Task { await settings.refresh() } }
                if settings.busy { ProgressView().controlSize(.small) }
            }
            if let status = settings.status {
                LabeledContent("当前中心", value: status.endpoint.isEmpty ? "尚未配对" : status.endpoint)
                LabeledContent("设备 ID", value: status.clientID.isEmpty ? "尚未配对" : status.clientID)
                LabeledContent("同步状态", value: stateLabel(status.state))
                LabeledContent("实际已保存开关", value: status.enabled ? "已启用" : "已关闭")
                LabeledContent("待发送批次", value: String(status.pendingBatches))
                LabeledContent("队列大小", value: ByteCountFormatter.string(fromByteCount: status.pendingBytes, countStyle: .file))
                LabeledContent("其他中心保留批次", value: String(status.retainedBatches))
                LabeledContent("最近尝试", value: time(status.hasLastAttemptAtMs ? status.lastAttemptAtMs : nil))
                LabeledContent("最近确认", value: time(status.hasLastSuccessAtMs ? status.lastSuccessAtMs : nil))
                Button("关闭同步并清理当前中心待发送队列…", role: .destructive) { confirmsClear = true }
                    .disabled(status.pendingBatches == 0).accessibilityIdentifier("reporting.clear-pending")
            }
            if let error = settings.error { Text(error).foregroundStyle(.orange).font(.caption) }
            if let error = settings.readError { Text(error).foregroundStyle(.orange).font(.caption) }
            if let message = settings.message { Text(message).foregroundStyle(.secondary).font(.caption) }
        }
        .disabled(settings.busy)
        .task { await settings.observe() }
        .confirmationDialog("清理本机待发送队列？", isPresented: $confirmsClear, titleVisibility: .visible) {
            Button("关闭同步并清理队列", role: .destructive) { Task { await settings.configure(clearPending: true) } }
            Button("取消", role: .cancel) {}
        } message: {
            Text("会丢弃尚未确认的本机上传批次，并关闭同步；中心历史不会删除，其他中心保留队列不受影响。重新启用后由 Helper 增量补采。")
        }
    }

    private func time(_ value: Int64?) -> String {
        guard let value else { return "尚无记录" }
        return Date(timeIntervalSince1970: Double(value) / 1000).formatted(date: .abbreviated, time: .shortened)
    }
    private func stateLabel(_ value: String) -> String {
        ["disabled":"已关闭", "ready":"已准备 / 等待增量同步", "partial":"部分来源可用", "offline":"连接失败，等待有界重试", "reconnect_required":"凭证已失效，请重新配对", "protocol_rejected":"协议不兼容，请先更新中心", "queue_full":"队列已满，保留待发送数据", "source_budget_exceeded":"来源超过单批预算", "source_unavailable":"本机来源尚不可用", "storage_unavailable":"本机同步存储不可用"][value] ?? "状态未知"
    }
}
