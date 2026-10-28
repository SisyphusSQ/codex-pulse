using System.Globalization;
using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Grpc.Core;

internal static class ShellTests
{
    private static void Require(bool condition, string name)
    { if (!condition) throw new InvalidOperationException(name); Console.WriteLine("PASS " + name); }
    private static AsyncUnaryCall<T> Reply<T>(Task<T> response) where T : class => new(response, Task.FromResult(new Metadata()), () => Status.DefaultSuccess, () => new Metadata(), () => { });
    public static async Task Run()
    {
        var workspace = new Workspace();
        workspace.ApplyProviders([new() { Provider = "codex", EffectiveState = ProviderEffectiveState.Enabled },
            new() { Provider = "cursor", EffectiveState = ProviderEffectiveState.Enabled }, new() { Provider = "grok", EffectiveState = ProviderEffectiveState.Unavailable }]);
        workspace.SelectProvider("cursor");
        Require(workspace.TrayProvider == "codex" && workspace.Provider == "cursor", "window and tray provider selections are independent");
        workspace.Navigate(Feature.AccountQuotas);
        Require(workspace.Feature == Feature.Overview, "Codex account navigation cannot leak into another provider");
        workspace.ApplyProviders([]);
        Require(workspace.Provider is null && workspace.Feature == Feature.Summary, "all disabled providers leave summary available");
        workspace.Navigate(Feature.Settings);
        Require(workspace.Feature == Feature.Settings && !string.IsNullOrEmpty(Workspace.LocalTimeZone()), "settings remain reachable and time zone is explicit");

        using var slot = new QuerySlot<BootstrapResponse>();
        var old = new TaskCompletionSource<BootstrapResponse>(TaskCreationOptions.RunContinuationsAsynchronously);
        var newer = new TaskCompletionSource<BootstrapResponse>(TaskCreationOptions.RunContinuationsAsynchronously);
        var a = slot.LoadAsync("a", _ => old.Task);
        var b = slot.LoadAsync("b", _ => newer.Task);
        newer.SetResult(new() { Name = "new context" }); await b;
        old.SetResult(new() { Name = "obsolete context" }); await a;
        Require(slot.State.Value?.Name == "new context", "obsolete completion cannot overwrite new navigation");
        await slot.LoadAsync("b", _ => Task.FromException<BootstrapResponse>(new IOException()));
        Require(slot.State.Kind == LoadKind.Stale && slot.State.Value?.Name == "new context", "same context failure retains last good with stale status");
        await slot.LoadAsync("different-account", _ => Task.FromException<BootstrapResponse>(new IOException()));
        Require(slot.State.Kind == LoadKind.Unavailable && slot.State.Value is null, "different context cannot retain previous account values");
        slot.Clear(true);
        Require(slot.State.Kind == LoadKind.RecoveryBlocked && slot.State.Value is null, "recovery blocks normal values");

        var client = new FeatureClient(() => null!);
        var context = new QueryContext("codex", new(2026,10,28), new(2026,10,29), "Asia/Hong_Kong", 1);
        var count = 0;
        AsyncUnaryCall<BootstrapResponse> Fetch(CoreService.CoreServiceClient _, DateTime deadline, CancellationToken token)
        { count++; return Reply(Task.FromResult(new BootstrapResponse { Name = count.ToString() })); }
        var first = await client.QueryAsync("Bootstrap", new BootstrapRequest(), context, ["index"], Fetch);
        first.Name = "caller mutation";
        var cached = await client.QueryAsync("Bootstrap", new BootstrapRequest(), context, ["index"], Fetch);
        Require(count == 1 && cached.Name == "1", "cached generated messages are isolated clones");
        await client.QueryAsync("Bootstrap", new BootstrapRequest(), context with { Provider = "cursor" }, ["index"], Fetch);
        await client.QueryAsync("Bootstrap", new BootstrapRequest(), context with { Generation = 2 }, ["index"], Fetch);
        Require(count == 3, "cache separates provider and Home/account context");
        client.Invalidate("quota");
        await client.QueryAsync("Bootstrap", new BootstrapRequest(), context, ["index"], Fetch);
        Require(count == 3, "unrelated invalidation keeps query cache");
        client.Invalidate("index");
        await client.QueryAsync("Bootstrap", new BootstrapRequest(), context, ["index"], Fetch);
        Require(count == 4, "targeted invalidation forces authority query");
        var response = new TaskCompletionSource<BootstrapResponse>(TaskCreationOptions.RunContinuationsAsynchronously);
        var pending = client.QueryAsync("Bootstrap", new BootstrapRequest(), context with { Generation = 9 }, ["index"], (_,_,_) => Reply(response.Task));
        client.Invalidate("index"); response.SetResult(new() { Name = "outdated" }); await pending;
        await client.QueryAsync("Bootstrap", new BootstrapRequest(), context with { Generation = 9 }, ["index"], Fetch);
        Require(count == 5, "invalidation fences an in-flight cache publication");
        var mutations = 0;
        var outcome = await client.MutateAsync<SettingsUpdateReceipt>((_,_,_) =>
        { mutations++; return Reply(Task.FromException<SettingsUpdateReceipt>(new RpcException(new Status(StatusCode.Unavailable,"")))); });
        Require(mutations == 1 && outcome.Outcome == MutationOutcome.Unknown, "uncertain mutation is not replayed or reported as known failure");
        var l = new Localization(); l.Set("zh-CN");
        Require(l.Number(new NumericValue()) == "—" && l.Number(new NumericValue { Value = 0 }) == "0", "display distinguishes unknown from real zero");
        Require(long.Parse(l.Number(new NumericValue { Value = long.MaxValue }), NumberStyles.AllowThousands, l.Culture) == long.MaxValue, "large token values retain exact display precision");
        l.Set("en-US"); Require(l.FeatureTitle(Feature.Settings) == "Settings", "English navigation labels resolve");
        Require(l.Money(new NumericValue { Value = 1 }) == "US$ 0.000001" && l.Money(new NumericValue { Value = 0 }) == "US$ 0.00", "small positive cost is not formatted as real zero");
        var rejectedMutationCache = false;
        try { await client.QueryAsync("UpdateAPICredential", new UpdateAPICredentialRequest(), context, ["settings"], Fetch); }
        catch (ArgumentException) { rejectedMutationCache = true; }
        Require(rejectedMutationCache, "credential mutations never enter the query cache");
    }
}
