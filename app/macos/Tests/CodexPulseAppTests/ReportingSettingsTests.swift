import CodexPulseAppSupport
import CodexPulseProtocolGenerated
import Foundation

private enum ReportingTestFailure: Error { case mismatch(String); case unavailable }
private func reportingExpect(_ value: Bool, _ message: String) throws {
    if !value { throw ReportingTestFailure.mismatch(message) }
}

private actor ReportingFake: ReportingSettingsServing {
    var value = Codexpulse_Core_V1_ReportingStatusResponse.with {
        $0.endpoint = "https://old.example.invalid"; $0.clientID = "old-client"
        $0.intervalSeconds = 60; $0.state = "disabled"
    }
    var pairRequests: [Codexpulse_Core_V1_PairReportingRequest] = []
    var configurationRequests: [Codexpulse_Core_V1_ConfigureReportingRequest] = []
    var holdRead = false
    var readContinuation: CheckedContinuation<Codexpulse_Core_V1_ReportingStatusResponse, Never>?
    var failsPair = false
    var readPending: Bool { readContinuation != nil }
    func prepareReadRace() { holdRead = true }
    func setPairFailure() { failsPair = true }
    func releaseRead() { readContinuation?.resume(returning: .with { $0.endpoint = "https://stale.example.invalid"; $0.intervalSeconds = 60 }); readContinuation = nil; holdRead = false }
    func reportingStatus() async throws -> Codexpulse_Core_V1_ReportingStatusResponse {
        if holdRead { return await withCheckedContinuation { readContinuation = $0 } }
        return value
    }
    func pairReporting(_ request: Codexpulse_Core_V1_PairReportingRequest) async throws -> Codexpulse_Core_V1_ReportingStatusResponse {
        pairRequests.append(request)
        if failsPair { throw ReportingTestFailure.unavailable }
        value.endpoint = request.endpoint; value.allowHTTP = request.allowHTTP; value.clientID = "new-client"
        value.enabled = false; value.state = "disabled"
        return value
    }
    func configureReporting(_ request: Codexpulse_Core_V1_ConfigureReportingRequest) async throws -> Codexpulse_Core_V1_ReportingStatusResponse {
        configurationRequests.append(request)
        value.enabled = request.enabled; value.historyStartAtMs = request.historyStartAtMs; value.intervalSeconds = request.intervalSeconds
        value.state = request.enabled ? "ready" : "disabled"
        return value
    }
    func syncReportingNow() async throws -> Codexpulse_Core_V1_ReportingStatusResponse { value }
}

@MainActor
func testReportingSettingsSafetyAndLateRead() async throws {
    let core = ReportingFake()
    let model = ReportingSettingsModel(service: core)
    await model.refresh()
    try reportingExpect(model.status?.enabled == false && !model.enabled && model.intervalSeconds == 60, "reporting must remain default-off")
    model.endpoint = " http://127.0.0.1:18085 "; model.allowHTTP = true; model.code = "ABCDEFGHIJKLMNOP"
    await model.pair()
    let request = await core.pairRequests.last
    try reportingExpect(request?.endpoint == "http://127.0.0.1:18085" && request?.allowHTTP == true && model.code.isEmpty && !model.enabled, "pair must use explicit HTTP and clear one-time code without enabling")
    model.enabled = true; model.intervalSeconds = 30; model.allHistory = false; model.historyStart = Date(timeIntervalSince1970: 1234)
    await model.configure()
    let config = await core.configurationRequests.last
    try reportingExpect(config?.enabled == true && config?.historyStartAtMs == 1_234_000 && config?.intervalSeconds == 30, "Helper owns precise persisted range and enable")
    model.historyStart = Date(timeIntervalSince1970: 9999)
    await model.configure(clearPending: true)
    let clear = await core.configurationRequests.last
    try reportingExpect(clear?.enabled == false && clear?.clearPending_p == true && clear?.historyStartAtMs == 1_234_000, "clear stops syncing without changing authoritative history start")
    await core.setPairFailure(); model.code = "ONETIMEFAILEDXXX"; await model.pair()
    try reportingExpect(model.code.isEmpty && model.error != nil && model.status?.enabled == false, "failed pairing cannot preserve code or fake success")
    await model.refresh()
    try reportingExpect(model.error != nil, "background read must not erase an action failure")

    let raceCore = ReportingFake()
    let subject = ReportingSettingsModel(service: raceCore)
    await raceCore.prepareReadRace()
    let read = Task { await subject.refresh() }
    let deadline = ContinuousClock.now.advanced(by: .seconds(2))
    while !(await raceCore.readPending) && ContinuousClock.now < deadline { await Task.yield() }
    guard await raceCore.readPending else { throw ReportingTestFailure.mismatch("held read not reached") }
    subject.endpoint = "https://new.example.invalid"; subject.code = "ABCDEFGHIJKLMNOP"
    await subject.pair(); await raceCore.releaseRead(); await read.value
    try reportingExpect(subject.status?.clientID == "new-client" && subject.endpoint == "https://new.example.invalid", "late read cannot replace a successful pairing")
    model.stop(); subject.stop()
    subject.code = "SHOULDBECLEARED"; subject.stop()
    try reportingExpect(subject.code.isEmpty, "shutdown must clear transient pairing input")
}
