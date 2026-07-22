import CodexPulseAppSupport
import CodexPulseCoreClient
import CodexPulseProtocolGenerated
import Foundation

private enum TestFailure: Error, CustomStringConvertible, Sendable {
    case mismatch(String)

    var description: String {
        switch self {
        case .mismatch(let message): message
        }
    }
}

private enum FakeFailure: Error, Sendable {
    case unavailable
}

private struct SessionPagePlan: Sendable {
    let delay: Duration
    let response: Codexpulse_Core_V1_SessionListResponse
    let fails: Bool

    init(
        delay: Duration,
        response: Codexpulse_Core_V1_SessionListResponse,
        fails: Bool = false
    ) {
        self.delay = delay
        self.response = response
        self.fails = fails
    }
}

private func expect(_ condition: @autoclosure () -> Bool, _ message: String) throws {
    guard condition() else { throw TestFailure.mismatch(message) }
}

private func waitUntil(
    _ context: String,
    timeout: Duration = .seconds(2),
    condition: @escaping @Sendable () async -> Bool
) async throws {
    let clock = ContinuousClock()
    let deadline = clock.now.advanced(by: timeout)
    while clock.now < deadline {
        if await condition() { return }
        try await Task.sleep(for: .milliseconds(10))
    }
    throw TestFailure.mismatch("timed out: \(context)")
}

private actor StateRecorder {
    private var phases: [String] = []

    func append(_ state: CoreConnectionState) {
        switch state {
        case .idle: phases.append("idle")
        case .starting: phases.append("starting")
        case .handshaking: phases.append("handshaking")
        case .loadingOverview: phases.append("loading_overview")
        case .normal: phases.append("normal")
        case .partial: phases.append("partial")
        case .recovery: phases.append("recovery")
        case .restartRequired: phases.append("restart_required")
        case .stale: phases.append("stale")
        case .unavailable: phases.append("unavailable")
        case .cancelled: phases.append("cancelled")
        case .shuttingDown: phases.append("shutting_down")
        case .stopped: phases.append("stopped")
        }
    }

    func snapshot() -> [String] { phases }
}

private actor FakeSupervisor: HelperSupervising {
    private var starts = 0
    private var stops = 0
    private let startFailure: Bool
    private let startDelay: Duration

    init(startFailure: Bool = false, startDelay: Duration = .zero) {
        self.startFailure = startFailure
        self.startDelay = startDelay
    }

    func start() async throws -> RunningHelper {
        starts += 1
        if startDelay != .zero { try await Task.sleep(for: startDelay) }
        if startFailure { throw FakeFailure.unavailable }
        return RunningHelper(
            processID: 42,
            socketPath: "/private/tmp/cp-test/core.sock",
            databasePath: "/private/tmp/cp-test/data/test.db",
            preferencesPath: "/private/tmp/cp-test/preferences.json",
            bearerToken: "test-only"
        )
    }

    func waitForExit(timeout: Duration) async throws -> Int32 { 0 }

    func stop(mode: HelperStopMode) async { stops += 1 }

    func counts() -> (Int, Int) { (starts, stops) }
}

private actor FakeCore: AppCoreServing {
    private var bootstrapResponse: Codexpulse_Core_V1_BootstrapResponse
    private var recoveryReceipt: Codexpulse_Core_V1_MigrationRecoveryReceipt
    private var responses: OverviewResponses
    private var failOverview = false
    private var handshakeFailure = false
    private var handshakeError: CoreClientError?
    private var overviewDelay: Duration = .zero
    private var handshakeDelay: Duration = .zero
    private var bootstrapDelay: Duration = .zero
    private var shutdownDelay: Duration = .zero
    private var calls: [String] = []
    private var featureSessionPlans: [SessionPagePlan] = []
    private var invalidationDomain: String?
    private var invalidationDelay: Duration = .zero
    private var quotaRefreshDelay: Duration = .zero
    private var settingsResponses: [Codexpulse_Core_V1_SettingsResponse] = []
    private var settingsUpdateFailure = false
    private var settingsReadDelay: Duration = .zero
    private var settingsUpdateDelay: Duration = .zero

    init(
        bootstrap: Codexpulse_Core_V1_BootstrapResponse,
        recoveryReceipt: Codexpulse_Core_V1_MigrationRecoveryReceipt = .init(),
        responses: OverviewResponses
    ) {
        self.bootstrapResponse = bootstrap
        self.recoveryReceipt = recoveryReceipt
        self.responses = responses
    }

    func setOverviewFailure(_ value: Bool) { failOverview = value }
    func setHandshakeFailure(_ value: Bool) { handshakeFailure = value }
    func setHandshakeError(_ value: CoreClientError?) { handshakeError = value }
    func setOverviewDelay(_ value: Duration) { overviewDelay = value }
    func setHandshakeDelay(_ value: Duration) { handshakeDelay = value }
    func setBootstrapDelay(_ value: Duration) { bootstrapDelay = value }
    func setShutdownDelay(_ value: Duration) { shutdownDelay = value }
    func setFeatureSessionPlans(_ plans: [SessionPagePlan]) { featureSessionPlans = plans }
    func setInvalidation(domain: String?, delay: Duration = .zero) {
        invalidationDomain = domain
        invalidationDelay = delay
    }
    func setQuotaRefreshDelay(_ value: Duration) { quotaRefreshDelay = value }
    func setSettingsReadDelay(_ value: Duration) { settingsReadDelay = value }
    func setSettingsUpdateDelay(_ value: Duration) { settingsUpdateDelay = value }
    func setSettingsResponses(_ values: [Codexpulse_Core_V1_SettingsResponse], updateFailure: Bool) {
        settingsResponses = values
        settingsUpdateFailure = updateFailure
    }
    func recordedCalls() -> [String] { calls }

    func handshake(
        clientName: String,
        clientVersion: String,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_HandshakeResponse {
        calls.append("handshake")
        if handshakeDelay != .zero { try await Task.sleep(for: handshakeDelay) }
        if let handshakeError { throw handshakeError }
        if handshakeFailure { throw FakeFailure.unavailable }
        var response = Codexpulse_Core_V1_HandshakeResponse()
        response.contractVersion = CodexPulseTransportContract.version
        response.transport = CodexPulseTransportContract.transport
        return response
    }

    func bootstrap(
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_BootstrapResponse {
        calls.append("bootstrap")
        if bootstrapDelay != .zero { try await Task.sleep(for: bootstrapDelay) }
        return bootstrapResponse
    }

    func usageCost(
        _ request: Codexpulse_Core_V1_UsageCostRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_UsageCostResponse {
        calls.append("usage")
        if overviewDelay != .zero { try await Task.sleep(for: overviewDelay) }
        if failOverview { throw FakeFailure.unavailable }
        return responses.usage
    }

    func quotaCurrent(
        _ request: Codexpulse_Core_V1_QuotaCurrentRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_QuotaCurrentResponse {
        calls.append("quota")
        if overviewDelay != .zero { try await Task.sleep(for: overviewDelay) }
        if failOverview { throw FakeFailure.unavailable }
        return responses.quota
    }

    func listSessions(
        _ request: Codexpulse_Core_V1_ListSessionsRequest,
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_SessionListResponse {
        calls.append("sessions")
        if request.query.page.limit != 5, !featureSessionPlans.isEmpty {
            let plan = featureSessionPlans.removeFirst()
            if plan.delay != .zero { try await Task.sleep(for: plan.delay) }
            if plan.fails { throw FakeFailure.unavailable }
            return plan.response
        }
        if overviewDelay != .zero { try await Task.sleep(for: overviewDelay) }
        if failOverview { throw FakeFailure.unavailable }
        return responses.sessions
    }

    func runRuntimeAction(
        _ request: Codexpulse_Core_V1_RuntimeActionRequest
    ) async throws -> Codexpulse_Core_V1_RuntimeActionReceipt {
        calls.append("runtime_action:\(request.action)")
        var receipt = Codexpulse_Core_V1_RuntimeActionReceipt()
        receipt.action = request.action
        receipt.pauseScope = request.action == "pause_all" ? "all" : "none"
        receipt.sourceState = "ready"
        receipt.transition = "applied"
        return receipt
    }

    func requestQuotaRefresh(
        _ request: Codexpulse_Core_V1_QuotaRefreshRequest
    ) async throws -> Codexpulse_Core_V1_QuotaRefreshReceipt {
        calls.append("quota_refresh:\(request.source)")
        if quotaRefreshDelay != .zero { try await Task.sleep(for: quotaRefreshDelay) }
        var receipt = Codexpulse_Core_V1_QuotaRefreshReceipt()
        receipt.source = request.source
        receipt.reason = "accepted"
        return receipt
    }

    func settings(
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_SettingsResponse {
        calls.append("settings")
        if settingsReadDelay != .zero { try await Task.sleep(for: settingsReadDelay) }
        guard !settingsResponses.isEmpty else { throw FakeFailure.unavailable }
        if settingsResponses.count == 1 { return settingsResponses[0] }
        return settingsResponses.removeFirst()
    }

    func updateSettings(
        _ request: Codexpulse_Core_V1_UpdateSettingsRequest
    ) async throws -> Codexpulse_Core_V1_SettingsUpdateReceipt {
        calls.append("settings_update:\(request.expectedRevision)")
        if settingsUpdateDelay != .zero { try await Task.sleep(for: settingsUpdateDelay) }
        if settingsUpdateFailure { throw FakeFailure.unavailable }
        var receipt = Codexpulse_Core_V1_SettingsUpdateReceipt()
        receipt.result = "applied"
        receipt.revision = settingsResponses.last?.snapshot.revision ?? request.expectedRevision
        return receipt
    }

    func healthProjection(
        retryPolicy: ReadRetryPolicy
    ) async throws -> Codexpulse_Core_V1_HealthProjectionResponse {
        calls.append("health")
        if overviewDelay != .zero { try await Task.sleep(for: overviewDelay) }
        if failOverview { throw FakeFailure.unavailable }
        return responses.health
    }

    func migrationRecoveryRetry() async throws -> Codexpulse_Core_V1_MigrationRecoveryReceipt {
        calls.append("recovery_retry")
        return recoveryReceipt
    }

    func notifyLifecycle(
        _ event: LifecycleEvent
    ) async throws -> Codexpulse_Core_V1_LifecycleNotificationReceipt {
        calls.append("lifecycle:\(event.rawValue)")
        var response = Codexpulse_Core_V1_LifecycleNotificationReceipt()
        response.event = event.rawValue
        response.accepted = true
        return response
    }

    func consumeInvalidations(
        domains: [String],
        afterSequence: UInt64,
        onReady: @Sendable @escaping () async -> Void,
        onEvent: @Sendable @escaping (Codexpulse_Core_V1_QueryInvalidationEvent) async throws -> Void
    ) async throws {
        calls.append("stream:\(domains.joined(separator: ","))")
        await onReady()
        if let invalidationDomain {
            if invalidationDelay != .zero { try await Task.sleep(for: invalidationDelay) }
            var event = Codexpulse_Core_V1_QueryInvalidationEvent()
            event.version = CodexPulseTransportContract.invalidationVersion
            event.domain = invalidationDomain
            event.sequence = 1
            try await onEvent(event)
        }
        try await Task.sleep(for: .seconds(60))
    }

    func shutdown(reason: String) async throws {
        calls.append("shutdown:\(reason)")
        if shutdownDelay != .zero { try await Task.sleep(for: shutdownDelay) }
    }
    func closeTransport() async { calls.append("close_transport") }
}

private func completeMeta() -> Codexpulse_Core_V1_ResponseMeta {
    var meta = Codexpulse_Core_V1_ResponseMeta()
    meta.version = "test-v1"
    meta.status = "complete"
    return meta
}

private func makeNormalBootstrap() -> Codexpulse_Core_V1_BootstrapResponse {
    var response = Codexpulse_Core_V1_BootstrapResponse()
    response.mode = "normal"
    return response
}

private func makeResponses(partial: Bool = false) -> OverviewResponses {
    var usage = Codexpulse_Core_V1_UsageCostResponse()
    usage.meta = completeMeta()
    usage.totals.totalTokens.value = 0
    usage.totals.totalTokens.unit = "tokens"
    usage.totals.estimatedUsdMicros.unknownReason = "pricing_missing"
    usage.totals.estimatedUsdMicros.unit = "usd_micros"
    var point = Codexpulse_Core_V1_TrendPoint()
    point.key = "2026-07-21"
    point.totals.totalTokens.value = 0
    point.totals.totalTokens.unit = "tokens"
    usage.trend = [point]

    var primary = Codexpulse_Core_V1_CurrentWindow()
    primary.windowKind = "primary"
    primary.limitID = "codex"
    primary.remainingPercent = 0
    primary.freshness = "fresh"
    var secondary = Codexpulse_Core_V1_CurrentWindow()
    secondary.windowKind = "secondary"
    secondary.limitID = "codex"
    secondary.freshness = "unknown"
    secondary.unknownReason = "not_observed"
    var quota = Codexpulse_Core_V1_QuotaCurrentResponse()
    quota.meta = completeMeta()
    quota.current.windows = [primary, secondary]

    var sessions = Codexpulse_Core_V1_SessionListResponse()
    sessions.meta = completeMeta()
    var session = Codexpulse_Core_V1_SessionItem()
    session.sessionID = "session-test"
    session.displayTitle = "真实生成类型会话"
    session.activity = "completed"
    session.totals.totalTokens.value = 0
    session.totals.totalTokens.unit = "tokens"
    sessions.items = [session]
    if partial {
        sessions.meta.status = "partial"
        var issue = Codexpulse_Core_V1_Issue()
        issue.code = "index_incomplete"
        issue.messageKey = "core.issue.index_incomplete"
        issue.retryable = true
        sessions.meta.issues = [issue]
    }

    var health = Codexpulse_Core_V1_HealthProjectionResponse()
    health.hasValue_p = true
    health.level = "healthy"

    return OverviewResponses(usage: usage, quota: quota, sessions: sessions, health: health)
}

private func makeSessionPage(
    id: String,
    title: String,
    nextCursor: String? = nil
) -> Codexpulse_Core_V1_SessionListResponse {
    var response = Codexpulse_Core_V1_SessionListResponse()
    response.meta = completeMeta()
    var item = Codexpulse_Core_V1_SessionItem()
    item.sessionID = id
    item.displayTitle = title
    item.totals.totalTokens.value = 0
    response.items = [item]
    response.meta.page.limit = 50
    if let nextCursor {
        response.meta.page.hasMore_p = true
        response.meta.page.nextCursor = nextCursor
    }
    return response
}

private func makeSettingsResponse(revision: String, quotaEnabled: Bool) -> Codexpulse_Core_V1_SettingsResponse {
    var response = Codexpulse_Core_V1_SettingsResponse()
    response.meta = completeMeta()
    response.snapshot.revision = revision
    response.snapshot.online.quotaEnabled = quotaEnabled
    response.snapshot.online.resetCreditsEnabled = true
    response.snapshot.refresh.quotaIntervalSeconds = 300
    response.snapshot.refresh.resetCreditsIntervalSeconds = 600
    response.snapshot.refresh.reconcileIntervalSeconds = 900
    response.snapshot.refresh.jsonlDebounceMilliseconds = 250
    response.snapshot.updates.autoCheckEnabled = true
    response.snapshot.updates.checkIntervalSeconds = 3_600
    response.snapshot.ui.launchBehavior = "main_window"
    response.snapshot.ui.overviewRange = "7d"
    var editable = Codexpulse_Core_V1_EditableField()
    editable.key = "online.quotaEnabled"
    editable.editable = true
    response.editableFields = [editable]
    return response
}

private func testFeatureRequestsStateAndMerge() throws {
    var calendar = Calendar(identifier: .buddhist)
    calendar.timeZone = TimeZone(secondsFromGMT: 0)!
    let now = Date(timeIntervalSince1970: 1_753_056_000)
    var options = SessionQueryOptions()
    options.range = .sevenDays
    options.activity = "active"
    options.projectID = "project-1"
    options.modelKey = "model-1"
    options.sortField = "totalTokens"
    options.sortDirection = "asc"
    let request = FeatureRequestFactory.sessions(
        options: options,
        cursor: "opaque-cursor",
        limit: 500,
        now: now,
        calendar: calendar
    )
    try expect(request.query.page.limit == 100, "feature page limit must be bounded by provider maximum")
    try expect(request.query.page.cursor == "opaque-cursor", "opaque cursor must pass through unchanged")
    try expect(request.query.sort.first?.field == "totalTokens", "provider sort field must remain explicit")
    try expect(request.query.sort.first?.direction == "asc", "provider sort direction must remain explicit")
    try expect(request.query.filters.count == 3, "session filters must be composed, not shadow-searched")
    try expect(request.query.filters.allSatisfy { $0.operator == "eq" }, "single-value filters must use eq")
    try expect(request.query.timeRange.startDate == "2025-07-15", "feature dates must remain Gregorian")
    try expect(
        RuntimeControlAction.allCases.map(\.rawValue) == ["pause_backfill", "pause_all", "resume", "reconcile"],
        "runtime controls must expose only the RunRuntimeAction allowlist"
    )
    try expect(RuntimeControlAction(commandKey: "repair") == nil, "high-risk repair must not become a runtime action")

    let first = makeSessionPage(id: "session-a", title: "first", nextCursor: "cursor-2")
    var second = makeSessionPage(id: "session-b", title: "second")
    second.items.insert(first.items[0], at: 0)
    let merged = FeatureResponseMerge.sessions(first, second, append: true)
    try expect(merged.items.map(\.sessionID) == ["session-a", "session-b"], "pagination merge must be stable and deduplicated")
    try expect(pageHasMore(first.meta), "has_more requires an opaque next cursor")
    try expect(!pageHasMore(second.meta), "missing cursor must stop pagination")

    var previousProject = Codexpulse_Core_V1_ProjectDetailResponse()
    var previousSession = Codexpulse_Core_V1_ProjectSessionItem()
    previousSession.sessionID = "session-complete"
    previousProject.sessions = [previousSession]
    previousProject.sessionPage.hasMore_p = false
    var previousModel = Codexpulse_Core_V1_ProjectModelItem()
    previousModel.dimensionKey = "model-a"
    previousProject.models = [previousModel]
    previousProject.modelPage.hasMore_p = true
    previousProject.modelPage.nextCursor = "model-cursor"
    var nextProject = Codexpulse_Core_V1_ProjectDetailResponse()
    var repeatedFirstPageSession = Codexpulse_Core_V1_ProjectSessionItem()
    repeatedFirstPageSession.sessionID = "session-first-page-again"
    nextProject.sessions = [repeatedFirstPageSession]
    var nextModel = Codexpulse_Core_V1_ProjectModelItem()
    nextModel.dimensionKey = "model-b"
    nextProject.models = [nextModel]
    let mergedProject = FeatureResponseMerge.projectDetail(
        previousProject,
        nextProject,
        append: true,
        appendSessions: false,
        appendModels: true
    )
    try expect(
        mergedProject.sessions.map(\.sessionID) == ["session-complete"],
        "completed Project session page must not reset while models continue"
    )
    try expect(
        mergedProject.models.map(\.dimensionKey) == ["model-a", "model-b"],
        "independent Project model page must append"
    )

    var partialMeta = completeMeta()
    partialMeta.status = "partial"
    var issue = Codexpulse_Core_V1_Issue()
    issue.code = "bounded_partial"
    issue.retryable = true
    partialMeta.issues = [issue]
    if case .partial(_, let notices) = loadState(value: first, meta: partialMeta, isEmpty: false) {
        try expect(notices.first?.code == "bounded_partial", "partial issue semantics must remain recursive")
    } else {
        throw TestFailure.mismatch("partial response was normalized to ready")
    }
    var unavailableMeta = completeMeta()
    unavailableMeta.status = "unavailable"
    if case .unavailable = loadState(value: first, meta: unavailableMeta, isEmpty: false) {
        // Provider explicitly declined to provide a trustworthy value.
    } else {
        throw TestFailure.mismatch("unavailable response was presented as partial value")
    }
    var unknownMeta = completeMeta()
    unknownMeta.status = "future_status"
    if case .unavailable(let notice) = loadState(value: first, meta: unknownMeta, isEmpty: false) {
        try expect(!notice.retryable, "unknown response status must fail closed")
    } else {
        throw TestFailure.mismatch("unknown response status did not fail closed")
    }
    if case .empty = loadState(value: second, meta: completeMeta(), isEmpty: true) {
        // Expected zero state.
    } else {
        throw TestFailure.mismatch("complete zero result must become empty")
    }
    if case .stale(let previous, _) = failedLoadState(previous: first, error: FakeFailure.unavailable) {
        try expect(previous.items.first?.sessionID == "session-a", "failed refresh must retain the previous page as stale")
    } else {
        throw TestFailure.mismatch("failed refresh discarded its previous value")
    }
    if case .cancelled(let previous) = failedLoadState(previous: first, error: CancellationError()) {
        try expect(previous?.items.first?.sessionID == "session-a", "cancelled refresh must retain its bounded previous value")
    } else {
        throw TestFailure.mismatch("cancellation was presented as unavailable")
    }
}

@MainActor
private func testInvalidationRefreshesActivePage() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureSessionPlans([
        SessionPagePlan(delay: .zero, response: makeSessionPage(id: "before", title: "before")),
        SessionPagePlan(delay: .zero, response: makeSessionPage(id: "after", title: "after")),
    ])
    await core.setInvalidation(domain: "index", delay: .milliseconds(150))
    let runtime = AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core })
    let model = AppModel(runtime: runtime)
    model.start()
    try await waitUntil("overview before invalidation") { await MainActor.run { model.presentation != nil } }
    model.navigate(to: .sessions)
    try await waitUntil("initial sessions page") {
        await MainActor.run { model.sessionsState.value?.items.first?.sessionID == "before" }
    }
    try await waitUntil("index invalidation reloads active sessions") {
        await MainActor.run { model.sessionsState.value?.items.first?.sessionID == "after" }
    }
    if case .idle = model.sessionDetailState {
        // No detail was selected, so an index invalidation must not invent a detail error.
    } else {
        throw TestFailure.mismatch("unselected session detail became unavailable after invalidation")
    }
    let calls = await core.recordedCalls()
    try expect(
        calls.contains(where: { $0 == "stream:index,quota,health,settings" }),
        "invalidation stream must subscribe to settings as well as data domains"
    )
    _ = await model.shutdown()
}

@MainActor
private func testRepeatedCursorStopsPagination() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureSessionPlans([
        SessionPagePlan(delay: .zero, response: makeSessionPage(id: "page-1", title: "page 1", nextCursor: "repeat")),
        SessionPagePlan(delay: .zero, response: makeSessionPage(id: "page-2", title: "page 2", nextCursor: "repeat")),
    ])
    let model = AppModel(runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core }))
    model.start()
    try await waitUntil("pagination overview") { await MainActor.run { model.presentation != nil } }
    model.loadSessions(reset: true)
    try await waitUntil("pagination first page") {
        await MainActor.run { model.sessionsState.value?.items.first?.sessionID == "page-1" }
    }
    model.loadSessions(reset: false)
    try await waitUntil("pagination second page") {
        await MainActor.run { model.sessionsState.value?.items.count == 2 }
    }
    model.loadSessions(reset: false)
    try expect(model.sessionsState.value?.meta.page.hasMore_p == false, "repeated cursor must terminate pagination")
    if case .partial(_, let notices) = model.sessionsState {
        try expect(notices.first?.code == "pagination_cursor_repeated", "cursor loop must remain visible")
    } else {
        throw TestFailure.mismatch("cursor loop did not produce a bounded partial state")
    }
    _ = await model.shutdown()
}

@MainActor
private func testTransientCursorFailureCanRetry() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureSessionPlans([
        SessionPagePlan(delay: .zero, response: makeSessionPage(id: "page-1", title: "page 1", nextCursor: "retry")),
        SessionPagePlan(delay: .zero, response: makeSessionPage(id: "failed", title: "failed"), fails: true),
        SessionPagePlan(delay: .zero, response: makeSessionPage(id: "page-2", title: "page 2")),
    ])
    let model = AppModel(runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core }))
    model.start()
    try await waitUntil("cursor retry overview") { await MainActor.run { model.presentation != nil } }
    model.loadSessions(reset: true)
    try await waitUntil("cursor retry first page") {
        await MainActor.run { model.sessionsState.value?.items.first?.sessionID == "page-1" }
    }
    model.loadSessions(reset: false)
    try await waitUntil("cursor retry transient failure") {
        await MainActor.run {
            if case .stale = model.sessionsState { return true }
            return false
        }
    }
    model.loadSessions(reset: false)
    try await waitUntil("cursor retry succeeds") {
        await MainActor.run { model.sessionsState.value?.items.map(\.sessionID) == ["page-1", "page-2"] }
    }
    _ = await model.shutdown()
}

@MainActor
private func testQuotaMutationIsSingleFlight() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setQuotaRefreshDelay(.milliseconds(100))
    let model = AppModel(runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core }))
    model.start()
    try await waitUntil("quota singleflight overview") { await MainActor.run { model.presentation != nil } }
    model.requestQuotaRefresh(source: "quota")
    model.requestQuotaRefresh(source: "quota")
    try await waitUntil("quota singleflight receipt") {
        await MainActor.run {
            if case .succeeded = model.quotaRefreshState { return true }
            return false
        }
    }
    let calls = await core.recordedCalls().filter { $0 == "quota_refresh:quota" }
    try expect(calls.count == 1, "quota mutation must not be cancelled and replayed")
    _ = await model.shutdown()
}

@MainActor
private func testLifecycleInvalidationPreservesMutation() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setQuotaRefreshDelay(.milliseconds(300))
    await core.setInvalidation(domain: "lifecycle", delay: .milliseconds(120))
    let model = AppModel(runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core }))
    model.start()
    try await waitUntil("lifecycle mutation overview") { await MainActor.run { model.presentation != nil } }
    model.requestQuotaRefresh(source: "quota")
    try await waitUntil("lifecycle mutation receipt") {
        await MainActor.run {
            if case .succeeded = model.quotaRefreshState { return true }
            return false
        }
    }
    let calls = await core.recordedCalls().filter { $0 == "quota_refresh:quota" }
    try expect(calls.count == 1, "active/wake invalidation must not cancel an in-flight mutation")
    _ = await model.shutdown()
}

@MainActor
private func testSettingsConflictPreservesDraft() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setSettingsResponses([
        makeSettingsResponse(revision: "revision-1", quotaEnabled: false),
        makeSettingsResponse(revision: "revision-2", quotaEnabled: false),
    ], updateFailure: true)
    let model = AppModel(runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core }))
    model.start()
    try await waitUntil("settings conflict overview") { await MainActor.run { model.presentation != nil } }
    model.loadSettings()
    try await waitUntil("settings authoritative load") {
        await MainActor.run { model.settingsState.value?.snapshot.revision == "revision-1" }
    }
    guard var draft = model.settingsDraft else { throw TestFailure.mismatch("settings draft missing") }
    draft.quotaEnabled = true
    model.settingsDraft = draft
    model.saveSettings()
    model.saveSettings()
    try await waitUntil("settings revision conflict") {
        await MainActor.run {
            if case .conflict = model.settingsSaveState { return true }
            return false
        }
    }
    try expect(model.settingsState.value?.snapshot.revision == "revision-2", "conflict must retain authoritative readback")
    try expect(model.settingsDraft?.quotaEnabled == true, "conflict must preserve the user's pending draft")
    let updates = await core.recordedCalls().filter { $0 == "settings_update:revision-1" }
    try expect(updates.count == 1, "Settings mutation must remain single-flight")
    _ = await model.shutdown()
}

@MainActor
private func testSettingsEditDuringSaveIsPreserved() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setSettingsResponses([
        makeSettingsResponse(revision: "revision-1", quotaEnabled: false),
        makeSettingsResponse(revision: "revision-2", quotaEnabled: true),
    ], updateFailure: false)
    await core.setSettingsUpdateDelay(.milliseconds(120))
    let model = AppModel(runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core }))
    model.start()
    try await waitUntil("settings edit overview") { await MainActor.run { model.presentation != nil } }
    model.loadSettings()
    try await waitUntil("settings edit authoritative load") {
        await MainActor.run { model.settingsState.value?.snapshot.revision == "revision-1" }
    }
    guard var submitted = model.settingsDraft else { throw TestFailure.mismatch("settings draft missing") }
    submitted.quotaEnabled = true
    model.settingsDraft = submitted
    model.saveSettings()
    try await waitUntil("settings update starts") {
        await core.recordedCalls().contains("settings_update:revision-1")
    }
    var edited = submitted
    edited.quotaEnabled = false
    model.settingsDraft = edited
    try await waitUntil("settings save readback") {
        await MainActor.run { model.settingsState.value?.snapshot.revision == "revision-2" }
    }
    try expect(model.settingsDraft?.quotaEnabled == false, "an edit made during save must survive receipt/readback")
    if case .idle = model.settingsSaveState {
        // The preserved edit remains pending against the authoritative readback.
    } else {
        throw TestFailure.mismatch("a pending post-submit edit must return Settings to idle")
    }
    _ = await model.shutdown()
}

@MainActor
private func testSettingsEditDuringRefreshIsPreserved() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setSettingsResponses([
        makeSettingsResponse(revision: "revision-1", quotaEnabled: false),
        makeSettingsResponse(revision: "revision-1", quotaEnabled: false),
    ], updateFailure: false)
    let model = AppModel(runtime: AppRuntime(supervisor: FakeSupervisor(), clientFactory: { _ in core }))
    model.start()
    try await waitUntil("settings refresh overview") { await MainActor.run { model.presentation != nil } }
    model.loadSettings()
    try await waitUntil("settings refresh initial load") {
        await MainActor.run { model.settingsState.value?.snapshot.revision == "revision-1" }
    }
    await core.setSettingsReadDelay(.milliseconds(120))
    model.loadSettings()
    try await Task.sleep(for: .milliseconds(20))
    guard var edited = model.settingsDraft else { throw TestFailure.mismatch("settings draft missing") }
    edited.quotaEnabled = true
    model.settingsDraft = edited
    try await waitUntil("settings refresh completes") {
        await MainActor.run { !model.settingsState.isLoading }
    }
    try expect(model.settingsDraft?.quotaEnabled == true, "an edit made during refresh must not be overwritten")
    _ = await model.shutdown()
}

private func testSettingsRevisionRequest() throws {
    var response = Codexpulse_Core_V1_SettingsResponse()
    response.meta = completeMeta()
    response.snapshot.revision = "revision-1"
    response.snapshot.online.quotaEnabled = false
    response.snapshot.online.resetCreditsEnabled = true
    response.snapshot.refresh.quotaIntervalSeconds = 300
    response.snapshot.refresh.resetCreditsIntervalSeconds = 600
    response.snapshot.refresh.reconcileIntervalSeconds = 900
    response.snapshot.refresh.jsonlDebounceMilliseconds = 250
    response.snapshot.updates.autoCheckEnabled = true
    response.snapshot.updates.checkIntervalSeconds = 3_600
    response.snapshot.ui.launchBehavior = "main_window"
    response.snapshot.ui.overviewRange = "7d"
    var editable = Codexpulse_Core_V1_EditableField()
    editable.key = "online.quotaEnabled"
    editable.editable = true
    response.editableFields = [editable]

    var draft = SettingsDraft(response)
    draft.quotaEnabled = true
    draft.resetCreditsEnabled = false
    draft.quotaIntervalSeconds = 1
    let request = draft.makeRequest(authoritative: response)
    try expect(request.expectedRevision == "revision-1", "settings write must carry authoritative revision")
    try expect(request.online.quotaEnabled, "editable field must carry the draft")
    try expect(request.online.resetCreditsEnabled, "non-editable field must preserve authoritative truth")
    try expect(request.refresh.quotaIntervalSeconds == 300, "non-editable numeric field must not be shadow-edited")
}

@MainActor
private func testFeatureGenerationPreventsStaleOverwrite() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureSessionPlans([
        SessionPagePlan(delay: .milliseconds(120), response: makeSessionPage(id: "old", title: "old request")),
        SessionPagePlan(delay: .zero, response: makeSessionPage(id: "new", title: "new request")),
    ])
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })
    let model = AppModel(runtime: runtime)
    model.start()
    try await waitUntil("AppModel reaches overview") {
        await MainActor.run { model.presentation != nil }
    }
    model.loadSessions(reset: true)
    try await Task.sleep(for: .milliseconds(20))
    model.sessionOptions.projectID = "replacement"
    model.sessionFiltersChanged()
    try await waitUntil("replacement feature result") {
        await MainActor.run { model.sessionsState.value?.items.first?.sessionID == "new" }
    }
    try await Task.sleep(for: .milliseconds(140))
    try expect(model.sessionsState.value?.items.first?.sessionID == "new", "cancelled old generation overwrote replacement")
    _ = await model.shutdown()
}

private func testRequestFactoryAndPresentation() throws {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(secondsFromGMT: 0)!
    let now = Date(timeIntervalSince1970: 1_753_056_000)
    let requests = OverviewRequestSet.make(now: now, calendar: calendar)
    try expect(requests.usage.granularity == "day", "usage granularity must be day")
    try expect(requests.usage.range.timeZone == "GMT", "request timezone must remain IANA/system truth")
    try expect(requests.usage.range.endDateExclusive > requests.usage.range.startDate, "date range must be half-open")
    try expect(requests.sessions.query.page.limit == 5, "overview sessions must stay bounded")
    try expect(requests.quota.evaluatedAtMs == Int64(now.timeIntervalSince1970 * 1_000), "quota clock")

    var buddhist = Calendar(identifier: .buddhist)
    buddhist.timeZone = TimeZone(secondsFromGMT: 0)!
    let buddhistRequests = OverviewRequestSet.make(now: now, calendar: buddhist)
    try expect(buddhistRequests.usage.range.startDate == "2025-07-15", "range must use Gregorian dates")
    try expect(buddhistRequests.usage.range.endDateExclusive == "2025-07-22", "Gregorian half-open end")

    let presentation = OverviewPresentation(makeResponses(partial: true))
    try expect(presentation.isPartial, "partial response meta must remain partial")
    try expect(presentation.notices.first?.code == "index_incomplete", "stable issue must survive mapping")
    try expect(presentation.quotaWindows[0].remainingPercent == 0, "real zero remaining must survive")
    try expect(presentation.quotaWindows[1].remainingPercent == nil, "unknown quota must not become zero")
    try expect(presentation.totalTokens == .known(0, unit: "tokens"), "real zero token total")
    try expect(
        presentation.estimatedCost == .unknown(reason: "pricing_missing", unit: "usd_micros"),
        "unknown cost must keep reason"
    )
    if case .partial = AppViewState(.normal(makeResponses(partial: true))) {
        // Expected: state mapper refuses to present partial data as fully normal.
    } else {
        throw TestFailure.mismatch("normal runtime response with partial meta was presented as complete")
    }
}

private func testLaunchConfigurationBoundaries() throws {
    do {
        _ = try AppLaunchConfiguration(
            helperExecutablePath: "/usr/bin/true",
            runtimeDirectory: "/private/tmp/cp-safe/../escape"
        )
        throw TestFailure.mismatch("runtime path traversal was accepted")
    } catch AppLaunchConfigurationError.runtimeDirectoryUnavailable {
        // Expected.
    }
    let parsed = try AppLaunchConfiguration.parse(
        arguments: [
            "codex-pulse-app",
            "-psn_0_12345",
            "--helper", "/usr/bin/true",
            "--runtime-directory", "/private/tmp/cp-test-launch",
        ]
    )
    try expect(parsed.helperExecutablePath == "/usr/bin/true", "LaunchServices argument must be ignored")
}

private func testNormalLifecycleAndShutdown() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let recorder = StateRecorder()
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })
    await runtime.setStateSink { state in await recorder.append(state) }

    await runtime.start()
    var phases = await recorder.snapshot()
    try expect(phases.contains("starting"), "runtime must expose starting")
    try expect(phases.contains("handshaking"), "runtime must expose handshaking")
    try expect(phases.contains("loading_overview"), "runtime must expose overview loading")
    try expect(phases.last == "normal", "runtime must reach normal")

    let actionReceipt = try await runtime.runRuntimeAction(.reconcile)
    try expect(actionReceipt.action == "reconcile", "runtime action must use the generated Core receipt")

    await runtime.applicationWillResignActive()
    await runtime.applicationDidBecomeActive()
    await runtime.prepareForSleep()
    await runtime.applicationDidBecomeActive()
    await runtime.resumeAfterWake()
    let calls = await core.recordedCalls()
    try expect(calls.contains("lifecycle:application_did_become_active"), "active lifecycle must reach Core")
    try expect(
        calls.filter { $0 == "lifecycle:application_did_become_active" }.count == 2,
        "active-before-wake must be replayed after stream recovery"
    )
    try expect(calls.contains("lifecycle:system_will_sleep"), "sleep lifecycle must reach Core")
    try expect(calls.contains("lifecycle:system_did_wake"), "wake lifecycle must reach Core")
    try expect(calls.contains("runtime_action:reconcile"), "confirmed runtime action must reach Core")

    let outcome = await runtime.shutdown()
    try expect(outcome == .clean, "normal shutdown must read back clean Helper exit")
    phases = await recorder.snapshot()
    try expect(phases.suffix(2) == ["shutting_down", "stopped"], "shutdown state order")
    let shutdownCalls = await core.recordedCalls()
    try expect(shutdownCalls.contains("shutdown:client_exit"), "Shutdown RPC must run")
}

private func testRecoveryAndRestartRequired() async throws {
    var bootstrap = Codexpulse_Core_V1_BootstrapResponse()
    bootstrap.mode = "recovery"
    bootstrap.recovery.version = "migration-recovery-v1"
    bootstrap.recovery.phase = "failed"
    bootstrap.recovery.stage = "migrate"
    bootstrap.recovery.code = "schema_future"
    var receipt = Codexpulse_Core_V1_MigrationRecoveryReceipt()
    receipt.restartRequired = true
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: bootstrap, recoveryReceipt: receipt, responses: makeResponses())
    let recorder = StateRecorder()
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })
    await runtime.setStateSink { state in await recorder.append(state) }

    await core.setBootstrapDelay(.milliseconds(80))
    let start = Task { await runtime.start() }
    try await waitUntil("recovery bootstrap in flight") {
        await core.recordedCalls().contains("bootstrap")
    }
    await runtime.applicationDidBecomeActive()
    await start.value
    let recoveryPhases = await recorder.snapshot()
    try expect(recoveryPhases.last == "recovery", "recovery Bootstrap must block Overview")
    let callsBeforeRetry = await core.recordedCalls()
    try expect(!callsBeforeRetry.contains("usage"), "recovery must not issue normal Overview RPCs")
    try expect(
        !callsBeforeRetry.contains("lifecycle:application_did_become_active"),
        "pending active must not bypass recovery Bootstrap"
    )
    await runtime.retryRecovery()
    let retryPhases = await recorder.snapshot()
    try expect(retryPhases.last == "restart_required", "recovery receipt must expose restart required")
    _ = await runtime.shutdown()
}

private func testStaleAndUnavailable() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let recorder = StateRecorder()
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })
    await runtime.setStateSink { state in await recorder.append(state) }
    await runtime.start()
    await core.setOverviewFailure(true)
    await runtime.refresh()
    let stalePhases = await recorder.snapshot()
    try expect(stalePhases.last == "stale", "refresh failure with snapshot must be stale")
    _ = await runtime.shutdown()

    let failedSupervisor = FakeSupervisor(startFailure: true)
    let failedRecorder = StateRecorder()
    let failedRuntime = AppRuntime(supervisor: failedSupervisor, clientFactory: { _ in core })
    await failedRuntime.setStateSink { state in await failedRecorder.append(state) }
    await failedRuntime.start()
    let unavailablePhases = await failedRecorder.snapshot()
    try expect(unavailablePhases.last == "unavailable", "startup failure must be unavailable")
}

@MainActor
private func testContractUnavailableCannotRestartLoop() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setHandshakeError(.incompatibleContract(expected: "expected", actual: "actual"))
    let model = AppModel(runtime: AppRuntime(supervisor: supervisor, clientFactory: { _ in core }))
    model.start()
    try await waitUntil("contract unavailable") {
        await MainActor.run {
            if case .unavailable(let notice) = model.state { return !notice.retryable }
            return false
        }
    }
    try expect(!model.requiresCoreRestart, "non-retryable contract failure must not be labeled restartable")
    try expect(!model.canRefreshOrRestart, "non-retryable contract failure must disable refresh/restart actions")
    model.refreshOrRestart()
    try await Task.sleep(for: .milliseconds(40))
    let counts = await supervisor.counts()
    try expect(counts.0 == 1, "non-retryable contract failure must not start a reconnect loop")
    _ = await model.shutdown()
}

private func testShutdownDuringStartup() async throws {
    let supervisor = FakeSupervisor(startDelay: .milliseconds(100))
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let recorder = StateRecorder()
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })
    await runtime.setStateSink { state in await recorder.append(state) }

    let start = Task { await runtime.start() }
    try await Task.sleep(for: .milliseconds(20))
    let outcome = await runtime.shutdown()
    await start.value

    try expect(outcome == .clean, "startup shutdown without connected Core must be clean")
    let phases = await recorder.snapshot()
    try expect(phases.last == "stopped", "stale startup callback must not overwrite stopped")
    if let stopped = phases.lastIndex(of: "stopped") {
        try expect(!phases.suffix(from: phases.index(after: stopped)).contains("cancelled"), "no cancelled after stopped")
        try expect(!phases.suffix(from: phases.index(after: stopped)).contains("unavailable"), "no unavailable after stopped")
    }
}

private func testConcurrentStartIsCoalesced() async throws {
    let supervisor = FakeSupervisor(startDelay: .milliseconds(80))
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })

    let first = Task { await runtime.start() }
    try await Task.sleep(for: .milliseconds(10))
    await runtime.start()
    await first.value

    let counts = await supervisor.counts()
    try expect(counts.0 == 1, "concurrent start must spawn exactly one Helper")
    _ = await runtime.shutdown()
}

private func testCancelledRefreshCannotOverwriteReplacement() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let recorder = StateRecorder()
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })
    await runtime.setStateSink { state in await recorder.append(state) }
    await runtime.start()

    await core.setOverviewDelay(.milliseconds(100))
    let first = Task { await runtime.refresh() }
    try await Task.sleep(for: .milliseconds(20))
    await runtime.cancelRefresh()
    await core.setOverviewDelay(.zero)
    await runtime.refresh()
    await first.value

    let phases = await recorder.snapshot()
    try expect(phases.last == "normal", "cancelled refresh must not overwrite its replacement")
    _ = await runtime.shutdown()
}

private func testPendingActiveWaitsForBootstrap() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setHandshakeDelay(.milliseconds(80))
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })

    let start = Task { await runtime.start() }
    try await waitUntil("handshake in flight") {
        await core.recordedCalls().contains("handshake")
    }
    await runtime.applicationDidBecomeActive()
    await start.value

    let calls = await core.recordedCalls()
    guard let bootstrap = calls.firstIndex(of: "bootstrap"),
          let lifecycle = calls.firstIndex(of: "lifecycle:application_did_become_active"),
          let usage = calls.firstIndex(of: "usage")
    else { throw TestFailure.mismatch("pending active calls were not delivered") }
    try expect(bootstrap < usage, "Bootstrap must precede Overview RPCs")
    try expect(bootstrap < lifecycle, "Bootstrap must precede pending active lifecycle")
    _ = await runtime.shutdown()
}

private func testSleepDuringStartupDefersOverviewUntilWake() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setHandshakeDelay(.milliseconds(80))
    let recorder = StateRecorder()
    let runtime = AppRuntime(supervisor: supervisor, clientFactory: { _ in core })
    await runtime.setStateSink { state in await recorder.append(state) }

    let start = Task { await runtime.start() }
    try await waitUntil("sleep startup handshake") {
        await core.recordedCalls().contains("handshake")
    }
    await runtime.prepareForSleep()
    await start.value

    var calls = await core.recordedCalls()
    try expect(calls.contains("lifecycle:system_will_sleep"), "startup sleep must reach Core after Bootstrap")
    try expect(!calls.contains("usage"), "startup sleep must defer Overview queries")
    await runtime.resumeAfterWake()
    calls = await core.recordedCalls()
    try expect(calls.contains("lifecycle:system_did_wake"), "startup wake must reach Core")
    try expect(calls.contains("usage"), "wake must load the deferred Overview")
    let phases = await recorder.snapshot()
    try expect(phases.last == "normal", "wake after startup sleep must reach normal")
    _ = await runtime.shutdown()
}

private final class FakeProcessMonitor: HelperProcessMonitoring, @unchecked Sendable {
    private let lock = NSLock()
    private var cancelled = false

    func cancel() {
        lock.lock()
        cancelled = true
        lock.unlock()
    }

    func isCancelled() -> Bool {
        lock.lock()
        defer { lock.unlock() }
        return cancelled
    }
}

private final class ProcessExitHarness: @unchecked Sendable {
    private let lock = NSLock()
    private var callback: (@Sendable () -> Void)?
    private var monitor: FakeProcessMonitor?

    func makeMonitor(
        processID: Int32,
        onExit: @escaping @Sendable () -> Void
    ) -> any HelperProcessMonitoring {
        let monitor = FakeProcessMonitor()
        lock.lock()
        callback = onExit
        self.monitor = monitor
        lock.unlock()
        return monitor
    }

    func triggerExit() {
        lock.lock()
        let callback = callback
        lock.unlock()
        callback?()
    }
}

private func testHelperExitBecomesStale() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    let recorder = StateRecorder()
    let exit = ProcessExitHarness()
    let runtime = AppRuntime(
        supervisor: supervisor,
        processMonitorFactory: { processID, onExit in
            exit.makeMonitor(processID: processID, onExit: onExit)
        },
        clientFactory: { _ in core }
    )
    await runtime.setStateSink { state in await recorder.append(state) }
    await runtime.start()
    exit.triggerExit()
    try await waitUntil("Helper exit state") {
        await recorder.snapshot().last == "stale"
    }
    let counts = await supervisor.counts()
    try expect(counts.1 == 1, "Helper exit must stop the supervised process")
    await runtime.restart()
    try await waitUntil("Helper exit recovery") {
        await recorder.snapshot().last == "normal"
    }
    let recoveredCounts = await supervisor.counts()
    try expect(recoveredCounts.0 == 2, "Helper exit recovery must start a fresh Helper")
    _ = await runtime.shutdown()
}

@MainActor
private func testHelperExitCannotBecomeFeatureCancelled() async throws {
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setFeatureSessionPlans([
        SessionPagePlan(delay: .zero, response: makeSessionPage(id: "last-good", title: "last good")),
        SessionPagePlan(delay: .milliseconds(250), response: makeSessionPage(id: "too-late", title: "too late")),
    ])
    let exit = ProcessExitHarness()
    let runtime = AppRuntime(
        supervisor: FakeSupervisor(),
        processMonitorFactory: { processID, onExit in
            exit.makeMonitor(processID: processID, onExit: onExit)
        },
        clientFactory: { _ in core }
    )
    let model = AppModel(runtime: runtime)
    model.start()
    try await waitUntil("feature terminal overview") { await MainActor.run { model.presentation != nil } }
    model.navigate(to: .sessions)
    try await waitUntil("feature terminal last good") {
        await MainActor.run { model.sessionsState.value?.items.first?.sessionID == "last-good" }
    }
    model.loadSessions(reset: true)
    try await Task.sleep(for: .milliseconds(20))
    exit.triggerExit()
    try await waitUntil("feature terminal stale state") {
        await MainActor.run {
            if case .stale = model.sessionsState { return true }
            return false
        }
    }
    try expect(
        model.sessionsState.value?.items.first?.sessionID == "last-good",
        "Helper exit must preserve the last good feature snapshot"
    )
    _ = await model.shutdown()
}

private func testShutdownDeadlineForcesHelperStop() async throws {
    let supervisor = FakeSupervisor()
    let core = FakeCore(bootstrap: makeNormalBootstrap(), responses: makeResponses())
    await core.setShutdownDelay(.seconds(60))
    let runtime = AppRuntime(
        supervisor: supervisor,
        shutdownRequestTimeout: .milliseconds(40),
        clientFactory: { _ in core }
    )
    await runtime.start()
    let clock = ContinuousClock()
    let started = clock.now
    let outcome = await runtime.shutdown()
    try expect(outcome == .forced, "Shutdown deadline must use forced Helper stop")
    try expect(started.duration(to: clock.now) < .seconds(1), "Shutdown deadline must stay bounded")
}

@main
struct CodexPulseAppTestMain {
    static func main() async throws {
        try testRequestFactoryAndPresentation()
        try testFeatureRequestsStateAndMerge()
        try testSettingsRevisionRequest()
        try testLaunchConfigurationBoundaries()
        try await testFeatureGenerationPreventsStaleOverwrite()
        try await testInvalidationRefreshesActivePage()
        try await testRepeatedCursorStopsPagination()
        try await testTransientCursorFailureCanRetry()
        try await testQuotaMutationIsSingleFlight()
        try await testLifecycleInvalidationPreservesMutation()
        try await testSettingsConflictPreservesDraft()
        try await testSettingsEditDuringSaveIsPreserved()
        try await testSettingsEditDuringRefreshIsPreserved()
        try await testNormalLifecycleAndShutdown()
        try await testRecoveryAndRestartRequired()
        try await testStaleAndUnavailable()
        try await testContractUnavailableCannotRestartLoop()
        try await testShutdownDuringStartup()
        try await testConcurrentStartIsCoalesced()
        try await testCancelledRefreshCannotOverwriteReplacement()
        try await testPendingActiveWaitsForBootstrap()
        try await testSleepDuringStartupDefersOverviewUntilWake()
        try await testHelperExitBecomesStale()
        try await testHelperExitCannotBecomeFeatureCancelled()
        try await testShutdownDeadlineForcesHelperStop()
        print("CodexPulseApp deterministic tests passed")
    }
}
