import CodexPulseProtocolGenerated
import Combine
import Foundation

public protocol ReportingSettingsServing: Sendable {
    func reportingStatus() async throws -> Codexpulse_Core_V1_ReportingStatusResponse
    func pairReporting(_ request: Codexpulse_Core_V1_PairReportingRequest) async throws -> Codexpulse_Core_V1_ReportingStatusResponse
    func configureReporting(_ request: Codexpulse_Core_V1_ConfigureReportingRequest) async throws -> Codexpulse_Core_V1_ReportingStatusResponse
    func syncReportingNow() async throws -> Codexpulse_Core_V1_ReportingStatusResponse
    func fullSyncReporting() async throws -> Codexpulse_Core_V1_ReportingStatusResponse
}

public extension ReportingSettingsServing {
    func fullSyncReporting() async throws -> Codexpulse_Core_V1_ReportingStatusResponse {
        throw AppRuntimeError.unavailable
    }
}
extension AppRuntime: ReportingSettingsServing {}

// UI 仅持有 CoreService 的安全状态和短暂配对码；不保存或接触设备凭证。
@MainActor
public final class ReportingSettingsModel: ObservableObject {
    @Published public private(set) var status: Codexpulse_Core_V1_ReportingStatusResponse?
    @Published public private(set) var busy = false
    @Published public private(set) var error: String?
    @Published public private(set) var readError: String?
    @Published public private(set) var message: String?
    @Published public var endpoint = ""
    @Published public var code = ""
    @Published public var allowHTTP = false
    @Published public var enabled = false
    @Published public var intervalSeconds: Int64 = 60
    @Published public var allHistory = true
    @Published public var historyStart = Date(timeIntervalSinceNow: -30 * 86400)

    private let service: any ReportingSettingsServing
    private var epoch: UInt64 = 0
    private var stopped = false
    public init(service: any ReportingSettingsServing) { self.service = service }

    public var historyLocked: Bool {
        guard let status else { return false }
        return status.hasLastAttemptAtMs || status.hasLastSuccessAtMs || status.pendingBatches > 0
    }

    public func stop() { stopped = true; epoch &+= 1; code = "" }

    public func observe() async {
        stopped = false
        while !Task.isCancelled && !stopped {
            await refresh()
            do { try await Task.sleep(for: .seconds(5)) } catch { return }
        }
    }

    public func refresh() async {
        guard !busy && !stopped else { return }
        let generation = epoch
        do {
            let result = try await service.reportingStatus()
            try Task.checkCancellation()
            guard generation == epoch && !stopped else { return }
            let first = status == nil
            status = result
            if first && endpoint.isEmpty && code.isEmpty && !allowHTTP && !enabled && intervalSeconds == 60 && allHistory {
                adopt(result)
            }
            readError = nil
        } catch {
            if !Task.isCancelled && generation == epoch && !stopped {
                readError = "无法读取本机同步状态，请确认 Helper 已连接后刷新。"
            }
        }
    }

    public func pair() async {
        var request = Codexpulse_Core_V1_PairReportingRequest()
        request.endpoint = endpoint.trimmingCharacters(in: .whitespacesAndNewlines)
        request.code = code.trimmingCharacters(in: .whitespacesAndNewlines)
        request.allowHTTP = allowHTTP
        code = "" // 一次性码不保留作自动重试；返回丢失时申请新码。
        await apply(success: "配对完成，同步仍关闭。请确认历史范围后保存启用。", failure: "配对未完成，请检查中心地址、HTTP 许可及配对码。返回中断时码可能已消费，请在中心核对后签发新码。") {
            try await self.service.pairReporting(request)
        }
    }

    public func configure(clearPending: Bool = false) async {
        var request = Codexpulse_Core_V1_ConfigureReportingRequest()
        request.enabled = clearPending ? false : enabled
        request.intervalSeconds = intervalSeconds
        // 清理队列不隐式改变已固定的补传范围。
        request.historyStartAtMs = clearPending ? status?.historyStartAtMs ?? 0 : allHistory ? 0 : Int64(historyStart.timeIntervalSince1970 * 1000)
        request.clearPending_p = clearPending
        await apply(success: clearPending ? "已关闭同步并清理当前中心的本机待发送队列；中心历史保留。" : "同步设置已由 Helper 保存并读回。", failure: "设置未保存。请确认间隔为 15–3600 秒；开始补传后历史起点固定，改变范围需要重新配对。") {
            try await self.service.configureReporting(request)
        }
    }

    public func syncNow() async {
        await apply(success: "已请求本机增量补传，确认时间以状态读回为准。", failure: "未能开始补传，请确认已启用同步；撤销或协议拒绝后需要重新配对或更新服务。") {
            try await self.service.syncReportingNow()
        }
    }

    public func fullSync() async {
        await apply(success: "已请求全量补传当前范围的已索引事实；退出 App 后暂停，下次启动续传。", failure: "未能开始全量补传，请确认同步已启用且中心支持分片协议。") {
            try await self.service.fullSyncReporting()
        }
    }

    private func apply(success: String, failure: String, operation: () async throws -> Codexpulse_Core_V1_ReportingStatusResponse) async {
        guard !busy && !stopped else { return }
        busy = true; error = nil; message = nil; epoch &+= 1
        let generation = epoch
        defer { busy = false }
        do {
            let result = try await operation()
            try Task.checkCancellation()
            guard generation == epoch && !stopped else { return }
            status = result
            adopt(result)
            message = success
        } catch {
            guard generation == epoch && !stopped && !Task.isCancelled else { return }
            self.error = failure
        }
    }

    private func adopt(_ status: Codexpulse_Core_V1_ReportingStatusResponse) {
        endpoint = status.endpoint; allowHTTP = status.allowHTTP; enabled = status.enabled
        intervalSeconds = status.intervalSeconds
        allHistory = status.historyStartAtMs == 0
        if status.historyStartAtMs > 0 { historyStart = Date(timeIntervalSince1970: Double(status.historyStartAtMs) / 1000) }
    }
}
