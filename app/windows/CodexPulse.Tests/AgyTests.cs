using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using CodexPulse.CoreClient;
using Grpc.Core;

internal static class AgyTests
{
    private static void Require(bool condition, string name)
    {
        if (!condition) throw new InvalidOperationException(name);
        Console.WriteLine("PASS " + name);
    }

    private static AsyncUnaryCall<T> Reply<T>(Task<T> response) where T : class =>
        new(response, Task.FromResult(new Metadata()), () => Status.DefaultSuccess, () => new Metadata(), () => { });

    private static NumericValue Number(long value) => new() { Value = value };

    private sealed class SettingsPeer : CoreService.CoreServiceClient
    {
        public SettingsResponse Value = new()
        {
            Snapshot = new()
            {
                Revision = "1",
                Home = new() { Configured = true, Generation = "1", SwitchStatus = "stable" },
                Online = new() { QuotaEnabled = true, AgyOnlineEnabled = true },
                Refresh = new() { QuotaIntervalSeconds = 300, ResetCreditsIntervalSeconds = 1800, ReconcileIntervalSeconds = 1800, JsonlDebounceMilliseconds = 3000 },
                Updates = new() { Channel = "stable", CheckIntervalSeconds = 3600 },
                Ui = new() { Locale = "zh-CN", LaunchBehavior = "main_window", OverviewRange = "quota_week" },
                CodexAccounts = new() { RetainQuotaHistory = true },
                Providers =
                {
                    new SettingsProviderSnapshot { Provider = "codex", Intent = ProviderIntent.Auto, EffectiveState = ProviderEffectiveState.Enabled },
                    new SettingsProviderSnapshot { Provider = "cursor", Intent = ProviderIntent.Auto, EffectiveState = ProviderEffectiveState.Enabled },
                    new SettingsProviderSnapshot { Provider = "grok", Intent = ProviderIntent.Auto, EffectiveState = ProviderEffectiveState.Enabled },
                    new SettingsProviderSnapshot { Provider = "dsh", Intent = ProviderIntent.Auto, EffectiveState = ProviderEffectiveState.Enabled },
                    new SettingsProviderSnapshot { Provider = "agy", Intent = ProviderIntent.Auto, EffectiveState = ProviderEffectiveState.Enabled }
                }
            }
        };

        public UpdateSettingsRequest? LastRequest;

        public override AsyncUnaryCall<SettingsResponse> SettingsAsync(SettingsRequest request, CallOptions options) =>
            Reply(Task.FromResult(Value.Clone()));

        public override AsyncUnaryCall<SettingsUpdateReceipt> UpdateSettingsAsync(UpdateSettingsRequest request, CallOptions options)
        {
            LastRequest = request.Clone();
            if (request.Online is { HasAgyOnlineEnabled: true })
                Value.Snapshot.Online.AgyOnlineEnabled = request.Online.AgyOnlineEnabled;
            foreach (var update in request.Providers)
            {
                var target = Value.Snapshot.Providers.FirstOrDefault(item => item.Provider == update.Provider);
                if (target is not null) target.Intent = update.Intent;
            }
            Value.Snapshot.Revision = "2";
            return Reply(Task.FromResult(new SettingsUpdateReceipt { Revision = "2", Result = "applied" }));
        }
    }

    public static async Task Run()
    {
        var zh = new Localization();
        zh.Set("zh-CN");
        var en = new Localization();
        en.Set("en-US");

        // 1. 5-way provider order, display names, compact name, and capability boundaries.
        Require(Enumerable.SequenceEqual(Workspace.ProviderOrder, new[] { "codex", "cursor", "grok", "dsh", "agy" }),
            "provider order puts agy fifth after codex, cursor, grok, and dsh");
        Require(Workspace.ProviderDisplayName("agy") == "Antigravity" && Workspace.ProviderCompactName("agy") == "AGY",
            "agy display name is Antigravity and compact label is AGY");
        Require(!Workspace.SupportsInvocationUsage("agy") && Workspace.SupportsInvocationUsage("codex") && Workspace.SupportsInvocationUsage("dsh"),
            "InvocationUsage is hidden and disabled for agy");
        Require(Workspace.SupportsThroughput("agy") && Workspace.SupportsAccountSnapshot("agy") && Workspace.SupportsQuota("agy"),
            "agy enables throughput, account snapshot, and quota capabilities");

        // 2. 5-way switching, independent tray selection, AccountQuotas guard, and disabled semantics.
        var workspace = new Workspace();
        workspace.ApplyProviders([
            new() { Provider = "agy", EffectiveState = ProviderEffectiveState.Enabled },
            new() { Provider = "dsh", EffectiveState = ProviderEffectiveState.Enabled },
            new() { Provider = "grok", EffectiveState = ProviderEffectiveState.Enabled },
            new() { Provider = "cursor", EffectiveState = ProviderEffectiveState.Enabled },
            new() { Provider = "codex", EffectiveState = ProviderEffectiveState.Enabled }
        ]);
        Require(workspace.EnabledProviders.SequenceEqual(new[] { "codex", "cursor", "grok", "dsh", "agy" }),
            "enabled providers preserve canonical 5-way ordering");
        workspace.SelectProvider("agy");
        workspace.SelectProvider("cursor", tray: true);
        Require(workspace.Provider == "agy" && workspace.TrayProvider == "cursor",
            "main window and tray select agy independently");
        workspace.Navigate(Feature.AccountQuotas);
        Require(workspace.Feature == Feature.Overview && workspace.RangePreset == "seven_days",
            "Codex AccountQuotas redirects to Overview on agy with seven_days default");
        workspace.SelectProvider("agy", tray: true);
        Require(workspace.TrayProvider == "agy", "tray switches to agy when enabled");

        workspace.ApplyProviders([
            new() { Provider = "codex", EffectiveState = ProviderEffectiveState.Enabled },
            new() { Provider = "agy", EffectiveState = ProviderEffectiveState.Disabled }
        ]);
        Require(!workspace.EnabledProviders.Contains("agy") && workspace.Provider == "codex" && workspace.TrayProvider == "codex",
            "disabled agy is removed from enabled list and evicted from active main/tray selections");
        workspace.SelectProvider("agy");
        workspace.SelectProvider("agy", tray: true);
        Require(workspace.Provider == "codex" && workspace.TrayProvider == "codex",
            "disabled agy cannot be re-selected");

        // 3. Multi-pool quota & pace presentation.
        const long nowMs = 1_795_000_000_000L;
        var multiPool = new CurrentQuota { EvaluatedAtMs = nowMs };
        multiPool.Windows.Add(new CurrentWindow { LimitId = "agy.claude_gpt", LimitName = "Claude & GPT", WindowKind = "secondary", WindowMinutes = 10080, RemainingPercent = 80, ResetsAtMs = nowMs + 86_400_000, Freshness = "fresh" });
        multiPool.Windows.Add(new CurrentWindow { LimitId = "agy.pool.zeta", WindowKind = "primary", WindowMinutes = 300, RemainingPercent = 90, ResetsAtMs = nowMs + 3_600_000, Freshness = "fresh" });
        multiPool.Windows.Add(new CurrentWindow { LimitId = "agy.carbon", LimitName = "Gemini 4 Carbon", WindowKind = "secondary", WindowMinutes = 10080, RemainingPercent = 65, ResetsAtMs = nowMs + 86_400_000, Freshness = "fresh" });
        multiPool.Windows.Add(new CurrentWindow { LimitId = "agy.argon", LimitName = "Gemini 4 Argon", WindowKind = "secondary", WindowMinutes = 10080, RemainingPercent = 55, ResetsAtMs = nowMs + 86_400_000, Freshness = "fresh" });
        multiPool.Windows.Add(new CurrentWindow { LimitId = "agy.gemini", LimitName = "Gemini", WindowKind = "secondary", WindowMinutes = 10080, RemainingPercent = 70, ResetsAtMs = nowMs + 86_400_000, Freshness = "fresh" });
        multiPool.Windows.Add(new CurrentWindow { LimitId = "agy.gemini", LimitName = "Gemini", WindowKind = "primary", WindowMinutes = 300, RemainingPercent = 40, ResetsAtMs = nowMs + 3_600_000, Freshness = "fresh" });
        multiPool.Windows.Add(new CurrentWindow { LimitId = "agy.claude_gpt", LimitName = "Claude & GPT", WindowKind = "primary", WindowMinutes = 300, RemainingPercent = 50, ResetsAtMs = nowMs + 3_600_000, Freshness = "fresh" });

        var pools = QuotaPresentation.AgyPools(multiPool, zh);
        Require(pools.Select(p => p.LimitId).SequenceEqual(new[] { "agy.gemini", "agy.argon", "agy.carbon", "agy.claude_gpt", "agy.pool.zeta" }),
            "agy multi-pool cards group and order pools deterministically");
        Require(pools[0].Primary?.RemainingPercent == 40 && pools[0].Secondary?.RemainingPercent == 70 && pools[1].Primary is null && pools[1].Secondary?.RemainingPercent == 55,
            "agy pool grouping pairs 5h primary with weekly secondary and retains weekly-only Pro pools");

        var pace = new CurrentQuotaPace { EvaluatedAtMs = nowMs };
        pace.Windows.Add(new QuotaPaceWindow { LimitId = "agy.claude_gpt", WindowKind = "primary", WindowMinutes = 300, RemainingPercent = 50 });
        pace.Windows.Add(new QuotaPaceWindow { LimitId = "agy.gemini", WindowKind = "secondary", WindowMinutes = 10080, RemainingPercent = 70 });
        pace.Windows.Add(new QuotaPaceWindow { LimitId = "agy.gemini", WindowKind = "primary", WindowMinutes = 300, RemainingPercent = 40 });
        var pacePools = QuotaPresentation.AgyPacePools(pace, multiPool, zh);
        Require(pacePools.Select(p => p.LimitId).SequenceEqual(new[] { "agy.gemini", "agy.claude_gpt" }) && pacePools[0].DisplayName == "Gemini" && pacePools[0].Primary is not null && pacePools[0].Secondary is not null,
            "agy pace pool switcher groups windows by limit_id in canonical order");

        var quotaWeekContext = new QueryContext("agy", new(2026, 11, 20), new(2026, 11, 27), "Asia/Hong_Kong", 1, "quota_week");
        var resolved = DashboardPresentation.ResolveRange(quotaWeekContext, multiPool, DateTimeOffset.FromUnixTimeMilliseconds(nowMs));
        Require(!resolved.Fallback && resolved.Exact.EndAtMs == nowMs,
            "agy quota_week resolves from primary weekly window when active");

        // 4. Tray tightest non-expired pool deterministic tie-break & offline fallback.
        var tieQuota = new CurrentQuota { EvaluatedAtMs = nowMs };
        // Expired 0% window MUST NOT win!
        tieQuota.Windows.Add(new CurrentWindow { LimitId = "agy.gemini", WindowKind = "primary", WindowMinutes = 300, RemainingPercent = 0, ResetsAtMs = nowMs - 1, Freshness = "expired_unknown" });
        // Weekly 25% vs 5h 25% across pools: 5h must win, and among 5h Argon beats Carbon beats Claude & GPT.
        tieQuota.Windows.Add(new CurrentWindow { LimitId = "agy.gemini", WindowKind = "secondary", WindowMinutes = 10080, RemainingPercent = 25, ResetsAtMs = nowMs + 86_400_000, Freshness = "fresh" });
        tieQuota.Windows.Add(new CurrentWindow { LimitId = "agy.claude_gpt", WindowKind = "primary", WindowMinutes = 300, RemainingPercent = 25, ResetsAtMs = nowMs + 3_600_000, Freshness = "fresh" });
        tieQuota.Windows.Add(new CurrentWindow { LimitId = "agy.carbon", WindowKind = "primary", WindowMinutes = 300, RemainingPercent = 25, ResetsAtMs = nowMs + 3_600_000, Freshness = "fresh" });
        tieQuota.Windows.Add(new CurrentWindow { LimitId = "agy.argon", WindowKind = "primary", WindowMinutes = 300, RemainingPercent = 25, ResetsAtMs = nowMs + 3_600_000, Freshness = "fresh" });

        var tightest = TrayPresentation.TightestAgyWindow(tieQuota, nowMs);
        Require(tightest is { LimitId: "agy.argon", WindowKind: "primary" },
            "tightest agy tray window skips expired windows and tie-breaks 5h then Gemini -> Argon -> Carbon -> Claude & GPT");

        // If Gemini 5h is also 25% and non-expired, Gemini beats Argon.
        tieQuota.Windows[0].ResetsAtMs = nowMs + 3_600_000;
        tieQuota.Windows[0].RemainingPercent = 25;
        tieQuota.Windows[0].Freshness = "fresh";
        var geminiTie = TrayPresentation.TightestAgyWindow(tieQuota, nowMs);
        Require(geminiTie is { LimitId: "agy.gemini", WindowKind: "primary" },
            "tightest agy tray tie-break prefers Gemini over Argon on equal 5h remaining");

        var agyQuotaResponse = new QuotaCurrentResponse
        {
            ProviderContext = new() { EffectiveProvider = "agy" },
            Current = tieQuota
        };
        var agyAccount = new AccountSnapshotResponse
        {
            Account = new() { Type = "agy", Email = "agy-user@example.invalid", PlanType = "Google AI Ultra 20×" }
        };
        var today = new DateOnly(2026, 11, 26);
        var todayUsage = new UsageCostResponse { Meta = new() { Status = "complete" } };
        todayUsage.Trend.Add(new TrendPoint { Key = "2026-11-26", Totals = new() { TotalTokens = Number(15000) } });

        var activeSummary = TrayPresentation.SummarizeAgyTray(agyQuotaResponse, agyAccount, todayUsage, today, nowMs, zh);
        Require(!activeSummary.IsFallback && activeSummary.CompactText == "AGY · Gemini 5h 25%",
            "agy tray summary formats compact tightest pool text");

        var offlineSummary = TrayPresentation.SummarizeAgyTray(null, agyAccount, todayUsage, today, nowMs, zh);
        Require(offlineSummary.IsFallback && offlineSummary.CompactText.StartsWith("AGY · 今日 "),
            "offline agy tray summary falls back to today token usage when available");

        var unavailableUsage = new UsageCostResponse { Meta = new() { Status = "unavailable" } };
        var unavailableSummary = TrayPresentation.SummarizeAgyTray(null, null, unavailableUsage, today, nowMs, zh);
        Require(unavailableSummary.IsFallback && unavailableSummary.CompactText == "AGY · --",
            "unavailable agy tray summary falls back to -- when neither quota nor usage is known");

        Require(TrayPresentation.Identity("agy", agyAccount, false, en) == "agy-user@example.invalid · Google AI Ultra 20×",
            "agy account capsule preserves email and standardized plan");
        Require(!TrayPresentation.Identity("agy", agyAccount, true, en).Contains("agy-user"),
            "privacy capture hides agy account details");
        Require(!TrayPresentation.Identity("agy", new AccountSnapshotResponse { Account = new() { Type = "cursor", Email = "wrong@example.invalid", PlanType = "Pro" } }, false, en).Contains("wrong@example.invalid"),
            "agy identity rejects account snapshot from another provider");

        // 5. Role tags and Antigravity source labels.
        Require(AnalysisQueries.SessionRoleTag("role:subagent", zh) == "子代理"
            && AnalysisQueries.SessionRoleTag("role:swarm_worker", zh) == "Swarm"
            && AnalysisQueries.SessionRoleTag("role:side_question", zh) == "支线提问"
            && AnalysisQueries.SessionRoleTag("provider_metadata", zh) is null,
            "lightweight agy session role tags resolve for subagent, swarm_worker, and side_question only");

        var agySourceKeys = new[] { "agy.summaries", "agy.trajectories", "agy.transcripts", "agy.ls_rpc" };
        foreach (var key in agySourceKeys)
        {
            var source = new SourceItem { Provider = "agy", Kind = "provider_source", SourceKey = "provider_source:agy:" + key, SourceType = "sqlite" };
            var label = RuntimeCopy.SourceLabel(source, zh);
            Require(RuntimeCopy.IsAgySource(source) && label.StartsWith("Antigravity · ") && label.Contains(key),
                "Antigravity source label formats " + key);
        }

        // 6. agy_online_enabled settings draft & authoritative save roundtrip.
        var peer = new SettingsPeer();
        var ctx = new QueryContext("agy", new(2026, 11, 20), new(2026, 11, 27), "Asia/Hong_Kong", 1);
        var draft = SettingsDraft.From(peer.Value.Snapshot);
        Require(draft.Online.HasAgyOnlineEnabled && draft.Online.AgyOnlineEnabled,
            "SettingsDraft copies agy_online_enabled with explicit presence");

        using var editor = new SettingsEditor(new(() => peer), () => ctx);
        await editor.ReloadAsync();
        Require(editor.Draft!.Online.HasAgyOnlineEnabled && editor.Draft.Online.AgyOnlineEnabled,
            "SettingsEditor loads agy_online_enabled into draft");
        editor.Draft.Online.AgyOnlineEnabled = false;
        editor.Draft.Providers.Single(p => p.Provider == "agy").Intent = ProviderIntent.Disabled;
        editor.Edited();
        Require(editor.Dirty, "toggling agy_online_enabled and agy intent marks draft dirty");
        await editor.SaveAsync();
        Require(editor.Action == SettingsAction.Applied && !editor.Dirty && peer.LastRequest is { Online: { HasAgyOnlineEnabled: true, AgyOnlineEnabled: false } } && !peer.Value.Snapshot.Online.AgyOnlineEnabled,
            "saving agy_online_enabled roundtrips through UpdateSettings and authoritative readback");
    }
}
