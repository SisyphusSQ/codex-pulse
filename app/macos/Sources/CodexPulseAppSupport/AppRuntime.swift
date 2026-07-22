import CodexPulseCoreClient
import CodexPulseProtocolGenerated
import Foundation

private enum ShutdownRequestResult: Equatable, Sendable {
    case accepted
    case failed
    case timedOut
}

private final class OneShot<Value: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var result: Value?
    private var continuation: CheckedContinuation<Value, Never>?

    func wait() async -> Value {
        await withCheckedContinuation { continuation in
            lock.lock()
            if let result {
                lock.unlock()
                continuation.resume(returning: result)
            } else {
                self.continuation = continuation
                lock.unlock()
            }
        }
    }

    func resolve(_ value: Value) {
        lock.lock()
        guard result == nil else {
            lock.unlock()
            return
        }
        result = value
        let continuation = continuation
        self.continuation = nil
        lock.unlock()
        continuation?.resume(returning: value)
    }
}

public enum AppRuntimeError: Error, Equatable, Sendable {
    case alreadyStarted
    case unavailable
    case invalidBootstrap
}

public enum ShutdownOutcome: Equatable, Sendable {
    case clean
    case forced
    case uncertain
}

public actor AppRuntime {
    public typealias StateSink = @Sendable (CoreConnectionState) async -> Void
    public typealias InvalidationSink = @Sendable (_ domain: String) async -> Void
    public typealias ClientFactory = @Sendable (RunningHelper) throws -> any AppCoreServing
    public typealias ProcessMonitorFactory = @Sendable (
        _ processID: Int32,
        _ onExit: @escaping @Sendable () -> Void
    ) -> any HelperProcessMonitoring

    private let supervisor: any HelperSupervising
    private let clientFactory: ClientFactory
    private let clientVersion: String
    private let sendLifecycleToHelper: Bool
    private let shutdownRequestTimeout: Duration
    private let processMonitorFactory: ProcessMonitorFactory?
    private var stateSink: StateSink = { _ in }
    private var invalidationSink: InvalidationSink = { _ in }
    private var client: (any AppCoreServing)?
    private var streamController: InvalidationStreamController?
    private var helperProcessMonitor: (any HelperProcessMonitoring)?
    private var refreshTask: Task<OverviewResponses, any Error>?
    private var lastResponses: OverviewResponses?
    private var runtimeGeneration: UInt64 = 0
    private var refreshGeneration: UInt64 = 0
    private var startInFlight = false
    private var shuttingDown = false
    private var applicationIsActive = true
    private var systemIsSleeping = false
    private var sleepLifecycleDelivered = false
    private var readyForOverview = false
    private var activeRefreshPending = false

    public init(configuration: AppLaunchConfiguration) {
        self.supervisor = HelperSupervisor(configuration: .init(
            executablePath: configuration.helperExecutablePath,
            runtimeDirectory: configuration.runtimeDirectory
        ))
        self.clientVersion = configuration.clientVersion
        self.sendLifecycleToHelper = configuration.sendLifecycleToHelper
        self.shutdownRequestTimeout = .seconds(5)
        self.processMonitorFactory = { processID, onExit in
            DispatchHelperProcessMonitor(processID: processID, onExit: onExit)
        }
        self.clientFactory = { helper in
            try CoreClient(socketPath: helper.socketPath, bearerToken: helper.bearerToken)
        }
    }

    public init(
        supervisor: any HelperSupervising,
        clientVersion: String = "test",
        sendLifecycleToHelper: Bool = true,
        shutdownRequestTimeout: Duration = .seconds(5),
        processMonitorFactory: ProcessMonitorFactory? = nil,
        clientFactory: @escaping ClientFactory
    ) {
        self.supervisor = supervisor
        self.clientVersion = clientVersion
        self.sendLifecycleToHelper = sendLifecycleToHelper
        self.shutdownRequestTimeout = shutdownRequestTimeout
        self.processMonitorFactory = processMonitorFactory
        self.clientFactory = clientFactory
    }

    public func setStateSink(_ sink: @escaping StateSink) {
        stateSink = sink
    }

    public func setInvalidationSink(_ sink: @escaping InvalidationSink) {
        invalidationSink = sink
    }

    public func start() async {
        guard client == nil, !startInFlight, !shuttingDown else {
            await emit(.unavailable(AppNotice(
                code: "already_started",
                messageKey: "app.error.already_started",
                retryable: false
            )))
            return
        }
        startInFlight = true
        defer { startInFlight = false }
        runtimeGeneration &+= 1
        let generation = runtimeGeneration
        readyForOverview = false
        await emit(.starting)
        do {
            let helper = try await supervisor.start()
            try Task.checkCancellation()
            guard generation == runtimeGeneration else { throw CancellationError() }
            installHelperProcessMonitor(processID: helper.processID, generation: generation)
            await emit(.handshaking)
            let connectedClient = try clientFactory(helper)
            client = connectedClient
            _ = try await connectedClient.handshake(
                clientName: "codex-pulse-macos",
                clientVersion: clientVersion,
                retryPolicy: .transportDefault
            )
            guard generation == runtimeGeneration else { throw CancellationError() }
            let bootstrap = try await connectedClient.bootstrap(retryPolicy: .transportDefault)
            guard generation == runtimeGeneration else { throw CancellationError() }
            switch BootstrapState(bootstrap) {
            case .normal:
                readyForOverview = true
                if systemIsSleeping {
                    await suspendWithoutStream(client: connectedClient, generation: generation)
                    return
                }
                await refresh(showLoading: true)
                guard generation == runtimeGeneration, client != nil else { return }
                if systemIsSleeping {
                    await suspendWithoutStream(client: connectedClient, generation: generation)
                    return
                }
                await startInvalidationStream(client: connectedClient, generation: generation)
                await deliverPendingActive(client: connectedClient, generation: generation)
            case .recovery(let snapshot):
                readyForOverview = false
                lastResponses = nil
                await emit(.recovery(snapshot))
            case .unsupported:
                throw AppRuntimeError.invalidBootstrap
            }
        } catch is CancellationError {
            guard generation == runtimeGeneration else { return }
            await closeFailedStartup()
            await emit(.cancelled)
        } catch {
            guard generation == runtimeGeneration else { return }
            await closeFailedStartup()
            await emit(.unavailable(AppNotice.from(error)))
        }
    }

    public func refresh() async {
        await refresh(showLoading: false)
    }

    public func usageCost(
        _ request: Codexpulse_Core_V1_UsageCostRequest
    ) async throws -> Codexpulse_Core_V1_UsageCostResponse {
        try await performRead { try await $0.usageCost(request, retryPolicy: .transportDefault) }
    }

    public func quotaCurrent(
        _ request: Codexpulse_Core_V1_QuotaCurrentRequest
    ) async throws -> Codexpulse_Core_V1_QuotaCurrentResponse {
        try await performRead { try await $0.quotaCurrent(request, retryPolicy: .transportDefault) }
    }

    public func requestQuotaRefresh(
        source: String
    ) async throws -> Codexpulse_Core_V1_QuotaRefreshReceipt {
        guard source == "quota" || source == "reset_credits" else {
            throw AppRuntimeError.unavailable
        }
        var request = Codexpulse_Core_V1_QuotaRefreshRequest()
        request.source = source
        let preparedRequest = request
        return try await performMutation { try await $0.requestQuotaRefresh(preparedRequest) }
    }

    public func runRuntimeAction(
        _ action: RuntimeControlAction
    ) async throws -> Codexpulse_Core_V1_RuntimeActionReceipt {
        var request = Codexpulse_Core_V1_RuntimeActionRequest()
        request.action = action.rawValue
        let preparedRequest = request
        return try await performMutation { try await $0.runRuntimeAction(preparedRequest) }
    }

    public func listSessions(
        _ request: Codexpulse_Core_V1_ListSessionsRequest
    ) async throws -> Codexpulse_Core_V1_SessionListResponse {
        try await performRead { try await $0.listSessions(request, retryPolicy: .transportDefault) }
    }

    public func sessionDetail(
        _ request: Codexpulse_Core_V1_SessionDetailRequest
    ) async throws -> Codexpulse_Core_V1_SessionDetailResponse {
        try await performRead { try await $0.sessionDetail(request, retryPolicy: .transportDefault) }
    }

    public func listProjects(
        _ request: Codexpulse_Core_V1_ListProjectsRequest
    ) async throws -> Codexpulse_Core_V1_ProjectListResponse {
        try await performRead { try await $0.listProjects(request, retryPolicy: .transportDefault) }
    }

    public func projectDetail(
        _ request: Codexpulse_Core_V1_ProjectDetailRequest
    ) async throws -> Codexpulse_Core_V1_ProjectDetailResponse {
        try await performRead { try await $0.projectDetail(request, retryPolicy: .transportDefault) }
    }

    public func listSources(
        _ request: Codexpulse_Core_V1_ListSourcesRequest
    ) async throws -> Codexpulse_Core_V1_SourceListResponse {
        try await performRead { try await $0.listSources(request, retryPolicy: .transportDefault) }
    }

    public func source(
        key: String
    ) async throws -> Codexpulse_Core_V1_SourceDetailResponse {
        guard !key.isEmpty else { throw AppRuntimeError.unavailable }
        var request = Codexpulse_Core_V1_SourceRequest()
        request.sourceKey = key
        let preparedRequest = request
        return try await performRead { try await $0.source(preparedRequest, retryPolicy: .transportDefault) }
    }

    public func listJobs(
        _ request: Codexpulse_Core_V1_ListJobsRequest
    ) async throws -> Codexpulse_Core_V1_JobListResponse {
        try await performRead { try await $0.listJobs(request, retryPolicy: .transportDefault) }
    }

    public func job(
        id: String
    ) async throws -> Codexpulse_Core_V1_JobDetailResponse {
        guard !id.isEmpty else { throw AppRuntimeError.unavailable }
        var request = Codexpulse_Core_V1_JobRequest()
        request.jobID = id
        let preparedRequest = request
        return try await performRead { try await $0.job(preparedRequest, retryPolicy: .transportDefault) }
    }

    public func listHealth(
        _ request: Codexpulse_Core_V1_ListHealthRequest
    ) async throws -> Codexpulse_Core_V1_HealthListResponse {
        try await performRead { try await $0.listHealth(request, retryPolicy: .transportDefault) }
    }

    public func health(
        eventID: String
    ) async throws -> Codexpulse_Core_V1_HealthDetailResponse {
        guard !eventID.isEmpty else { throw AppRuntimeError.unavailable }
        var request = Codexpulse_Core_V1_HealthRequest()
        request.eventID = eventID
        let preparedRequest = request
        return try await performRead { try await $0.health(preparedRequest, retryPolicy: .transportDefault) }
    }

    public func healthProjection() async throws -> Codexpulse_Core_V1_HealthProjectionResponse {
        try await performRead { try await $0.healthProjection(retryPolicy: .transportDefault) }
    }

    public func dataHealth(
        _ request: Codexpulse_Core_V1_DataHealthRequest
    ) async throws -> Codexpulse_Core_V1_DataHealthResponse {
        try await performRead { try await $0.dataHealth(request, retryPolicy: .transportDefault) }
    }

    public func settings() async throws -> Codexpulse_Core_V1_SettingsResponse {
        try await performRead { try await $0.settings(retryPolicy: .transportDefault) }
    }

    public func updateSettings(
        _ request: Codexpulse_Core_V1_UpdateSettingsRequest
    ) async throws -> Codexpulse_Core_V1_SettingsUpdateReceipt {
        try await performMutation { try await $0.updateSettings(request) }
    }

    public func primaryPagesSmoke(
        now: Date = Date(),
        calendar: Calendar = .current
    ) async throws -> PrimaryPagesSmokeSummary {
        var step = "usage"
        do {
            var unavailableSteps: [String] = []
            var usage: Codexpulse_Core_V1_UsageCostResponse?
            do {
                usage = try await usageCost(FeatureRequestFactory.usage(range: .sevenDays, now: now, calendar: calendar))
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }
            step = "quota"
            var quota: Codexpulse_Core_V1_QuotaCurrentResponse?
            do {
                quota = try await quotaCurrent(FeatureRequestFactory.quota(now: now))
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }
            step = "sessions"
            var sessions: Codexpulse_Core_V1_SessionListResponse?
            do {
                sessions = try await listSessions(FeatureRequestFactory.sessions(options: .init(), limit: 20, now: now, calendar: calendar))
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }
            step = "projects"
            var projects: Codexpulse_Core_V1_ProjectListResponse?
            do {
                projects = try await listProjects(FeatureRequestFactory.projects(options: .init(), limit: 20, now: now, calendar: calendar))
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }
            step = "sources"
            var sources: Codexpulse_Core_V1_SourceListResponse?
            do {
                sources = try await listSources(FeatureRequestFactory.sources(options: .init(), limit: 20))
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }
            step = "jobs"
            var jobs: Codexpulse_Core_V1_JobListResponse?
            do {
                jobs = try await listJobs(FeatureRequestFactory.jobs(options: .init(), limit: 20))
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }
            step = "health_events"
            var healthEvents: Codexpulse_Core_V1_HealthListResponse?
            do {
                healthEvents = try await listHealth(FeatureRequestFactory.health(options: .init(), limit: 20))
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }
            step = "health_projection"
            do {
                _ = try await healthProjection()
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }
            step = "data_health"
            do {
                _ = try await dataHealth(FeatureRequestFactory.dataHealth(now: now))
            } catch {
                unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
            }

            var detailsRead = 0
            if let item = sessions?.items.first {
                step = "session_detail"
                do {
                    _ = try await sessionDetail(FeatureRequestFactory.sessionDetail(sessionID: item.sessionID))
                    detailsRead += 1
                } catch {
                    unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
                }
            }
            if let item = projects?.items.first {
                step = "project_detail"
                do {
                    _ = try await projectDetail(FeatureRequestFactory.projectDetail(
                        dimensionKey: item.dimensionKey,
                        range: .thirtyDays,
                        now: now,
                        calendar: calendar
                    ))
                    detailsRead += 1
                } catch {
                    unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
                }
            }
            if let item = sources?.items.first {
                step = "source_detail"
                do {
                    _ = try await source(key: item.sourceKey)
                    detailsRead += 1
                } catch {
                    unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
                }
            }
            if let item = jobs?.items.first {
                step = "job_detail"
                do {
                    _ = try await job(id: item.jobID)
                    detailsRead += 1
                } catch {
                    unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
                }
            }
            if let item = healthEvents?.items.first {
                step = "health_detail"
                do {
                    _ = try await health(eventID: item.eventID)
                    detailsRead += 1
                } catch {
                    unavailableSteps.append(try acceptedSmokeFailure(step: step, error: error))
                }
            }

            step = "settings"
            return PrimaryPagesSmokeSummary(
                sessions: sessions?.items.count ?? 0,
                projects: projects?.items.count ?? 0,
                sources: sources?.items.count ?? 0,
                jobs: jobs?.items.count ?? 0,
                healthEvents: healthEvents?.items.count ?? 0,
                usageTrend: usage?.trend.count ?? 0,
                usageModels: usage?.models.count ?? 0,
                usageCostKnown: usage?.totals.estimatedUsdMicros.hasValue == true,
                quotaWindows: quota?.current.windows.count ?? 0,
                detailsRead: detailsRead,
                settingsMutation: try await settingsMutationSmoke(),
                unavailableSteps: unavailableSteps
            )
        } catch let error as PrimaryPagesSmokeError {
            throw error
        } catch {
            throw PrimaryPagesSmokeError(step: step)
        }
    }

    private func acceptedSmokeFailure(step: String, error: any Error) throws -> String {
        let notice = AppNotice.from(error)
        guard ["not_found", "partial", "unavailable", "deadline_exceeded"].contains(notice.code) else {
            throw PrimaryPagesSmokeError(step: step)
        }
        return "\(step)_\(notice.code)"
    }

    private func settingsMutationSmoke() async throws -> String {
        var step = "settings_read"
        do {
            let original = try await settings()
            let originalDraft = SettingsDraft(original)
            var changedDraft = originalDraft
            let editable = original.editableFields.filter(\.editable)
            var hasChange = false

            if let field = editable.first(where: { $0.key == "ui.launchBehavior" }),
               let alternate = field.options.first(where: { $0 != originalDraft.launchBehavior }) {
                changedDraft.launchBehavior = alternate
                hasChange = true
            } else if let field = editable.first(where: { $0.key == "ui.overviewRange" }),
                      let alternate = field.options.first(where: { $0 != originalDraft.overviewRange }) {
                changedDraft.overviewRange = alternate
                hasChange = true
            } else if editable.contains(where: { $0.key == "online.quotaEnabled" }) {
                changedDraft.quotaEnabled.toggle()
                hasChange = true
            }

            guard hasChange else { return "readback_only" }
            step = "settings_apply"
            let appliedReceipt = try await updateSettings(changedDraft.makeRequest(authoritative: original))
            step = "settings_apply_readback"
            var authoritative = try await settings()
            guard authoritative.snapshot.revision == appliedReceipt.revision,
                  SettingsDraft(authoritative) == changedDraft
            else {
                try? await restoreSettings(originalDraft, authoritative: authoritative)
                throw AppRuntimeError.invalidBootstrap
            }

            step = "settings_conflict"
            var conflictObserved = false
            do {
                _ = try await updateSettings(originalDraft.makeRequest(authoritative: original))
                authoritative = try await settings()
            } catch {
                conflictObserved = true
                step = "settings_conflict_readback"
                authoritative = try await settings()
            }

            step = "settings_restore"
            try await restoreSettings(originalDraft, authoritative: authoritative)
            step = "settings_restore_readback"
            let restored = try await settings()
            guard SettingsDraft(restored) == originalDraft, conflictObserved else {
                throw AppRuntimeError.invalidBootstrap
            }
            return "receipt+readback+conflict+restored"
        } catch let error as PrimaryPagesSmokeError {
            throw error
        } catch {
            throw PrimaryPagesSmokeError(step: step)
        }
    }

    private func restoreSettings(
        _ original: SettingsDraft,
        authoritative: Codexpulse_Core_V1_SettingsResponse
    ) async throws {
        let receipt = try await updateSettings(original.makeRequest(authoritative: authoritative))
        let readback = try await settings()
        guard readback.snapshot.revision == receipt.revision, SettingsDraft(readback) == original else {
            throw AppRuntimeError.invalidBootstrap
        }
    }

    public func cancelRefresh() async {
        refreshGeneration &+= 1
        refreshTask?.cancel()
        refreshTask = nil
        await emit(.cancelled)
    }

    private func performRead<Value: Sendable>(
        _ operation: @Sendable (any AppCoreServing) async throws -> Value
    ) async throws -> Value {
        guard readyForOverview, !shuttingDown, let client else {
            throw AppRuntimeError.unavailable
        }
        let generation = runtimeGeneration
        try Task.checkCancellation()
        let value = try await operation(client)
        try Task.checkCancellation()
        guard generation == runtimeGeneration, readyForOverview, !shuttingDown else {
            throw CancellationError()
        }
        return value
    }

    private func performMutation<Value: Sendable>(
        _ operation: @Sendable (any AppCoreServing) async throws -> Value
    ) async throws -> Value {
        guard readyForOverview, !shuttingDown, let client else {
            throw AppRuntimeError.unavailable
        }
        let generation = runtimeGeneration
        try Task.checkCancellation()
        let value = try await operation(client)
        try Task.checkCancellation()
        guard generation == runtimeGeneration, readyForOverview, !shuttingDown else {
            throw CancellationError()
        }
        return value
    }

    public func applicationWillResignActive() {
        applicationIsActive = false
        activeRefreshPending = false
    }

    public func applicationDidBecomeActive() async {
        applicationIsActive = true
        guard !shuttingDown, !systemIsSleeping else {
            activeRefreshPending = true
            return
        }
        guard readyForOverview, let client else {
            activeRefreshPending = true
            return
        }
        let generation = runtimeGeneration
        await notifyActiveAndRefresh(client: client, generation: generation)
    }

    public func prepareForSleep() async {
        systemIsSleeping = true
        refreshGeneration &+= 1
        refreshTask?.cancel()
        refreshTask = nil
        let generation = runtimeGeneration
        guard let streamController else {
            if readyForOverview, let client {
                await suspendWithoutStream(client: client, generation: generation)
            }
            return
        }
        do {
            try await streamController.prepareForSleep(sendLifecycle: sendLifecycleToHelper)
            guard generation == runtimeGeneration, !shuttingDown else { return }
            sleepLifecycleDelivered = sendLifecycleToHelper
        } catch {
            guard generation == runtimeGeneration, !shuttingDown else { return }
            await emitRefreshFailure(error)
        }
    }

    public func resumeAfterWake() async {
        systemIsSleeping = false
        let generation = runtimeGeneration
        guard let streamController else {
            guard readyForOverview, let client else {
                sleepLifecycleDelivered = false
                return
            }
            do {
                if sendLifecycleToHelper, sleepLifecycleDelivered {
                    _ = try await client.notifyLifecycle(.systemDidWake)
                    guard generation == runtimeGeneration, !shuttingDown else { return }
                }
                sleepLifecycleDelivered = false
                await refresh(showLoading: lastResponses == nil)
                guard generation == runtimeGeneration, !shuttingDown else { return }
                await startInvalidationStream(client: client, generation: generation)
                await deliverPendingActive(client: client, generation: generation)
            } catch {
                guard generation == runtimeGeneration, !shuttingDown else { return }
                await emitRefreshFailure(error)
            }
            return
        }
        do {
            try await streamController.resumeAfterWake(sendLifecycle: sendLifecycleToHelper)
            guard generation == runtimeGeneration, !shuttingDown else { return }
            try await streamController.waitUntilReady()
            guard generation == runtimeGeneration, !shuttingDown else { return }
            sleepLifecycleDelivered = false
            await refresh(showLoading: false)
            guard generation == runtimeGeneration, !shuttingDown, let client else { return }
            await deliverPendingActive(client: client, generation: generation)
        } catch {
            guard generation == runtimeGeneration, !shuttingDown else { return }
            await emitRefreshFailure(error)
        }
    }

    public func retryRecovery() async {
        guard !shuttingDown, let client else {
            await emit(.unavailable(AppNotice(
                code: "core_unavailable",
                messageKey: "app.error.core_unavailable",
                retryable: true
            )))
            return
        }
        let generation = runtimeGeneration
        do {
            let receipt = try await client.migrationRecoveryRetry()
            guard generation == runtimeGeneration, !shuttingDown else { return }
            if RecoveryTransition(receipt) == .restartRequired {
                await emit(.restartRequired)
                return
            }
            let bootstrap = try await client.bootstrap(retryPolicy: .transportDefault)
            guard generation == runtimeGeneration, !shuttingDown else { return }
            switch BootstrapState(bootstrap) {
            case .recovery(let snapshot):
                readyForOverview = false
                await emit(.recovery(snapshot))
            case .normal:
                readyForOverview = true
                if systemIsSleeping {
                    await suspendWithoutStream(client: client, generation: generation)
                    return
                }
                await refresh(showLoading: true)
                guard generation == runtimeGeneration, !shuttingDown else { return }
                if systemIsSleeping {
                    await suspendWithoutStream(client: client, generation: generation)
                    return
                }
                await startInvalidationStream(client: client, generation: runtimeGeneration)
                await deliverPendingActive(client: client, generation: generation)
            case .unsupported: throw AppRuntimeError.invalidBootstrap
            }
        } catch {
            guard generation == runtimeGeneration, !shuttingDown else { return }
            await emitRefreshFailure(error)
        }
    }

    public func restart() async {
        guard !startInFlight, !shuttingDown else { return }
        runtimeGeneration &+= 1
        _ = await stopCurrentCore(reason: "client_restart")
        await start()
    }

    public func shutdown() async -> ShutdownOutcome {
        guard !shuttingDown else { return .uncertain }
        shuttingDown = true
        runtimeGeneration &+= 1
        readyForOverview = false
        activeRefreshPending = false
        sleepLifecycleDelivered = false
        refreshGeneration &+= 1
        refreshTask?.cancel()
        refreshTask = nil
        await emit(.shuttingDown)
        let outcome = await stopCurrentCore(reason: "client_exit")
        shuttingDown = false
        await emit(.stopped)
        return outcome
    }

    private func refresh(showLoading: Bool) async {
        guard !shuttingDown, let client else { return }
        if let refreshTask {
            _ = try? await refreshTask.value
            return
        }
        if showLoading || lastResponses == nil { await emit(.loadingOverview) }
        let requests = OverviewRequestSet.make()
        refreshGeneration &+= 1
        let generation = refreshGeneration
        let task = Task<OverviewResponses, any Error> {
            async let usage = client.usageCost(requests.usage, retryPolicy: .transportDefault)
            async let quota = client.quotaCurrent(requests.quota, retryPolicy: .transportDefault)
            async let sessions = client.listSessions(requests.sessions, retryPolicy: .transportDefault)
            async let health = client.healthProjection(retryPolicy: .transportDefault)
            let values = try await (usage, quota, sessions, health)
            return OverviewResponses(
                usage: values.0,
                quota: values.1,
                sessions: values.2,
                health: values.3
            )
        }
        refreshTask = task
        do {
            let responses = try await task.value
            guard generation == refreshGeneration, refreshTask != nil, !shuttingDown else { return }
            refreshTask = nil
            lastResponses = responses
            let presentation = OverviewPresentation(responses)
            if presentation.isPartial {
                await emit(.partial(responses, presentation.notices))
            } else {
                await emit(.normal(responses))
            }
        } catch is CancellationError {
            guard generation == refreshGeneration else { return }
            refreshTask = nil
            await emit(.cancelled)
        } catch {
            guard generation == refreshGeneration else { return }
            refreshTask = nil
            await emitRefreshFailure(error)
        }
    }

    private func startInvalidationStream(
        client: any AppCoreServing,
        generation: UInt64
    ) async {
        guard streamController == nil, generation == runtimeGeneration else { return }
        let runtime = self
        let controller = InvalidationStreamController(
            domains: ["index", "quota", "health", "settings"],
            consumeInvalidations: { domains, afterSequence, onReady, onEvent in
                try await client.consumeInvalidations(
                    domains: domains,
                    afterSequence: afterSequence,
                    onReady: onReady,
                    onEvent: onEvent
                )
            },
            notifyLifecycle: { event in
                _ = try await client.notifyLifecycle(event)
            },
            onTerminalFailure: {
                await runtime.handleRuntimeFailure(
                    generation: generation,
                    streamIsAlreadyTerminal: true,
                    code: "invalidation_stream_failed"
                )
            },
            onEvent: { event in await runtime.handleInvalidation(domain: event.domain) }
        )
        streamController = controller
        await controller.start()
    }

    private func handleInvalidation(domain: String) async {
        await invalidationSink(domain)
        guard applicationIsActive, !systemIsSleeping else {
            return
        }
        if domain != "settings" { await refresh(showLoading: false) }
    }

    private func notifyActiveAndRefresh(
        client: any AppCoreServing,
        generation: UInt64
    ) async {
        if sendLifecycleToHelper {
            do {
                _ = try await client.notifyLifecycle(.applicationDidBecomeActive)
                guard generation == runtimeGeneration, !shuttingDown, readyForOverview else { return }
            } catch {
                guard generation == runtimeGeneration, !shuttingDown else { return }
                await emitRefreshFailure(error)
                return
            }
        }
        await invalidationSink("lifecycle")
        await refresh(showLoading: false)
    }

    private func deliverPendingActive(
        client: any AppCoreServing,
        generation: UInt64
    ) async {
        guard activeRefreshPending, applicationIsActive, !systemIsSleeping,
              generation == runtimeGeneration, readyForOverview
        else { return }
        activeRefreshPending = false
        await notifyActiveAndRefresh(client: client, generation: generation)
    }

    private func suspendWithoutStream(
        client: any AppCoreServing,
        generation: UInt64
    ) async {
        guard systemIsSleeping, generation == runtimeGeneration, !shuttingDown else { return }
        do {
            if sendLifecycleToHelper, !sleepLifecycleDelivered {
                _ = try await client.notifyLifecycle(.systemWillSleep)
                guard generation == runtimeGeneration, systemIsSleeping, !shuttingDown else { return }
                sleepLifecycleDelivered = true
            }
            await emit(.cancelled)
        } catch {
            guard generation == runtimeGeneration, !shuttingDown else { return }
            await emitRefreshFailure(error)
        }
    }

    private func installHelperProcessMonitor(processID: Int32, generation: UInt64) {
        guard let processMonitorFactory else { return }
        let runtime = self
        helperProcessMonitor?.cancel()
        helperProcessMonitor = processMonitorFactory(processID) {
            Task {
                await runtime.handleRuntimeFailure(
                    generation: generation,
                    streamIsAlreadyTerminal: false,
                    code: "helper_exited"
                )
            }
        }
    }

    private func handleRuntimeFailure(
        generation: UInt64,
        streamIsAlreadyTerminal: Bool,
        code: String
    ) async {
        guard generation == runtimeGeneration, !shuttingDown else { return }
        runtimeGeneration &+= 1
        let failureGeneration = runtimeGeneration
        readyForOverview = false
        activeRefreshPending = applicationIsActive
        sleepLifecycleDelivered = false
        refreshGeneration &+= 1
        refreshTask?.cancel()
        refreshTask = nil
        helperProcessMonitor?.cancel()
        helperProcessMonitor = nil
        if let streamController {
            self.streamController = nil
            if !streamIsAlreadyTerminal { await streamController.stop() }
        }
        if let client {
            self.client = nil
            Task { await client.closeTransport() }
        }
        await supervisor.stop(mode: .terminate)
        guard failureGeneration == runtimeGeneration, !shuttingDown else { return }
        await emitRefreshFailure(AppNotice(
            code: code,
            messageKey: "app.error.core_unavailable",
            retryable: true
        ))
    }

    private func emitRefreshFailure(_ error: any Error) async {
        await emitRefreshFailure(AppNotice.from(error))
    }

    private func emitRefreshFailure(_ notice: AppNotice) async {
        if let lastResponses {
            await emit(.stale(lastResponses, notice))
        } else if notice.code == "cancelled" {
            await emit(.cancelled)
        } else {
            await emit(.unavailable(notice))
        }
    }

    private func stopCurrentCore(reason: String) async -> ShutdownOutcome {
        readyForOverview = false
        sleepLifecycleDelivered = false
        helperProcessMonitor?.cancel()
        helperProcessMonitor = nil
        refreshGeneration &+= 1
        refreshTask?.cancel()
        refreshTask = nil
        if let streamController {
            await streamController.stop()
            self.streamController = nil
        }
        lastResponses = nil

        guard let client else {
            await supervisor.stop(mode: .terminate)
            return .clean
        }
        self.client = nil
        let shutdownResult = await boundedShutdownRequest(client: client, reason: reason)
        if shutdownResult == .accepted {
            do {
                let status = try await supervisor.waitForExit(timeout: .seconds(10))
                return status == 0 ? .clean : .uncertain
            } catch {
                // Fall through to the forced bounded shutdown path.
            }
        }
        Task { await client.closeTransport() }
        await supervisor.stop(mode: .terminate)
        do {
            _ = try await supervisor.waitForExit(timeout: .seconds(1))
            return .forced
        } catch {
            return .uncertain
        }
    }

    private func boundedShutdownRequest(
        client: any AppCoreServing,
        reason: String
    ) async -> ShutdownRequestResult {
        let completion = OneShot<ShutdownRequestResult>()
        let requestTask = Task {
            do {
                try await client.shutdown(reason: reason)
                completion.resolve(.accepted)
            } catch {
                completion.resolve(.failed)
            }
        }
        let timeout = shutdownRequestTimeout
        let timeoutTask = Task {
            do {
                try await Task.sleep(for: timeout)
                completion.resolve(.timedOut)
            } catch {
                // The request completed before the deadline.
            }
        }
        let result = await completion.wait()
        requestTask.cancel()
        timeoutTask.cancel()
        return result
    }

    private func closeFailedStartup() async {
        runtimeGeneration &+= 1
        readyForOverview = false
        sleepLifecycleDelivered = false
        helperProcessMonitor?.cancel()
        helperProcessMonitor = nil
        if let client {
            self.client = nil
            Task { await client.closeTransport() }
        }
        if let streamController {
            await streamController.stop()
            self.streamController = nil
        }
        refreshGeneration &+= 1
        refreshTask?.cancel()
        refreshTask = nil
        await supervisor.stop(mode: .terminate)
    }

    private func emit(_ state: CoreConnectionState) async {
        await stateSink(state)
    }
}
