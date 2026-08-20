import Charts
import CodexPulseAppSupport
import SwiftUI

struct RootView: View {
    @ObservedObject var model: AppModel
    @ObservedObject var loginItemSettings: LoginItemSettingsModel

    private var localization: AppLocalization { model.localization }

    var body: some View {
        NavigationSplitView {
            List(selection: selection) {
				Section {
					Picker("客户端", selection: providerSelection) {
						ForEach(AgentProvider.allCases) { provider in
							Text(provider.title).tag(provider)
						}
					}
					.labelsHidden()
					.pickerStyle(.menu)
					.frame(maxWidth: .infinity, alignment: .leading)
					.accessibilityIdentifier("sidebar.provider-picker")
				} header: {
					Text("客户端")
				}
                Section(localization.text("sidebar.section.usage")) {
					ForEach(AppFeature.usageFeatures(for: model.selectedProvider)) { section in
                        Label(section.title(localization: localization), systemImage: section.symbol)
                            .tag(section)
                    }
                }
                Section(localization.text("sidebar.section.apiSubscriptions")) {
                    Label(
                        AppFeature.apiSubscriptions.title(localization: localization),
                        systemImage: AppFeature.apiSubscriptions.symbol
                    )
                    .tag(AppFeature.apiSubscriptions)
                }
                Section(localization.text("sidebar.section.system")) {
                    ForEach([AppFeature.localStatus, .sourcesJobs, .settings]) { section in
                        Label(section.title(localization: localization), systemImage: section.symbol)
                            .tag(section)
                    }
                }
            }
            .listStyle(.sidebar)
            .navigationTitle("Codex Pulse")
            .frame(minWidth: 190)
        } detail: {
            featureContent
				.navigationTitle(navigationTitle)
                .toolbar {
                    ToolbarItemGroup(placement: .primaryAction) {
                        Button {
                            model.refresh(model.selectedFeature)
                        } label: {
                            currentReloadLabel
                        }
                        .keyboardShortcut("r", modifiers: .command)
                        .disabled(
                            !model.canRefreshOrRestart
                                || model.isRefreshingAll
                                || model.isRefreshing(model.selectedFeature)
                        )
                        .help(currentReloadHelp)
                        .accessibilityIdentifier("toolbar.refresh.current")
                        Menu {
                            Button(
                                "重新加载所有页面",
                                systemImage: "arrow.triangle.2.circlepath"
                            ) {
                                model.refreshAllFeatures()
                            }
                            .accessibilityIdentifier("toolbar.refresh.all")
                        } label: {
                            reloadOptionsLabel
                        }
                        .disabled(
                            !model.canRefreshOrRestart
                                || model.requiresCoreRestart
                                || model.isRefreshingAll
                                || model.isRefreshing(model.selectedFeature)
                        )
                        .help(reloadOptionsHelp)
                        .accessibilityIdentifier("toolbar.reload.options")
                    }
                }
        }
        .frame(minWidth: 820, minHeight: 560)
        .id(model.localization.preference.rawValue)
        .environment(\.locale, model.localization.locale)
        .onChange(of: model.selectedFeature) { _, next in
            model.load(next)
            model.markFeatureRendered(next)
        }
    }

    private var selection: Binding<AppFeature?> {
        Binding(
            get: { model.selectedFeature },
            set: { if let feature = $0 { model.navigate(to: feature) } }
        )
    }

    private var providerSelection: Binding<AgentProvider> {
        Binding(
            get: { model.selectedProvider },
            set: { provider in model.selectProvider(provider) }
        )
    }

    private var navigationTitle: String {
        let featureTitle = model.selectedFeature.title(localization: localization)
        return model.selectedFeature == .apiSubscriptions
            ? featureTitle
            : "\(featureTitle) · \(model.selectedProvider.title)"
    }

    @ViewBuilder
    private var currentReloadLabel: some View {
        if model.requiresCoreRestart {
            Label("重新连接", systemImage: "bolt.horizontal.circle")
        } else if model.isRefreshing(model.selectedFeature), !model.isRefreshingAll {
            ProgressView()
                .controlSize(.small)
                .frame(width: 16, height: 16)
                .accessibilityLabel(localization.format("正在%@", currentReloadTitle))
        } else {
            Label(currentReloadTitle, systemImage: "arrow.clockwise")
        }
    }

    private var currentReloadTitle: String {
        localization.format(
            "重新加载「%@」", model.selectedFeature.title(localization: localization)
        )
    }

    private var currentReloadHelp: String {
        if model.requiresCoreRestart { return localization.textValue("重新连接本地数据") }
        if model.isRefreshingAll { return localization.textValue("正在重新加载所有页面") }
        return model.isRefreshing(model.selectedFeature)
            ? localization.format("正在%@", currentReloadTitle)
            : currentReloadTitle
    }

    @ViewBuilder
    private var reloadOptionsLabel: some View {
        if model.isRefreshingAll {
            ProgressView()
                .controlSize(.small)
                .frame(width: 16, height: 16)
                .accessibilityLabel(localization.textValue("正在重新加载所有页面"))
        } else {
            Label("更多重新加载选项", systemImage: "ellipsis.circle")
        }
    }

    private var reloadOptionsHelp: String {
        model.isRefreshingAll
            ? localization.textValue("正在重新加载所有页面")
            : localization.textValue("更多重新加载选项")
    }

    @ViewBuilder
    private var featureContent: some View {
        Group {
            switch model.selectedFeature {
            case .overview:
                OverviewStateView(model: model) { model.navigate(to: $0) }
            case .sessions:
                RuntimeAwarePage(model: model) { SessionsView(model: model) }
            case .projects:
                RuntimeAwarePage(model: model) { ProjectsView(model: model) }
            case .quotaUsage:
                RuntimeAwarePage(model: model) { QuotaUsageView(model: model) }
            case .invocationUsage:
                RuntimeAwarePage(model: model) { InvocationUsageView(model: model) }
            case .apiSubscriptions:
                RuntimeAwarePage(model: model) { APISubscriptionsView(model: model) }
            case .localStatus:
                RuntimeAwarePage(model: model) { LocalStatusView(model: model) }
            case .sourcesJobs:
                RuntimeAwarePage(model: model) { SourcesJobsView(model: model) }
            case .settings:
                RuntimeAwarePage(model: model) {
                    SettingsView(model: model, loginItemSettings: loginItemSettings)
                }
            }
        }
        .id("\(model.selectedFeature.id):\(model.selectedProvider.rawValue)")
        .onAppear { model.markFeatureRendered(model.selectedFeature) }
    }
}

struct OverviewStateView: View {
    @ObservedObject var model: AppModel
    var onNavigate: (AppFeature) -> Void = { _ in }

    var body: some View {
        switch model.state {
        case .idle:
            loading("准备启动…")
        case .loading(let message):
            loading(message)
        case .overview(let overview):
            overviewContent(overview)
        case .partial(let overview):
            overviewContent(overview)
        case .stale(let overview, _):
            overviewContent(overview)
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
        case .unavailable(let notice):
            ContentUnavailableView {
                Label("本地数据暂时不可用", systemImage: "bolt.slash")
            } description: {
                Text(notice.retryable ? "可以重试连接。" : "当前版本无法读取这些数据，请更新 App。")
            } actions: {
                if notice.retryable { Button("重试") { model.restartCore() } }
            }
        case .cancelled:
            ContentUnavailableView("加载已取消", systemImage: "xmark.circle")
        case .shuttingDown:
            loading("正在安全退出…")
        case .stopped:
            ContentUnavailableView("本地数据服务已停止", systemImage: "stop.circle")
        }
    }

    @ViewBuilder
    private func overviewContent(_ overview: OverviewPresentation) -> some View {
        if model.selectedProvider == .cursor || model.selectedProvider == .grok {
            CursorOverviewContentView(
                provider: model.selectedProvider,
                overview: overview,
				selectedRange: model.overviewRange,
				onSelectRange: model.selectOverviewRange,
				onNavigate: { feature in
					if feature == .invocationUsage {
						model.navigateToInvocationUsageFromOverview()
					} else {
						onNavigate(feature)
					}
				},
				onSelectProject: { projectKey in
					model.projectOptions.range = model.overviewRange
					model.projectOptions.exactRange = overview.contentRange
					model.navigate(to: .projects)
					model.selectProject(projectKey)
				},
				onSelectSession: { sessionID in
					model.sessionOptions.range = model.overviewRange
					model.sessionOptions.exactRange = overview.contentRange
					model.navigate(to: .sessions)
					model.selectSession(sessionID)
				},
				localization: model.localization
            )
        } else {
            OverviewContentView(
            overview: overview,
            selectedRange: model.overviewRange,
            onSelectRange: model.selectOverviewRange,
            onNavigate: { feature in
                if feature == .invocationUsage {
                    model.navigateToInvocationUsageFromOverview()
                } else {
                    onNavigate(feature)
                }
            },
            onSelectProject: { projectKey in
                model.projectOptions.range = model.overviewRange
                model.projectOptions.exactRange = overview.contentRange
                model.navigate(to: .projects)
                model.selectProject(projectKey)
            },
            onSelectSession: { sessionID in
                model.sessionOptions.range = model.overviewRange
                model.sessionOptions.exactRange = overview.contentRange
                model.navigate(to: .sessions)
                model.selectSession(sessionID)
            },
                localization: model.localization
            )
        }
    }

    private func loading(_ text: String) -> some View {
        VStack(spacing: 12) {
            ProgressView()
                .controlSize(.large)
            Text(model.localization.textValue(text))
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityElement(children: .combine)
    }
}

private struct CursorOverviewContentView: View {
	let provider: AgentProvider
	let overview: OverviewPresentation
	let selectedRange: DateRangePreset
	let onSelectRange: (DateRangePreset) -> Void
	let onNavigate: (AppFeature) -> Void
	let onSelectProject: (String) -> Void
	let onSelectSession: (String) -> Void
	let localization: AppLocalization
	@State private var selectedTrendDate: Date?

	private var ranges: [DateRangePreset] {
		switch provider {
		case .grok: [.quotaWeek, .quotaMonth, .today, .sevenDays, .thirtyDays]
		case .cursor: [.quotaMonth, .today, .sevenDays, .thirtyDays]
		case .codex: [.quotaWeek, .today, .sevenDays, .thirtyDays]
		}
	}

	var body: some View {
		let summary = CursorOverviewSummaryPresentation(overview)
		ScrollView {
			VStack(alignment: .leading, spacing: 16) {
				pageHeader
				if officialPeriodFellBack {
					monthlyQuotaFallbackNotice
				}
				if summary.showsRecentActivityFallback || summary.usesLastKnownTodayData {
					activityNotice(summary)
				}
				cursorQuotaStatusStrip
				tokenActivitySection
				consumptionSection(summary)
				modelSection(summary, fillsProposedHeight: false)
				activityAndSessionsSection
				if provider.supportsInvocationStatistics {
					OverviewInvocationProfileCard(
						profile: overview.invocationProfile,
						rangeLabel: overview.usageRangeLabel,
						onNavigate: { onNavigate(.invocationUsage) },
						localization: localization,
						showsSkillActivity: false,
						showsAIEditActivity: provider == .cursor
					)
					.accessibilityIdentifier("cursor.overview.invocations")
				}
			}
			.padding(24)
			.frame(maxWidth: .infinity, alignment: .leading)
		}
		.accessibilityIdentifier(provider == .grok ? "page.overview.grok" : "page.overview.cursor")
	}

	private var officialPeriodFellBack: Bool {
		switch overview.requestedRange {
		case .quotaMonth: overview.effectiveRange != .quotaMonth
		case .quotaWeek: overview.effectiveRange != .quotaWeek || overview.fellBackFromQuotaWeek
		default: false
		}
	}

	private var monthlyQuotaFallbackNotice: some View {
		Label(
			provider == .grok
				? (overview.requestedRange == .quotaMonth
					? "暂未获取到月额度周期，当前显示最近 30 天。"
					: "暂未获取到额度周期，当前显示最近 7 天。")
				: "暂未获取到月额度周期，当前显示最近 30 天。",
			systemImage: "arrow.trianglehead.2.clockwise.rotate.90"
		)
		.font(.caption.weight(.medium))
		.foregroundStyle(.orange)
		.padding(.horizontal, 12)
		.padding(.vertical, 8)
		.background(Color.orange.opacity(0.1), in: RoundedRectangle(cornerRadius: 9))
	}

	private var pageHeader: some View {
		HStack(alignment: .center, spacing: 20) {
			VStack(alignment: .leading, spacing: 4) {
				Text(provider == .grok ? "Grok 概览" : "Cursor 概览").font(.largeTitle.bold())
				Text(provider == .grok ? "额度周期、近期趋势与工作归属" : "今日状态、近期趋势与工作归属").foregroundStyle(.secondary)
			}
			Spacer()
			VStack(alignment: .trailing, spacing: 6) {
				HStack(spacing: 8) {
					Text("分析范围").font(.subheadline).foregroundStyle(.secondary)
					Picker("分析范围", selection: rangeBinding) {
						ForEach(ranges) { range in
							Text(verbatim: localization.textValue(range.title)).tag(range)
						}
					}
					.labelsHidden()
					.pickerStyle(.segmented)
					.frame(width: 340)
				}
				if let dataAsOf = overview.dataAsOfMS {
					Label("数据截至 \(dateTimeText(dataAsOf))", systemImage: "clock")
						.font(.caption)
						.foregroundStyle(.secondary)
				}
			}
		}
	}

	private var rangeBinding: Binding<DateRangePreset> {
		Binding(
			get: { selectedRange },
			set: { value in onSelectRange(value) }
		)
	}

	private func activityNotice(_ summary: CursorOverviewSummaryPresentation) -> some View {
		let message: String
		if summary.usesLastKnownTodayData {
			message = summary.dataAsOfMS.map { "今日数据截至 \(dateTimeText($0))，会在下次同步后更新。" }
				?? "今日数据会在下次同步后更新。"
		} else if let latest = summary.recentActivityAtMS {
			message = "今日暂无新活动，最近一次活动在 \(dateTimeText(latest))。"
		} else {
			message = "今日暂无新活动。"
		}
		return Label(message, systemImage: summary.usesLastKnownTodayData ? "clock" : "moon.stars")
			.font(.caption.weight(.medium))
			.foregroundStyle(.secondary)
			.padding(.horizontal, 12)
			.padding(.vertical, 8)
			.background(Color.secondary.opacity(0.07), in: RoundedRectangle(cornerRadius: 9))
			.accessibilityIdentifier("cursor.overview.activity-notice")
	}

	private var cursorQuotaStatusStrip: some View {
		HStack(spacing: 18) {
			Label("额度状态", systemImage: "gauge.with.dots.needle.67percent")
				.font(.headline)
			if !overview.quotaAvailable || overview.quotaWindows.isEmpty {
				Text("暂时无法获取额度").foregroundStyle(.secondary)
			} else {
				ForEach(
					Array(
						OverviewQuotaWindowResolver.visibleWindows(overview.quotaWindows)
							.enumerated()
					),
					id: \.element.id
				) { index, window in
					let reset = QuotaResetPresentation(
						resetsAtMS: window.resetsAtMS,
						resetRemainingMS: window.resetRemainingMS
					)
					let progress = QuotaProgressPresentation(
						usedPercent: window.usedPercent,
						localization: localization
					)
					if index > 0 { Divider().frame(height: 64) }
					VStack(alignment: .leading, spacing: 5) {
						HStack(spacing: 8) {
							Text(window.title).font(.subheadline.weight(.medium))
							Spacer(minLength: 10)
							Text("已使用")
								.font(.caption)
								.foregroundStyle(.secondary)
							Text(progress.percentText)
								.font(.subheadline.bold())
								.monospacedDigit()
								.foregroundStyle(quotaLevelColor(progress.level))
						}
						ProgressView(value: progress.fraction)
							.tint(quotaLevelColor(progress.level))
							.accessibilityLabel(localization.textValue("已使用"))
							.accessibilityValue(progress.accessibilityValue)
						Text(reset.compactText)
							.font(.caption)
							.foregroundStyle(.secondary)
							.lineLimit(1)
						if let pace = overview.quotaPaceWindows.first(where: { $0.id == window.id }) {
							Text(pace.forecastText)
								.font(.caption.weight(.medium))
								.foregroundStyle(pace.forecastState == "at_risk" ? .orange : .secondary)
								.lineLimit(1)
						}
					}
					.frame(minWidth: 210, idealWidth: 250, maxWidth: 290)
				}
			}
			Spacer(minLength: 12)
			Button("额度详情") { onNavigate(.quotaUsage) }.buttonStyle(.link)
		}
		.padding(.horizontal, 16)
		.padding(.vertical, 12)
		.background(.thinMaterial, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
		.overlay {
			RoundedRectangle(cornerRadius: 12, style: .continuous)
				.stroke(.quaternary, lineWidth: 1)
		}
		.accessibilityIdentifier("cursor.overview.quota")
	}

	private func consumptionSection(_ summary: CursorOverviewSummaryPresentation) -> some View {
		SectionCard(title: "消耗概览") {
			HStack(alignment: .top, spacing: 22) {
				VStack(alignment: .leading, spacing: 18) {
					usageSummary(summary)
						.fixedSize(horizontal: false, vertical: true)
					Divider()
					trendChart
				}
				.frame(maxWidth: .infinity, alignment: .leading)
				Divider()
				projectBreakdown
					.frame(minWidth: 220, idealWidth: 270, maxWidth: 310)
			}
		}
		.accessibilityIdentifier("cursor.overview.range")
	}

	private var tokenActivitySection: some View {
		TokenActivityCard(
			card: TokenActivityCardPresentation(
				overview.tokenActivity,
				localization: localization,
				partialNotice: localization.textValue("仅展示当前已采集数据")
			),
			localization: localization
		)
		.accessibilityIdentifier("cursor.overview.token-activity")
	}

	private var activityAndSessionsSection: some View {
		ViewThatFits(in: .horizontal) {
			HStack(alignment: .top, spacing: 16) {
				activityDistributionCard(fillsProposedHeight: true)
					.frame(minWidth: 560)
					.frame(maxHeight: .infinity, alignment: .top)
				recentSessionsSection(fillsProposedHeight: true)
					.frame(minWidth: 320, idealWidth: 360, maxWidth: 420)
					.frame(maxHeight: .infinity, alignment: .top)
			}
			VStack(alignment: .leading, spacing: 16) {
				activityDistributionCard(fillsProposedHeight: false)
				recentSessionsSection(fillsProposedHeight: false)
			}
		}
	}

	private func activityDistributionCard(fillsProposedHeight: Bool) -> some View {
		OverviewActivityCard(
			activity: overview.activityDistribution,
			rangeLabel: overview.usageRangeLabel,
			fillsProposedHeight: fillsProposedHeight,
			localization: localization
		)
		.accessibilityIdentifier("cursor.overview.activity-distribution")
	}

	private func modelSection(
		_ summary: CursorOverviewSummaryPresentation,
		fillsProposedHeight: Bool
	) -> some View {
		SectionCard(title: "模型用量", fillsProposedHeight: fillsProposedHeight) {
			Text("\(localization.textValue(selectedRange.title)) · 按 Token 排序")
				.font(.caption)
				.foregroundStyle(.secondary)
			if summary.models.isEmpty {
				Text("当前范围没有模型用量。")
					.foregroundStyle(.secondary)
			} else {
				let maximum = summary.models.compactMap { metricValue($0.tokens.total) }.max() ?? 0
				ForEach(Array(summary.models.prefix(5).enumerated()), id: \.element.id) { index, item in
					VStack(alignment: .leading, spacing: 4) {
						HStack {
							Text("\(index + 1)").font(.caption.bold()).foregroundStyle(.secondary)
							Text(item.title).font(.subheadline.weight(.medium)).lineLimit(1)
							Spacer()
							Text(item.tokens.total.formatted).monospacedDigit().foregroundStyle(.tint)
						}
						HStack(spacing: 8) {
							ProgressView(value: modelFraction(item, maximum: maximum))
							Text("\(item.requestCount.formatted) 次")
								.font(.caption2)
								.foregroundStyle(.secondary)
						}
					}
					if item.id != summary.models.prefix(5).last?.id { Divider() }
				}
			}
		}
		.accessibilityIdentifier("cursor.overview.models")
	}

	private func recentSessionsSection(fillsProposedHeight: Bool) -> some View {
		SectionCard(title: "最近会话", fillsProposedHeight: fillsProposedHeight) {
			Text("\(localization.textValue(selectedRange.title)) · 按最近活动排序")
				.font(.caption)
				.foregroundStyle(.secondary)
			if !overview.sessionsAvailable {
				Text("会话数据暂时不可用。").foregroundStyle(.secondary)
			} else if overview.sessions.isEmpty {
				Text("当前范围没有会话。可切换到更长时间范围。")
					.foregroundStyle(.secondary)
			} else {
				ForEach(Array(overview.sessions.enumerated()), id: \.element.id) { index, session in
					Button { onSelectSession(session.id) } label: {
						HStack(spacing: 10) {
							VStack(alignment: .leading, spacing: 3) {
								Text(session.title).lineLimit(1)
								Text(sessionSecondaryText(session))
									.font(.caption).foregroundStyle(.secondary).lineLimit(1)
							}
							Spacer()
							if session.tokens.isKnown {
								Text(session.tokens.formatted).monospacedDigit().foregroundStyle(.tint)
							}
							Image(systemName: "chevron.right").font(.caption).foregroundStyle(.tertiary)
						}
						.contentShape(Rectangle())
					}
					.buttonStyle(.plain)
					.padding(.vertical, 6)
					if index < overview.sessions.count - 1 { Divider() }
				}
				Button("查看全部会话") { onNavigate(.sessions) }.buttonStyle(.link)
			}
		}
		.accessibilityIdentifier("cursor.overview.sessions")
	}

	private func usageSummary(_ summary: CursorOverviewSummaryPresentation) -> some View {
		HStack(alignment: .top, spacing: 24) {
			VStack(alignment: .leading, spacing: 10) {
				Text("Token 总量")
					.font(.subheadline.weight(.medium))
					.foregroundStyle(.secondary)
				Text(summary.rangeTotalTokens.formatted)
					.font(.system(size: 25, weight: .semibold, design: .rounded))
					.monospacedDigit()
				HStack(alignment: .top, spacing: 22) {
					usageBreakdownMetric(
						title: "输入",
						value: overview.tokenBreakdown.input,
						detailTitle: "缓存",
						detailValue: overview.tokenBreakdown.cachedInput
					)
					usageBreakdownMetric(
						title: "输出",
						value: overview.tokenBreakdown.output,
						detailTitle: nil,
						detailValue: nil
					)
				}
			}
			.frame(maxWidth: .infinity, alignment: .leading)

			Divider()

			VStack(alignment: .leading, spacing: 5) {
				Text(summary.rangeCostBasis == .reported
					? (provider == .grok ? "Grok 上报费用" : "Cursor 上报费用")
					: (provider == .grok ? "xAI 参考价估算" : "文档价目估算"))
					.font(.subheadline.weight(.medium))
					.foregroundStyle(.secondary)
				Text(metricText(summary.rangePrimaryCost, cost: true))
					.font(.system(size: 25, weight: .semibold, design: .rounded))
					.monospacedDigit()
				Text(summary.rangeCostBasis == .reported ? "Dashboard 实际上报" : "仅在可定价模型上计算")
					.font(.caption2)
					.foregroundStyle(.tertiary)
			}
			.frame(minWidth: 180, idealWidth: 210, maxWidth: 240, alignment: .leading)
		}
	}

	private func usageBreakdownMetric(
		title: String,
		value: DisplayMetric,
		detailTitle: String?,
		detailValue: DisplayMetric?
	) -> some View {
		VStack(alignment: .leading, spacing: 3) {
			Text(title).font(.caption).foregroundStyle(.secondary)
			Text(value.formatted).font(.headline).monospacedDigit()
			if let detailTitle, let detailValue {
				Text("\(detailTitle) \(detailValue.formatted)")
					.font(.caption2)
					.foregroundStyle(.tertiary)
			}
		}
		.frame(minWidth: 108, alignment: .leading)
		.accessibilityElement(children: .combine)
	}

	@ViewBuilder
	private var trendChart: some View {
		VStack(alignment: .leading, spacing: 10) {
			HStack(alignment: .firstTextBaseline) {
				Text("Token 趋势").font(.subheadline.weight(.semibold))
				Text(overview.usageRangeLabel).font(.caption).foregroundStyle(.secondary)
			}
			if trendPoints.isEmpty {
				ContentUnavailableView(
					"当前范围暂无用量",
					systemImage: "chart.xyaxis.line",
					description: Text("可切换到更长时间范围查看最近数据。")
				)
				.frame(height: 180)
			} else {
				Chart {
					ForEach(trendPoints) { point in
						AreaMark(x: .value("时间", point.date), y: .value("Token", point.tokens))
							.interpolationMethod(.monotone)
							.foregroundStyle(LinearGradient(
								colors: [Color.blue.opacity(0.28), Color.blue.opacity(0.03)],
								startPoint: .top, endPoint: .bottom))
						LineMark(x: .value("时间", point.date), y: .value("Token", point.tokens))
							.interpolationMethod(.monotone)
							.foregroundStyle(.blue)
							.lineStyle(StrokeStyle(lineWidth: 2.2, lineCap: .round, lineJoin: .round))
						PointMark(x: .value("时间", point.date), y: .value("Token", point.tokens))
							.foregroundStyle(.blue)
							.symbolSize(selectedTrendPoint?.id == point.id ? 70 : 28)
					}
					if let selected = selectedTrendPoint {
						RuleMark(x: .value("选中时间", selected.date))
							.foregroundStyle(Color.secondary.opacity(0.55))
							.lineStyle(StrokeStyle(lineWidth: 1, dash: [4, 4]))
							.annotation(
								position: .top,
								alignment: .leading,
								spacing: 8,
								overflowResolution: .init(x: .fit(to: .chart), y: .disabled)
							) {
								VStack(alignment: .leading, spacing: 3) {
									Text(dateTimeText(Int64(selected.date.timeIntervalSince1970 * 1_000)))
										.font(.caption).foregroundStyle(.secondary)
									Text(TokenQuantityFormatter.stringWithUnit(selected.tokens))
										.font(.caption.bold()).monospacedDigit()
								}
								.padding(8)
								.background(.regularMaterial, in: RoundedRectangle(cornerRadius: 8))
							}
					}
				}
				.chartYAxis {
					AxisMarks(position: .leading) { value in
						AxisGridLine().foregroundStyle(.quaternary)
						AxisValueLabel {
							if let value = value.as(Int64.self) {
								Text(TokenQuantityFormatter.string(value))
							}
						}
					}
				}
				.chartXAxis { AxisMarks(values: .automatic(desiredCount: 6)) }
				.chartXSelection(value: $selectedTrendDate)
				.frame(height: trendPoints.count <= 1 ? 170 : 230)
				.onChange(of: selectedRange) { _, _ in selectedTrendDate = nil }
				.accessibilityIdentifier("cursor.overview.trend")
			}
		}
	}

	private var projectBreakdown: some View {
		VStack(alignment: .leading, spacing: 12) {
			HStack {
				Text("项目归属").font(.subheadline.weight(.semibold))
				Spacer()
				Button("全部项目") { onNavigate(.projects) }.buttonStyle(.link)
			}
			if !overview.projectsAvailable {
				Text("项目数据暂时不可用。").foregroundStyle(.secondary)
			} else if overview.projects.isEmpty {
				Text("当前范围没有项目活动。").foregroundStyle(.secondary)
			} else {
				ForEach(Array(overview.projects.enumerated()), id: \.element.id) { index, project in
					if project.isOther {
						projectBreakdownRow(project, rank: index + 1)
					} else {
						Button { onSelectProject(project.id) } label: {
							projectBreakdownRow(project, rank: index + 1)
						}
						.buttonStyle(.plain)
					}
				}
			}
		}
		.accessibilityIdentifier("cursor.overview.projects")
	}

	private func projectBreakdownRow(_ project: ProjectPresentation, rank: Int) -> some View {
		VStack(alignment: .leading, spacing: 5) {
			HStack(spacing: 8) {
				Text("\(rank)").font(.caption.bold()).foregroundStyle(.secondary).frame(width: 18)
				Text(project.title).lineLimit(1)
				Spacer()
				Text(project.tokens.isKnown ? project.tokens.formatted : "\(project.sessionCount.formatted) 会话")
					.font(.subheadline.weight(.semibold))
					.monospacedDigit()
					.foregroundStyle(.tint)
			}
			ProgressView(value: projectFraction(project))
		}
		.padding(.vertical, 4)
		.contentShape(Rectangle())
	}

	private var trendPoints: [CursorOverviewTrendPoint] {
		overview.trend.compactMap { point in
			guard let at = point.startAtMS, let tokens = metricValue(point.tokens) else { return nil }
			return CursorOverviewTrendPoint(
				id: point.id,
				date: Date(timeIntervalSince1970: Double(at) / 1_000),
				tokens: tokens
			)
		}
	}

	private var selectedTrendPoint: CursorOverviewTrendPoint? {
		guard let selected = TrendSelectionResolver.nearest(
			to: selectedTrendDate,
			in: overview.trend
		),
		let at = selected.startAtMS,
		let tokens = metricValue(selected.tokens)
		else { return nil }
		return CursorOverviewTrendPoint(
			id: selected.id,
			date: Date(timeIntervalSince1970: Double(at) / 1_000),
			tokens: tokens
		)
	}

	private func modelFraction(_ item: OverviewUsageModelPresentation, maximum: Int64) -> Double {
		guard let value = metricValue(item.tokens.total), maximum > 0 else { return 0 }
		return min(max(Double(value) / Double(maximum), 0), 1)
	}

	private func projectFraction(_ project: ProjectPresentation) -> Double {
		if let value = metricValue(project.tokens) {
			let maximum = overview.projects.compactMap { metricValue($0.tokens) }.max() ?? 0
			guard maximum > 0 else { return 0 }
			return min(max(Double(value) / Double(maximum), 0), 1)
		}
		guard let sessions = metricValue(project.sessionCount) else { return 0 }
		let maximum = overview.projects.compactMap { metricValue($0.sessionCount) }.max() ?? 0
		guard maximum > 0 else { return 0 }
		return min(max(Double(sessions) / Double(maximum), 0), 1)
	}

	private func sessionSecondaryText(_ session: SessionPresentation) -> String {
		let pieces = [
			session.project,
			session.lastActivityAtMS.map(dateTimeText),
		].compactMap { $0 }
		return pieces.isEmpty ? session.activity : pieces.joined(separator: " · ")
	}

	private func dateMetricText(_ metric: DisplayMetric) -> String {
		guard let value = metricValue(metric) else { return "--" }
		return dateTimeText(value)
	}

	private func dateTimeText(_ milliseconds: Int64) -> String {
		let formatter = DateFormatter()
		formatter.locale = localization.locale
		formatter.timeZone = TimeZone(identifier: overview.usageRange.timeZone) ?? .current
		formatter.dateFormat = "M-d HH:mm"
		return formatter.string(from: Date(timeIntervalSince1970: Double(milliseconds) / 1_000))
	}
}

private struct CursorOverviewTrendPoint: Identifiable {
	let id: String
	let date: Date
	let tokens: Int64
}

private func cursorDataAsOfText(_ milliseconds: Int64?, timeZone: String) -> String? {
	guard let milliseconds else { return nil }
	let formatter = DateFormatter()
	formatter.locale = AppLocalizationRegistry.shared.current.locale
	formatter.timeZone = TimeZone(identifier: timeZone) ?? .current
	formatter.dateFormat = "M-d HH:mm"
	return formatter.string(from: Date(timeIntervalSince1970: Double(milliseconds) / 1_000))
}

private struct OverviewContentView: View {
    let overview: OverviewPresentation
    let selectedRange: DateRangePreset
    let onSelectRange: (DateRangePreset) -> Void
    let onNavigate: (AppFeature) -> Void
    let onSelectProject: (String) -> Void
    let onSelectSession: (String) -> Void
    let localization: AppLocalization
    @State private var selectedTrendDate: Date?

    private let ranges: [DateRangePreset] = [.quotaWeek, .today, .sevenDays, .thirtyDays]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                pageHeader
                if overview.fellBackFromQuotaWeek { fallbackNotice }
                quotaStatusStrip
                tokenActivitySection
                consumptionSection
                activityAndSessionsSection
                invocationProfileSection
            }
            .padding(24)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .accessibilityIdentifier("page.overview")
    }

    private var pageHeader: some View {
        HStack(alignment: .center, spacing: 20) {
            VStack(alignment: .leading, spacing: 4) {
                Text("额度概览")
                    .font(.largeTitle.bold())
                Text("看清消耗了多少，以及消耗去了哪里")
                    .foregroundStyle(.secondary)
            }
            Spacer()
            VStack(alignment: .trailing, spacing: 6) {
                HStack(spacing: 8) {
                    Text("概览范围")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                    Picker("概览范围", selection: rangeBinding) {
                        ForEach(ranges) { range in
                            Text(verbatim: localization.textValue(range.title)).tag(range)
                        }
                    }
                    .labelsHidden()
                    .pickerStyle(.segmented)
                    .frame(width: 340)
                }
                .fixedSize(horizontal: false, vertical: true)
                Label("更新于 \(timestampText(overview.evaluatedAtMS))", systemImage: "clock")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .fixedSize(horizontal: false, vertical: true)
        }
    }

    private var rangeBinding: Binding<DateRangePreset> {
        Binding(get: { selectedRange }, set: { value in onSelectRange(value) })
    }

    private var fallbackNotice: some View {
        Label(
            "暂未获取到周额度周期，当前显示最近 7 天。",
            systemImage: "arrow.trianglehead.2.clockwise.rotate.90"
        )
        .font(.caption.weight(.medium))
        .foregroundStyle(.orange)
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(Color.orange.opacity(0.1), in: RoundedRectangle(cornerRadius: 9))
    }

    private var quotaStatusStrip: some View {
        HStack(spacing: 18) {
            Label("额度状态", systemImage: "gauge.with.dots.needle.67percent")
                .font(.headline)
            if !overview.quotaAvailable || overview.quotaWindows.isEmpty {
                Text("暂时无法获取额度")
                    .foregroundStyle(.secondary)
            } else {
                ForEach(
                    Array(
                        OverviewQuotaWindowResolver.visibleWindows(overview.quotaWindows)
                            .enumerated()
                    ),
                    id: \.element.id
                ) { index, window in
                    let reset = QuotaResetPresentation(
                        resetsAtMS: window.resetsAtMS,
                        resetRemainingMS: window.resetRemainingMS
                    )
                    let progress = QuotaProgressPresentation(
                        remainingPercent: window.remainingPercent,
                        localization: localization
                    )
                    if index > 0 { Divider().frame(height: 48) }
                    VStack(alignment: .leading, spacing: 4) {
                        HStack(spacing: 8) {
                            Text(window.title).font(.subheadline.weight(.medium))
                            Text(progress.percentText)
                                .font(.subheadline.bold())
                                .monospacedDigit()
                                .foregroundStyle(quotaLevelColor(progress.level))
                        }
                        ProgressView(value: progress.fraction)
                            .tint(quotaLevelColor(progress.level))
                            .frame(width: 132)
                            .accessibilityLabel(localization.textValue("剩余"))
                            .accessibilityValue(progress.accessibilityValue)
                        Text(reset.compactText)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                            .minimumScaleFactor(0.8)
                            .accessibilityIdentifier("overview.quota.reset.\(window.id)")
                            .accessibilityLabel("额度重置")
                            .accessibilityValue(reset.compactText)
                        if let pace = overview.quotaPaceWindows.first(where: { $0.id == window.id }) {
                            Text(pace.forecastText)
                                .font(.caption.weight(.medium))
                                .foregroundStyle(pace.forecastState == "at_risk" ? .orange : .secondary)
                                .lineLimit(1)
                                .minimumScaleFactor(0.75)
                                .accessibilityIdentifier("overview.quota.pace.\(window.id)")
                        }
                    }
                }
            }
            Spacer(minLength: 12)
            Button("额度详情") { onNavigate(.quotaUsage) }.buttonStyle(.link)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .stroke(.quaternary, lineWidth: 1)
        }
    }

    private var consumptionSection: some View {
        SectionCard(title: "消耗概览") {
            HStack(alignment: .top, spacing: 22) {
                VStack(alignment: .leading, spacing: 18) {
                    usageSummary
                        .fixedSize(horizontal: false, vertical: true)
                    Divider()
                    trendChart
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                Divider()
                projectBreakdown
                    .frame(minWidth: 220, idealWidth: 270, maxWidth: 310)
            }
        }
    }

    private var tokenActivitySection: some View {
        TokenActivityCard(
            card: TokenActivityCardPresentation(
                overview.tokenActivity,
                localization: localization
            ),
            localization: localization
        )
    }

    private var usageSummary: some View {
        HStack(alignment: .top, spacing: 28) {
            VStack(alignment: .leading, spacing: 12) {
                VStack(alignment: .leading, spacing: 5) {
                    Text("Token 总量")
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(.secondary)
                    Text(metricText(overview.tokenBreakdown.total, localization: localization))
                        .font(.system(size: 25, weight: .semibold, design: .rounded))
                        .monospacedDigit()
                }
                HStack(alignment: .top, spacing: 28) {
                    usageBreakdownMetric(
                        title: "输入",
                        value: overview.tokenBreakdown.input,
                        detailTitle: "缓存",
                        detailValue: overview.tokenBreakdown.cachedInput
                    )
                    usageBreakdownMetric(
                        title: "输出",
                        value: overview.tokenBreakdown.output,
                        detailTitle: "推理",
                        detailValue: overview.tokenBreakdown.reasoning
                    )
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)

            Divider()

            VStack(alignment: .leading, spacing: 5) {
                Text("API 等价成本")
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(.secondary)
                Text(metricText(
                    overview.estimatedCost,
                    cost: true,
                    localization: localization
                ))
                    .font(.system(size: 25, weight: .semibold, design: .rounded))
                    .monospacedDigit()
            }
            .frame(minWidth: 180, idealWidth: 220, maxWidth: 260, alignment: .leading)
        }
    }

    private func usageBreakdownMetric(
        title: String,
        value: DisplayMetric,
        detailTitle: String,
        detailValue: DisplayMetric
    ) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(localization.textValue(title))
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(metricText(value, localization: localization))
                .font(.headline)
                .monospacedDigit()
            Text(
                localization.textValue(detailTitle) + " "
                    + metricText(detailValue, localization: localization)
            )
                .font(.caption2)
                .foregroundStyle(.tertiary)
        }
        .frame(minWidth: 128, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    @ViewBuilder
    private var trendChart: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline) {
                Text("Token 趋势").font(.subheadline.weight(.semibold))
                Text(overview.usageRangeLabel).font(.caption).foregroundStyle(.secondary)
            }
            if !overview.usageAvailable {
                ContentUnavailableView(
                    "Token 用量暂时不可用",
                    systemImage: "chart.xyaxis.line",
                    description: Text("额度与其他区域仍可继续查看。")
                )
                .frame(height: 220)
            } else if chartPoints.isEmpty {
                ContentUnavailableView(
                    "当前范围暂无用量",
                    systemImage: "chart.xyaxis.line",
                    description: Text("产生新的会话后，趋势会显示在这里。")
                )
                .frame(height: 220)
            } else {
                Chart {
                    ForEach(chartPoints) { point in
                        AreaMark(
                            x: .value("时间", point.date),
                            y: .value("Token", point.tokens)
                        )
                        .interpolationMethod(.monotone)
                        .foregroundStyle(
                            LinearGradient(
                                colors: [Color.blue.opacity(0.30), Color.blue.opacity(0.03)],
                                startPoint: .top,
                                endPoint: .bottom
                            )
                        )
                        LineMark(
                            x: .value("时间", point.date),
                            y: .value("Token", point.tokens)
                        )
                        .interpolationMethod(.monotone)
                        .foregroundStyle(Color.blue)
                        .lineStyle(
                            StrokeStyle(lineWidth: 2.2, lineCap: .round, lineJoin: .round))
                        PointMark(
                            x: .value("时间", point.date),
                            y: .value("Token", point.tokens)
                        )
                        .foregroundStyle(Color.blue)
                        .symbolSize(selectedTrendPoint?.id == point.id ? 70 : 28)
                        .accessibilityLabel(trendPointTimeText(point.date))
                        .accessibilityValue(
                            TokenQuantityFormatter.stringWithUnit(point.tokens)
                        )
                    }
                    if let selected = selectedTrendPoint {
                        RuleMark(x: .value("选中时间", selected.date))
                            .foregroundStyle(Color.secondary.opacity(0.55))
                            .lineStyle(StrokeStyle(lineWidth: 1, dash: [4, 4]))
                            .annotation(
                                position: .top,
                                alignment: .leading,
                                spacing: 8,
                                overflowResolution: .init(x: .fit(to: .chart), y: .disabled)
                            ) {
                                selectedTrendDetail(selected)
                            }
                    }
                }
                .chartYAxis {
                    AxisMarks(position: .leading) { value in
                        AxisGridLine().foregroundStyle(.quaternary)
                        AxisValueLabel {
                            if let value = value.as(Int64.self) {
                                Text(TokenQuantityFormatter.string(value))
                            }
                        }
                    }
                }
                .chartXAxis {
                    if selectedRange == .quotaWeek {
                        AxisMarks(values: .stride(by: .day)) { value in
                            AxisGridLine().foregroundStyle(.quaternary)
                            AxisTick()
                            AxisValueLabel {
                                if let date = value.as(Date.self) {
                                    Text(trendPointDateText(date))
                                }
                            }
                        }
                    } else {
                        AxisMarks(values: .automatic(desiredCount: 6))
                    }
                }
                .chartXSelection(value: $selectedTrendDate)
                .frame(height: 230)
                .onChange(of: selectedRange) { _, _ in selectedTrendDate = nil }
            }
        }
    }

    private var projectBreakdown: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("项目消耗").font(.subheadline.weight(.semibold))
                Spacer()
                Button("全部项目") { onNavigate(.projects) }.buttonStyle(.link)
            }
            if !overview.projectsAvailable {
                Text("项目消耗暂时不可用。")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .padding(.vertical, 8)
            } else if overview.projects.isEmpty {
                Text("当前范围暂无项目消耗。")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .padding(.vertical, 8)
            } else {
                ForEach(Array(overview.projects.enumerated()), id: \.element.id) { index, project in
                    projectBreakdownRow(project, rank: index + 1)
                }
            }
        }
    }

    @ViewBuilder
    private func projectBreakdownRow(_ project: ProjectPresentation, rank: Int) -> some View {
        if project.isOther {
            ProjectUsageRow(
                rank: rank,
                title: project.title,
                tokens: project.tokenBreakdown,
                fraction: projectFraction(project.tokens),
                localization: localization
            )
        } else {
            Button { onSelectProject(project.id) } label: {
                ProjectUsageRow(
                    rank: rank,
                    title: project.title,
                    tokens: project.tokenBreakdown,
                    fraction: projectFraction(project.tokens),
                    localization: localization
                )
            }
            .buttonStyle(.plain)
        }
    }

    private var activityAndSessionsSection: some View {
        ViewThatFits(in: .horizontal) {
            HStack(alignment: .top, spacing: 16) {
                activityDistributionCard(fillsProposedHeight: true)
                    .frame(minWidth: 560)
                    .frame(maxHeight: .infinity, alignment: .top)
                highConsumptionSessions(fillsProposedHeight: true)
                    .frame(minWidth: 320, idealWidth: 360, maxWidth: 420)
                    .frame(maxHeight: .infinity, alignment: .top)
            }
            VStack(alignment: .leading, spacing: 16) {
                activityDistributionCard(fillsProposedHeight: false)
                highConsumptionSessions(fillsProposedHeight: false)
            }
        }
    }

    private var invocationProfileSection: some View {
        OverviewInvocationProfileCard(
            profile: overview.invocationProfile,
            rangeLabel: overview.usageRangeLabel,
            onNavigate: { onNavigate(.invocationUsage) },
            localization: localization
        )
    }

    private func activityDistributionCard(fillsProposedHeight: Bool) -> some View {
        OverviewActivityCard(
            activity: overview.activityDistribution,
            rangeLabel: overview.usageRangeLabel,
            fillsProposedHeight: fillsProposedHeight,
            localization: localization
        )
    }

    private func highConsumptionSessions(fillsProposedHeight: Bool) -> some View {
        SectionCard(
            title: "高消耗会话",
            fillsProposedHeight: fillsProposedHeight
        ) {
            Text("当前范围 · 按 Token 总量排序")
                .font(.caption)
                .foregroundStyle(.secondary)
            if !overview.sessionsAvailable {
                Text("会话消耗暂时不可用。").foregroundStyle(.secondary)
            } else if overview.sessions.isEmpty {
                Text("当前范围暂无会话消耗。").foregroundStyle(.secondary)
            } else {
                VStack(spacing: 0) {
                    ForEach(
                        Array(overview.sessions.enumerated()),
                        id: \.element.id
                    ) { index, session in
                        Button { onSelectSession(session.id) } label: {
                            HStack(spacing: 12) {
                                Text("\(index + 1)")
                                    .font(.caption.bold())
                                    .foregroundStyle(.secondary)
                                    .frame(width: 20)
                                VStack(alignment: .leading, spacing: 3) {
                                    Text(session.title).lineLimit(1)
                                    if let project = session.project {
                                        Text(project)
                                            .font(.caption)
                                            .foregroundStyle(.secondary)
                                            .lineLimit(1)
                                    }
                                }
                                Spacer()
                                Text(sessionTokenText(session.tokens))
                                    .font(.subheadline.weight(.semibold))
                                    .monospacedDigit()
                                    .foregroundStyle(.tint)
                                Image(systemName: "chevron.right")
                                    .font(.caption)
                                    .foregroundStyle(.tertiary)
                            }
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .padding(.vertical, 8)
                        .frame(
                            maxWidth: .infinity,
                            maxHeight: fillsProposedHeight ? .infinity : nil,
                            alignment: .leading
                        )
                        if index < overview.sessions.count - 1 { Divider() }
                    }
                }
                .frame(
                    maxHeight: fillsProposedHeight ? .infinity : nil,
                    alignment: .top
                )
                Button("查看全部会话") { onNavigate(.sessions) }.buttonStyle(.link)
            }
        }
    }

    private func sessionTokenText(_ metric: DisplayMetric) -> String {
        guard case .known(let value, _) = metric else { return "--" }
        return TokenQuantityFormatter.stringWithUnit(
            value, compact: true, localization: localization
        )
    }

    private var chartPoints: [OverviewChartPoint] {
        overview.trend.compactMap { point in
            guard let startAtMS = point.startAtMS,
                  let tokens = metricValue(point.tokens)
            else { return nil }
            return OverviewChartPoint(
                id: point.id,
                date: Date(timeIntervalSince1970: Double(startAtMS) / 1_000),
                tokens: tokens,
                tokenBreakdown: point.tokenBreakdown,
                estimatedCost: point.estimatedCost)
        }
    }

    private var selectedTrendPoint: OverviewChartPoint? {
        guard let selected = TrendSelectionResolver.nearest(
            to: selectedTrendDate,
            in: overview.trend
        ),
            let startAtMS = selected.startAtMS,
            let tokens = metricValue(selected.tokens)
        else { return nil }
        return OverviewChartPoint(
            id: selected.id,
            date: Date(timeIntervalSince1970: Double(startAtMS) / 1_000),
            tokens: tokens,
            tokenBreakdown: selected.tokenBreakdown,
            estimatedCost: selected.estimatedCost
        )
    }

    private func selectedTrendDetail(_ point: OverviewChartPoint) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(trendPointTimeText(point.date))
                .font(.caption)
                .foregroundStyle(.secondary)
            TokenBreakdownView(
                tokens: point.tokenBreakdown,
                style: .compact,
                localization: localization
            )
            if case .known = point.estimatedCost {
                Text(localization.format(
                    "API 等价成本 %@",
                    metricText(
                        point.estimatedCost,
                        cost: true,
                        localization: localization
                    )
                ))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 8)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .stroke(.quaternary, lineWidth: 1)
        }
    }

    private func trendPointTimeText(_ date: Date) -> String {
        if selectedRange == .today {
            return date.formatted(
                .dateTime.month().day().hour().minute().locale(localization.locale)
            )
        }
        return date.formatted(.dateTime.year().month().day().locale(localization.locale))
    }

    private func trendPointDateText(_ date: Date) -> String {
        date.formatted(.dateTime.month().day().locale(localization.locale))
    }

    private var projectTokenTotal: Int64? {
        let metrics = overview.projects.map(\.tokens) + [overview.otherProjectTokens].compactMap { $0 }
        var total: Int64 = 0
        for metric in metrics {
            guard let value = metricValue(metric) else { return nil }
            let (next, overflow) = total.addingReportingOverflow(value)
            guard !overflow else { return nil }
            total = next
        }
        return total > 0 ? total : nil
    }

    private func projectFraction(_ metric: DisplayMetric) -> Double? {
        guard let total = projectTokenTotal, let value = metricValue(metric) else { return nil }
        return min(max(Double(value) / Double(total), 0), 1)
    }

}

private struct OverviewInvocationProfileCard: View {
    let profile: OverviewInvocationProfilePresentation
    let rangeLabel: String
    let onNavigate: () -> Void
    let localization: AppLocalization
	let showsSkillActivity: Bool
	let showsAIEditActivity: Bool

	init(
		profile: OverviewInvocationProfilePresentation,
		rangeLabel: String,
		onNavigate: @escaping () -> Void,
		localization: AppLocalization,
		showsSkillActivity: Bool = true,
		showsAIEditActivity: Bool = false
	) {
		self.profile = profile
		self.rangeLabel = rangeLabel
		self.onNavigate = onNavigate
		self.localization = localization
		self.showsSkillActivity = showsSkillActivity
		self.showsAIEditActivity = showsAIEditActivity
	}

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .firstTextBaseline, spacing: 16) {
                Text(localization.textValue("调用画像"))
                    .font(.headline)
                Spacer()
                Button(localization.textValue("查看完整调用统计"), action: onNavigate)
                    .buttonStyle(.link)
            }
            Text(localization.format(
                "%@ · %@",
                rangeLabel,
                localization.textValue("全部来源")
            ))
                .font(.caption)
                .foregroundStyle(.secondary)

            invocationContent
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .padding(16)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .stroke(.quaternary, lineWidth: 1)
        }
        .accessibilityIdentifier("overview.invocation-profile")
    }

    @ViewBuilder
    private var invocationContent: some View {
        switch profile.availability {
        case .unavailable:
            ContentUnavailableView(
                localization.textValue("调用统计暂时不可用"),
                systemImage: "wrench.and.screwdriver"
            )
            .frame(height: 170)
        case .available, .partial:
            if profile.isEmpty && !hasAIEditActivity {
                ContentUnavailableView(
                    localization.textValue("当前范围没有调用活动"),
                    systemImage: "wrench.and.screwdriver"
                )
                .frame(height: 170)
            } else {
                if profile.availability == .partial {
                    Label(
                        localization.textValue("调用数据仍在整理"),
                        systemImage: "circle.dotted"
                    )
                    .font(.caption.weight(.medium))
                    .foregroundStyle(.orange)
                }
                summaryGrid
                rankings
            }
        }
    }

    private var summaryGrid: some View {
        ViewThatFits(in: .horizontal) {
            LazyVGrid(
                columns: Array(repeating: GridItem(.flexible(), spacing: 12), count: 4),
                alignment: .leading,
                spacing: 12
            ) {
                summaryMetrics
            }
            .frame(minWidth: 616)

            LazyVGrid(
                columns: [GridItem(.adaptive(minimum: 145), spacing: 12)],
                alignment: .leading,
                spacing: 12
            ) {
                summaryMetrics
            }
        }
    }

    @ViewBuilder
    private var summaryMetrics: some View {
        OverviewInvocationSummaryMetric(
            title: localization.textValue("Tool 调用"),
            value: metricText(profile.toolCallCount, localization: localization),
            detail: localization.format(
                "%@ 种 Tool",
                metricText(profile.distinctToolCount, localization: localization)
            ),
            symbol: "wrench.adjustable",
            tint: .blue
        )
		if showsSkillActivity {
			OverviewInvocationSummaryMetric(
				title: localization.textValue("Skill 检测活动"),
				value: metricText(profile.skillActivityCount, localization: localization),
				detail: localization.format(
					"%@ 种 Skill",
					metricText(profile.distinctSkillCount, localization: localization)
				),
				symbol: "puzzlepiece.extension",
				tint: .purple
			)
		} else if showsAIEditActivity {
			OverviewInvocationSummaryMetric(
				title: "AI edits",
				value: metricText(profile.aiEditCount, localization: localization),
				detail: localization.textValue("当前概览范围"),
				symbol: "pencil.and.list.clipboard",
				tint: .purple
			)
		}
        OverviewInvocationSummaryMetric(
            title: localization.textValue("相关会话"),
            value: metricText(profile.sessionCount, localization: localization),
            detail: localization.textValue("当前概览范围"),
            symbol: "text.bubble",
            tint: .teal
        )
        OverviewInvocationSummaryMetric(
            title: localization.textValue("明确失败"),
            value: metricText(profile.toolFailureCount, localization: localization),
            detail: localization.textValue("仅带结果的 Tool 事件"),
            symbol: "exclamationmark.triangle",
            tint: failureTint
        )
    }

    @ViewBuilder
    private var rankings: some View {
		if showsSkillActivity {
			ViewThatFits(in: .horizontal) {
				HStack(alignment: .top, spacing: 20) {
					toolRanking.frame(maxWidth: .infinity, alignment: .topLeading)
					Divider()
					skillRanking.frame(maxWidth: .infinity, alignment: .topLeading)
				}
				.frame(minWidth: 620)

				VStack(alignment: .leading, spacing: 16) {
					toolRanking
					Divider()
					skillRanking
				}
			}
		} else {
			toolRanking
		}
    }

	private var hasAIEditActivity: Bool {
		guard showsAIEditActivity, let count = knownValue(profile.aiEditCount) else { return false }
		return count > 0
	}

    private var toolRanking: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(localization.textValue("常用 Tool"))
                .font(.subheadline.weight(.semibold))
            if profile.tools.isEmpty {
                Text(localization.textValue("当前范围没有 Tool 调用"))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .padding(.vertical, 8)
            } else {
                let maximum = profile.tools.compactMap { knownValue($0.callCount) }.max() ?? 0
                ForEach(profile.tools) { item in
                    OverviewInvocationRankingRow(
                        name: item.name,
                        value: metricText(item.callCount, localization: localization),
                        count: knownValue(item.callCount),
                        maximum: maximum,
                        tint: .blue,
                        help: toolHelp(item)
                    )
                }
            }
        }
    }

    private var skillRanking: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(localization.textValue("常用 Skill"))
                .font(.subheadline.weight(.semibold))
            if profile.skills.isEmpty {
                Text(localization.textValue("当前范围没有 Skill 活动"))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .padding(.vertical, 8)
            } else {
                let maximum = profile.skills.compactMap { knownValue($0.activityCount) }.max() ?? 0
                ForEach(profile.skills) { item in
                    OverviewInvocationRankingRow(
                        name: item.name,
                        value: metricText(item.activityCount, localization: localization),
                        count: knownValue(item.activityCount),
                        maximum: maximum,
                        tint: .purple,
                        help: skillHelp(item)
                    )
                }
            }
        }
    }

    private var failureTint: Color {
        guard let count = knownValue(profile.toolFailureCount) else { return .secondary }
        return count > 0 ? .orange : .green
    }

    private func knownValue(_ metric: DisplayMetric) -> Int64? {
        guard case .known(let value, _) = metric else { return nil }
        return value
    }

    private func toolHelp(_ item: OverviewInvocationToolPresentation) -> String {
        var lines = [
            localization.format("%@ 次调用", metricText(item.callCount, localization: localization)),
            localization.format(
                "相关会话：%@",
                metricText(item.sessionCount, localization: localization)
            ),
            localization.format(
                "成功 %@ · 失败 %@ · 结果未知 %@",
                metricText(item.succeededCount, localization: localization),
                metricText(item.failedCount, localization: localization),
                metricText(item.unknownCount, localization: localization)
            ),
        ]
        if let duration = durationText(item.averageDurationMS) {
            lines.append(localization.format("平均耗时：%@", duration))
        }
        if let lastSeen = dateText(item.lastSeenAtMS) {
            lines.append(localization.format("最近：%@", lastSeen))
        }
        return lines.joined(separator: "\n")
    }

    private func skillHelp(_ item: OverviewInvocationSkillPresentation) -> String {
        var lines = [
            localization.format(
                "%@ 次活动",
                metricText(item.activityCount, localization: localization)
            ),
            localization.format(
                "相关会话：%@",
                metricText(item.sessionCount, localization: localization)
            ),
            localization.format(
                "显式引用 %@ · 文件加载 %@",
                metricText(item.explicitCount, localization: localization),
                metricText(item.fileLoadedCount, localization: localization)
            ),
        ]
        if let lastSeen = dateText(item.lastSeenAtMS) {
            lines.append(localization.format("最近：%@", lastSeen))
        }
        return lines.joined(separator: "\n")
    }

    private func durationText(_ metric: DisplayMetric) -> String? {
        guard case .known(let milliseconds, _) = metric else { return nil }
        if milliseconds < 1_000 { return "\(milliseconds) ms" }
        return String(
            format: "%.2f s",
            locale: localization.locale,
            Double(milliseconds) / 1_000
        )
    }

    private func dateText(_ metric: DisplayMetric) -> String? {
        guard case .known(let milliseconds, _) = metric else { return nil }
        return timestampText(milliseconds)
    }
}

private struct OverviewInvocationSummaryMetric: View {
    let title: String
    let value: String
    let detail: String
    let symbol: String
    let tint: Color

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            Label(title, systemImage: symbol)
                .font(.caption.weight(.medium))
                .foregroundStyle(tint)
            Text(value)
                .font(.title3.bold())
                .monospacedDigit()
            Text(detail)
                .font(.caption2)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(10)
        .background(tint.opacity(0.07), in: RoundedRectangle(cornerRadius: 9))
        .accessibilityElement(children: .combine)
    }
}

private struct OverviewInvocationRankingRow: View {
    let name: String
    let value: String
    let count: Int64?
    let maximum: Int64
    let tint: Color
    let help: String

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack(spacing: 12) {
                Text(name)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 12)
                Text(value)
                    .font(.subheadline.weight(.semibold))
                    .monospacedDigit()
            }
            GeometryReader { geometry in
                ZStack(alignment: .leading) {
                    Capsule().fill(tint.opacity(0.12))
                    Capsule()
                        .fill(tint)
                        .frame(width: geometry.size.width * fraction)
                }
            }
            .frame(height: 6)
        }
        .padding(.vertical, 3)
        .help(help)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(name)
        .accessibilityValue(help)
    }

    private var fraction: Double {
        guard let count, maximum > 0 else { return 0 }
        return min(max(Double(count) / Double(maximum), 0), 1)
    }
}

private struct OverviewActivityCard: View {
    let activity: OverviewActivityPresentation
    let rangeLabel: String
    let fillsProposedHeight: Bool
    let localization: AppLocalization
    @State private var selectedMetric = OverviewActivityMetric.tokenConsumption
    @State private var selectedTimelineDate: Date?
    @State private var hoveredHeatmapCellID: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(alignment: .center, spacing: 12) {
                VStack(alignment: .leading, spacing: 3) {
                    Text("活动分布")
                        .font(.headline)
                    Text(scopeText)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer(minLength: 12)
                Picker("活动指标", selection: $selectedMetric) {
                    ForEach(OverviewActivityMetric.allCases) { metric in
                        Text(localization.textValue(metric.title)).tag(metric)
                    }
                }
                .labelsHidden()
                .pickerStyle(.segmented)
                .frame(width: 220)
            }

            switch activity.availability {
            case .unavailable:
                ContentUnavailableView(
                    "活动分布暂时不可用",
                    systemImage: "chart.bar.xaxis",
                    description: Text("当前范围的 Token 与会话活动尚未准备好。")
                )
                .frame(maxWidth: .infinity, minHeight: 390)
            case .available, .partial:
                timelineChart
                Divider()
                weekdayHourHeatmap
            }
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
        .onChange(of: selectedMetric) {
            selectedTimelineDate = nil
            hoveredHeatmapCellID = nil
        }
    }

    @ViewBuilder
    private var timelineChart: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 12) {
                Text("时段活动")
                    .font(.subheadline.weight(.semibold))
                Spacer()
                Text(
                    selectedTimelinePoint.map(timelinePointDetail)
                        ?? localization.textValue("悬停柱体查看详情")
                )
                    .font(.caption2)
                    .foregroundStyle(selectedTimelinePoint == nil ? .secondary : metricColor)
                    .monospacedDigit()
            }
            if timelinePoints.isEmpty {
                ContentUnavailableView(
                    "当前范围暂无活动",
                    systemImage: "chart.bar.xaxis"
                )
                .frame(maxWidth: .infinity, minHeight: 150)
            } else {
                Chart(timelinePoints) { point in
                    RectangleMark(
                        xStart: .value("开始时间", point.barStartDate),
                        xEnd: .value("结束时间", point.barEndDate),
                        yStart: .value("基线", Int64.zero),
                        yEnd: .value(selectedMetric.title, point.value)
                    )
                    .foregroundStyle(metricColor.opacity(barOpacity(point)))
                    .cornerRadius(6)
                    .accessibilityLabel(activityTimeRangeText(point))
                    .accessibilityValue(activityValueText(point.value))
                }
                .chartXScale(domain: timelineDomain)
                .chartXScale(
                    range: .plotDimension(startPadding: 24, endPadding: 32)
                )
                .chartYAxis {
                    AxisMarks(position: .leading) { value in
                        AxisGridLine()
                            .foregroundStyle(Color.secondary.opacity(0.10))
                        AxisValueLabel {
                            if let value = value.as(Int64.self) {
                                Text(axisValueText(value))
                            }
                        }
                    }
                }
                .chartXAxis {
                    AxisMarks(values: timelineAxisTicks.map(\.date)) { value in
                        AxisGridLine(stroke: StrokeStyle(dash: [3, 3]))
                            .foregroundStyle(Color.secondary.opacity(0.08))
                        AxisValueLabel {
                            if let date = value.as(Date.self),
                               let tick = timelineAxisTicks.first(where: { $0.date == date })
                            {
                                Text(tick.label)
                                    .fixedSize(horizontal: true, vertical: false)
                            }
                        }
                    }
                }
                .chartOverlay { proxy in
                    GeometryReader { geometry in
                        Rectangle()
                            .fill(.clear)
                            .contentShape(Rectangle())
                            .onContinuousHover { phase in
                                switch phase {
                                case .active(let location):
                                    guard let plotFrame = proxy.plotFrame else {
                                        selectedTimelineDate = nil
                                        return
                                    }
                                    let plotRect = geometry[plotFrame]
                                    guard plotRect.contains(location) else {
                                        selectedTimelineDate = nil
                                        return
                                    }
                                    selectedTimelineDate = proxy.value(
                                        atX: location.x - plotRect.origin.x,
                                        as: Date.self
                                    )
                                case .ended:
                                    selectedTimelineDate = nil
                                }
                            }
                    }
                }
                .frame(height: 170)
            }
        }
    }

    private var weekdayHourHeatmap: some View {
        let renderPlan = OverviewActivityHeatmapRenderPlan(
            cells: activity.heatmap,
            metric: selectedMetric
        )
        return VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text("星期与小时分布")
                    .font(.subheadline.weight(.semibold))
                Spacer()
                Text(hoveredHeatmapCell.map(cellHelp) ?? heatmapHint)
                    .font(.caption2)
                    .foregroundStyle(hoveredHeatmapCell == nil ? .secondary : metricColor)
                    .monospacedDigit()
            }
            GeometryReader { proxy in
                let spacing: CGFloat = 3
                let labelWidth: CGFloat = 30
                let gridOrigin = CGPoint(x: labelWidth + spacing, y: 16)
                let layout = ActivityHeatmapGridLayout(
                    availableWidth: proxy.size.width - gridOrigin.x,
                    columnCount: 24,
                    rowCount: 7,
                    horizontalSpacing: spacing,
                    verticalSpacing: 4,
                    minimumCellSize: 6,
                    maximumCellSize: 22
                )

                ZStack(alignment: .topLeading) {
                    Canvas { context, _ in
                        drawWeekdayHourHeatmap(
                            context: &context,
                            layout: layout,
                            gridOrigin: gridOrigin,
                            labelWidth: labelWidth,
                            renderPlan: renderPlan
                        )
                    }

                    if let hoveredHeatmapCell,
                       let frame = layout.cellFrame(
                           column: hoveredHeatmapCell.hour,
                           row: hoveredHeatmapCell.weekday - 1
                       )
                    {
                        Text(cellHelp(hoveredHeatmapCell))
                            .font(.caption)
                            .foregroundStyle(.primary)
                            .fixedSize()
                            .padding(.horizontal, 8)
                            .padding(.vertical, 5)
                            .background(
                                .ultraThickMaterial,
                                in: RoundedRectangle(cornerRadius: 6, style: .continuous)
                            )
                            .overlay {
                                RoundedRectangle(cornerRadius: 6, style: .continuous)
                                    .stroke(.separator.opacity(0.55), lineWidth: 0.5)
                            }
                            .shadow(color: .black.opacity(0.14), radius: 5, y: 2)
                            .position(
                                x: min(
                                    max(gridOrigin.x + frame.midX, 110),
                                    max(proxy.size.width - 110, 110)
                                ),
                                y: max(gridOrigin.y + frame.minY - 18, 0)
                            )
                            .allowsHitTesting(false)
                            .transition(.opacity)
                    }
                }
                .contentShape(Rectangle())
                .onContinuousHover { phase in
                    switch phase {
                    case .active(let location):
                        updateWeekdayHourSelection(
                            cellID: weekdayHourCell(
                                at: CGPoint(
                                    x: location.x - gridOrigin.x,
                                    y: location.y - gridOrigin.y
                                ),
                                layout: layout,
                                renderPlan: renderPlan
                            )?.id,
                            animated: true
                        )
                    case .ended:
                        updateWeekdayHourSelection(cellID: nil, animated: true)
                    }
                }
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(Text(localization.textValue("星期与小时分布")))
                .accessibilityValue(Text(
                    hoveredHeatmapCell.map(cellHelp) ?? heatmapHint
                ))
                .accessibilityAdjustableAction { direction in
                    let selectionDirection: ActivityHeatmapSelectionDirection
                    switch direction {
                    case .increment: selectionDirection = .increment
                    case .decrement: selectionDirection = .decrement
                    @unknown default: return
                    }
                    updateWeekdayHourSelection(
                        cellID: ActivityHeatmapSelectionResolver.move(
                            from: hoveredHeatmapCellID,
                            orderedIDs: renderPlan.rows.flatMap(\.cells).map(\.id),
                            direction: selectionDirection
                        ),
                        animated: false
                    )
                }
            }
            .frame(height: 202)
        }
    }

    private var timelinePoints: [OverviewActivityChartPoint] {
        activity.timeline.compactMap { point in
            guard let value = point.value(for: selectedMetric),
                  let barRange = OverviewActivityTimelineResolver.visibleRange(for: point)
            else { return nil }
            let startDate = Date(timeIntervalSince1970: Double(point.startAtMS) / 1_000)
            let endDate = Date(timeIntervalSince1970: Double(point.endAtMS) / 1_000)
            return OverviewActivityChartPoint(
                id: point.id,
                startDate: startDate,
                endDate: endDate,
                barStartDate: barRange.lowerBound,
                barEndDate: barRange.upperBound,
                value: value
            )
        }
    }

    private var timelineAxisTicks: [OverviewActivityAxisTick] {
        OverviewActivityTimelineResolver.axisTicks(
            points: activity.timeline.filter { $0.value(for: selectedMetric) != nil },
            granularity: activity.timelineGranularity ?? .day,
            timeZoneID: activity.reportingTimeZone
        )
    }

    private var timelineDomain: ClosedRange<Date> {
        guard let first = timelinePoints.first, let last = timelinePoints.last else {
            let now = Date()
            return now...now
        }
        return first.startDate...last.endDate
    }

    private var selectedTimelinePoint: OverviewActivityChartPoint? {
        let availablePoints = activity.timeline.filter { $0.value(for: selectedMetric) != nil }
        guard let selected = OverviewActivityTimelineResolver.nearest(
            to: selectedTimelineDate,
            in: availablePoints
        ) else { return nil }
        return timelinePoints.first(where: { $0.id == selected.id })
    }

    private var hoveredHeatmapCell: OverviewActivityHeatmapCell? {
        guard let hoveredHeatmapCellID else { return nil }
        return activity.heatmap.first(where: { $0.id == hoveredHeatmapCellID })
    }

    private var heatmapHint: String {
        localization.textValue(
            activity.availability == .partial ? "部分格子暂不可用" : "悬停格子查看详情"
        )
    }

    private var metricColor: Color {
        selectedMetric == .tokenConsumption ? .blue : .green
    }

    private var scopeText: String {
        rangeLabel
    }

    private func weekdayText(_ weekday: Int) -> String {
        ["周一", "周二", "周三", "周四", "周五", "周六", "周日"].map {
            localization.textValue($0)
        }[
            min(max(weekday - 1, 0), 6)
        ]
    }

    private func barOpacity(_ point: OverviewActivityChartPoint) -> Double {
        guard let selectedTimelinePoint else { return 0.72 }
        return selectedTimelinePoint.id == point.id ? 0.96 : 0.26
    }

    private func activityTimeRangeText(_ point: OverviewActivityChartPoint) -> String {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: activity.reportingTimeZone) ?? .current
        let startFormatter = DateFormatter()
        startFormatter.calendar = calendar
        startFormatter.locale = localization.locale
        startFormatter.timeZone = TimeZone(identifier: activity.reportingTimeZone)
        startFormatter.dateFormat = localization.language == .englishUS ? "MMM d H:mm" : "M月d日 H:mm"

        let endFormatter = DateFormatter()
        endFormatter.calendar = calendar
        endFormatter.locale = localization.locale
        endFormatter.timeZone = TimeZone(identifier: activity.reportingTimeZone)
        endFormatter.dateFormat = calendar.isDate(
            point.startDate,
            inSameDayAs: point.endDate
        ) ? "H:mm" : (localization.language == .englishUS ? "MMM d H:mm" : "M月d日 H:mm")
        let start = startFormatter.string(from: point.startDate)
        let end = endFormatter.string(from: point.endDate)
        return "\(start)–\(end)"
    }

    private func timelinePointDetail(_ point: OverviewActivityChartPoint) -> String {
        "\(activityTimeRangeText(point)) · \(activityValueText(point.value))"
    }

    private func activityValueText(_ value: Int64) -> String {
        switch selectedMetric {
        case .tokenConsumption:
            TokenQuantityFormatter.stringWithUnit(value, localization: localization)
        case .sessionCount:
            localization.quantity("copy.count.session", count: value)
        }
    }

    private func axisValueText(_ value: Int64) -> String {
        switch selectedMetric {
        case .tokenConsumption: TokenQuantityFormatter.compactString(value, localization: localization)
        case .sessionCount: localization.number(value)
        }
    }

    private func cellValueText(_ cell: OverviewActivityHeatmapCell) -> String {
        guard let value = cell.value(for: selectedMetric) else {
            return localization.textValue("数据暂不可用")
        }
        return activityValueText(value)
    }

    private func cellHelp(_ cell: OverviewActivityHeatmapCell) -> String {
        localization.format(
            "activity.heatmap.detail", weekdayText(cell.weekday), cell.hour, cellValueText(cell)
        )
    }

    private func drawWeekdayHourHeatmap(
        context: inout GraphicsContext,
        layout: ActivityHeatmapGridLayout,
        gridOrigin: CGPoint,
        labelWidth: CGFloat,
        renderPlan: OverviewActivityHeatmapRenderPlan
    ) {
        for hour in stride(from: 0, to: 24, by: 3) {
            guard let frame = layout.cellFrame(column: hour, row: 0) else { continue }
            context.draw(
                Text("\(hour)")
                    .font(.system(size: 8))
                    .foregroundStyle(.secondary),
                at: CGPoint(x: gridOrigin.x + frame.midX, y: 0),
                anchor: .top
            )
        }

        for row in renderPlan.rows {
            guard let labelFrame = layout.cellFrame(column: 0, row: row.weekday - 1) else {
                continue
            }
            context.draw(
                Text(weekdayText(row.weekday))
                    .font(.caption2)
                    .foregroundStyle(.secondary),
                at: CGPoint(x: labelWidth, y: gridOrigin.y + labelFrame.midY),
                anchor: .trailing
            )
            for renderCell in row.cells {
                guard let frame = layout.cellFrame(
                    column: renderCell.cell.hour,
                    row: row.weekday - 1
                ) else { continue }
                let resolvedFrame = frame.offsetBy(dx: gridOrigin.x, dy: gridOrigin.y)
                let path = Path(
                    roundedRect: resolvedFrame,
                    cornerRadius: max(2, layout.cellSize * 0.22)
                )
                context.fill(
                    path,
                    with: .color(activityHeatmapColor(
                        for: renderCell.intensity,
                        tint: metricColor
                    ))
                )
                if renderCell.intensity == .unknown {
                    context.stroke(
                        path,
                        with: .color(.secondary.opacity(0.28)),
                        lineWidth: 0.7
                    )
                }
            }
        }
    }

    private func weekdayHourCell(
        at point: CGPoint,
        layout: ActivityHeatmapGridLayout,
        renderPlan: OverviewActivityHeatmapRenderPlan
    ) -> OverviewActivityHeatmapCell? {
        guard let position = layout.position(at: point),
              renderPlan.rows.indices.contains(position.row),
              renderPlan.rows[position.row].cells.indices.contains(position.column)
        else { return nil }
        return renderPlan.rows[position.row].cells[position.column].cell
    }

    private func updateWeekdayHourSelection(cellID: String?, animated: Bool) {
        guard hoveredHeatmapCellID != cellID else { return }
        let update = { hoveredHeatmapCellID = cellID }
        if animated {
            withAnimation(.easeOut(duration: 0.12), update)
        } else {
            update()
        }
    }
}

private struct OverviewActivityChartPoint: Identifiable {
    let id: Int64
    let startDate: Date
    let endDate: Date
    let barStartDate: Date
    let barEndDate: Date
    let value: Int64
}

private struct TokenActivityCard: View {
    let card: TokenActivityCardPresentation
    let localization: AppLocalization
    @State private var hoverState = TokenActivityHoverState()

    var body: some View {
        SectionCard(title: card.title) {
            if card.availability == .unavailable {
                ContentUnavailableView(
                    "年度活动暂时不可用",
                    systemImage: "calendar.badge.exclamationmark",
                    description: Text("额度状态和消耗概览仍可继续查看。")
                )
                .frame(height: 160)
            } else {
                metricStrip
                Divider()
                heatmapHeader
                heatmap
            }
        }
        .accessibilityIdentifier("overview.token-activity")
    }

    private var metricStrip: some View {
        HStack(alignment: .top, spacing: 14) {
            ForEach(Array(card.metrics.enumerated()), id: \.element.id) { index, metric in
                if index > 0 { Divider().frame(height: 54) }
                VStack(alignment: .leading, spacing: 5) {
                    Text(metric.value)
                        .font(.system(size: 22, weight: .semibold, design: .rounded))
                        .monospacedDigit()
                    Text(verbatim: metric.title)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .minimumScaleFactor(0.72)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityElement(children: .combine)
            }
        }
    }

    private var heatmapHeader: some View {
        HStack(spacing: 10) {
            Text(verbatim: card.scope)
                .font(.subheadline.weight(.semibold))
            if let notice = card.notice {
                Label {
                    Text(verbatim: notice)
                } icon: {
                    Image(systemName: "externaldrive")
                }
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
        }
    }

    private var heatmap: some View {
        GeometryReader { geometry in
            let weekCount = max(card.calendar.weeks.count, 1)
            let labelWidth: CGFloat = 24
            let labelGap: CGFloat = 8
            let gridOrigin = CGPoint(x: labelWidth + labelGap, y: 18)
            let spacing: CGFloat = geometry.size.width >= 900 ? 3 : 2
            let layout = ActivityHeatmapGridLayout(
                availableWidth: geometry.size.width - gridOrigin.x,
                columnCount: weekCount,
                rowCount: 7,
                horizontalSpacing: spacing,
                verticalSpacing: spacing,
                minimumCellSize: 6
            )

            ZStack(alignment: .topLeading) {
                Canvas { context, _ in
                    drawAnnualHeatmap(
                        context: &context,
                        layout: layout,
                        gridOrigin: gridOrigin,
                        labelWidth: labelWidth
                    )
                }

                if let selectedDay,
                   let position = heatmapPosition(for: selectedDay.id),
                   let frame = layout.cellFrame(column: position.column, row: position.row)
                {
                    Text(card.dayDetail(selectedDay))
                        .font(.caption)
                        .foregroundStyle(.primary)
                        .fixedSize()
                        .padding(.horizontal, 8)
                        .padding(.vertical, 5)
                        .background(
                            .ultraThickMaterial,
                            in: RoundedRectangle(cornerRadius: 6, style: .continuous)
                        )
                        .overlay {
                            RoundedRectangle(cornerRadius: 6, style: .continuous)
                                .stroke(.separator.opacity(0.55), lineWidth: 0.5)
                        }
                        .shadow(color: .black.opacity(0.14), radius: 5, y: 2)
                        .position(
                            x: min(
                                max(gridOrigin.x + frame.midX, 110),
                                max(geometry.size.width - 110, 110)
                            ),
                            y: max(gridOrigin.y + frame.minY - 18, 0)
                        )
                        .allowsHitTesting(false)
                        .transition(.opacity)
                }
            }
            .contentShape(Rectangle())
            .onContinuousHover { phase in
                switch phase {
                case .active(let location):
                    updateAnnualHeatmapSelection(
                        dayID: heatmapDay(
                            at: CGPoint(
                                x: location.x - gridOrigin.x,
                                y: location.y - gridOrigin.y
                            ),
                            layout: layout
                        )?.id,
                        animated: true
                    )
                case .ended:
                    updateAnnualHeatmapSelection(dayID: nil, animated: true)
                }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(Text(verbatim: "\(card.title) · \(card.scope)"))
            .accessibilityValue(Text(verbatim: selectedDay.map(card.dayDetail) ?? card.scope))
            .accessibilityAdjustableAction { direction in
                let selectionDirection: ActivityHeatmapSelectionDirection
                switch direction {
                case .increment: selectionDirection = .increment
                case .decrement: selectionDirection = .decrement
                @unknown default: return
                }
                updateAnnualHeatmapSelection(
                    dayID: ActivityHeatmapSelectionResolver.move(
                        from: hoverState.dayID,
                        orderedIDs: orderedDays.map(\.id),
                        direction: selectionDirection
                    ),
                    animated: false
                )
            }
        }
        .frame(maxWidth: .infinity)
        .aspectRatio(6.8, contentMode: .fit)
    }

    private var orderedDays: [TokenActivityCalendarDay] {
        card.calendar.weeks.flatMap(\.days).compactMap { $0 }
    }

    private var selectedDay: TokenActivityCalendarDay? {
        guard let dayID = hoverState.dayID else { return nil }
        return orderedDays.first(where: { $0.id == dayID })
    }

    private func drawAnnualHeatmap(
        context: inout GraphicsContext,
        layout: ActivityHeatmapGridLayout,
        gridOrigin: CGPoint,
        labelWidth: CGFloat
    ) {
        for row in 0..<7 {
            let label = row == 0 ? "一" : row == 2 ? "三" : row == 4 ? "五" : ""
            guard !label.isEmpty,
                  let frame = layout.cellFrame(column: 0, row: row)
            else { continue }
            context.draw(
                Text(verbatim: localization.textValue(label))
                    .font(.caption2)
                    .foregroundStyle(.secondary),
                at: CGPoint(x: labelWidth, y: gridOrigin.y + frame.midY),
                anchor: .trailing
            )
        }

        for label in card.calendar.monthLabels {
            guard let frame = layout.cellFrame(column: label.weekIndex, row: 0) else { continue }
            context.draw(
                Text(label.title)
                    .font(.caption2)
                    .foregroundStyle(.secondary),
                at: CGPoint(x: gridOrigin.x + frame.minX, y: 0),
                anchor: .topLeading
            )
        }

        for (column, week) in card.calendar.weeks.enumerated() {
            for (row, day) in week.days.enumerated() {
                guard let day,
                      let frame = layout.cellFrame(column: column, row: row)
                else { continue }
                let resolvedFrame = frame.offsetBy(dx: gridOrigin.x, dy: gridOrigin.y)
                let cornerRadius = max(2, layout.cellSize * 0.22)
                let path = Path(
                    roundedRect: resolvedFrame,
                    cornerRadius: cornerRadius
                )
                context.fill(path, with: .color(color(for: day.intensity)))
                if day.intensity == .unknown {
                    context.stroke(
                        path,
                        with: .color(.secondary.opacity(0.28)),
                        lineWidth: 0.7
                    )
                }
            }
        }
    }

    private func heatmapDay(
        at point: CGPoint,
        layout: ActivityHeatmapGridLayout
    ) -> TokenActivityCalendarDay? {
        guard let position = layout.position(at: point),
              card.calendar.weeks.indices.contains(position.column),
              card.calendar.weeks[position.column].days.indices.contains(position.row)
        else { return nil }
        return card.calendar.weeks[position.column].days[position.row]
    }

    private func heatmapPosition(for dayID: String) -> ActivityHeatmapGridPosition? {
        for (column, week) in card.calendar.weeks.enumerated() {
            if let row = week.days.firstIndex(where: { $0?.id == dayID }) {
                return ActivityHeatmapGridPosition(column: column, row: row)
            }
        }
        return nil
    }

    private func updateAnnualHeatmapSelection(dayID: String?, animated: Bool) {
        guard hoverState.dayID != dayID else { return }
        let update = { hoverState = TokenActivityHoverState(dayID: dayID) }
        if animated {
            withAnimation(.easeOut(duration: 0.12), update)
        } else {
            update()
        }
    }

    private func color(for intensity: TokenActivityIntensity) -> Color {
        activityHeatmapColor(for: intensity, tint: .blue)
    }
}

func activityHeatmapColor(
    for intensity: TokenActivityIntensity,
    tint: Color
) -> Color {
    switch intensity {
    case .unknown: .secondary.opacity(0.08)
    case .none: .secondary.opacity(0.10)
    case .low: tint.opacity(0.24)
    case .medium: tint.opacity(0.43)
    case .high: tint.opacity(0.68)
    case .veryHigh: tint
    }
}

private struct OverviewChartPoint: Identifiable {
    let id: String
    let date: Date
    let tokens: Int64
    let tokenBreakdown: TokenBreakdownPresentation
    let estimatedCost: DisplayMetric
}

private struct ProjectUsageRow: View {
    let rank: Int?
    let title: String
    let tokens: TokenBreakdownPresentation
    let fraction: Double?
    let localization: AppLocalization

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack(spacing: 8) {
                Text(rank.map(String.init) ?? "·")
                    .font(.caption.bold())
                    .foregroundStyle(.secondary)
                    .frame(width: 18)
                Text(title).lineLimit(1)
                Spacer(minLength: 8)
            }
            TokenBreakdownView(
                tokens: tokens,
                style: .compact,
                localization: localization
            )
                .padding(.leading, 26)
            ProgressView(value: fraction ?? 0)
                .tint(.blue)
                .opacity(fraction == nil ? 0.35 : 1)
        }
        .padding(.vertical, 3)
        .contentShape(Rectangle())
    }
}

struct MetricCard: View {
    let title: String
    let value: String
    let detail: String
    var systemImage: String? = nil

    var body: some View {
        let localization = AppLocalizationRegistry.shared.current
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 7) {
                if let systemImage {
                    Image(systemName: systemImage)
                        .foregroundStyle(.tint)
                }
                Text(localization.textValue(title)).font(.headline)
            }
            Text(value).font(.system(size: 30, weight: .semibold, design: .rounded)).monospacedDigit()
            Text(localization.textValue(detail)).font(.caption).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(16)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .stroke(.quaternary, lineWidth: 1)
        }
        .accessibilityElement(children: .combine)
    }
}

private func metricValue(_ metric: DisplayMetric) -> Int64? {
    if case .known(let value, _) = metric { return value }
    return nil
}

func metricText(
    _ metric: DisplayMetric,
    cost: Bool = false,
    localization: AppLocalization = AppLocalizationRegistry.shared.current
) -> String {
    switch metric {
    case .known(let value, let unit):
        if cost {
            return String(
                format: "$%.2f", locale: localization.locale, Double(value) / 1_000_000
            )
        }
        if unit == "tokens" {
            return TokenQuantityFormatter.string(value, localization: localization)
        }
        return localization.number(value)
    case .unknown, .absent:
        return "--"
    }
}
