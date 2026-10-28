using Codexpulse.Core.V1;
using CodexPulse.CoreClient;
using Grpc.Core;

internal static class TransportTests
{
    private static void Require(bool value, string name)
    {
        if (!value) throw new InvalidOperationException(name);
        Console.WriteLine("PASS " + name);
    }

    private static async Task<RpcException> Reject(Func<Task> action, StatusCode code, string name)
    {
        try { await action(); }
        catch (RpcException error) when (error.StatusCode == code) { Require(true, name); return error; }
        throw new InvalidOperationException(name);
    }

    public static async Task Run(string executable, string root)
    {
        var normal = Path.Combine(root, "normal");
        var home = Path.Combine(normal, "synthetic-home");
        await using (var session = await HelperSession.StartAsync(executable, normal, home, "dev"))
        {
            var client = session.Connection.Raw;
            Require(session.Bootstrap.Platform == "windows" && session.State == CoreConnectionState.Connected, "real C# to Go named pipe Bootstrap");
            var contracts = await client.ContractsAsync(new ContractsRequest());
            Require(contracts.Version == CoreConnection.ContractVersion && contracts.Methods.Count > 40, "generated full CoreService contract");
            using (var bad = new CoreConnection(session.PipeName, session.ProcessId, new string('x', 64)))
            {
                await Reject(async () => await bad.Raw.BootstrapAsync(new BootstrapRequest()), StatusCode.Unauthenticated, "wrong token rejected for unary");
                await Reject(async () =>
                {
                    using var stream = bad.Raw.SubscribeInvalidations(new SubscribeInvalidationsRequest());
                    await stream.ResponseStream.MoveNext(CancellationToken.None);
                }, StatusCode.Unauthenticated, "wrong token rejected for stream");
            }
            using (var wrongProcess = new CoreConnection(session.PipeName, session.ProcessId + 1, new string('x', 64)))
            {
                await Reject(async () => await wrongProcess.Raw.BootstrapAsync(new BootstrapRequest(), deadline: DateTime.UtcNow.AddSeconds(2)),
                    StatusCode.Unavailable, "unexpected pipe server process rejected");
            }
            await Reject(async () => await client.HandshakeAsync(new HandshakeRequest { ClientName = "transport-test", ContractVersion = "core-rpc-v0" }),
                StatusCode.FailedPrecondition, "incompatible contract rejected");
            try { await session.Connection.HandshakeAsync("different-bundle-version"); throw new InvalidOperationException("version rejection"); }
            catch (IOException) { Require(true, "incompatible App Helper bundle rejected"); }
            var typed = await Reject(async () => await client.UpdateSettingsAsync(new UpdateSettingsRequest()), StatusCode.InvalidArgument, "invalid mutation rejected");
            Require(CoreConnection.ErrorDetail(typed) is { Code: "validation" }, "typed Google RPC error detail preserved");
            await Reject(async () => await client.BootstrapAsync(new BootstrapRequest(), deadline: DateTime.UtcNow.AddSeconds(-1)),
                StatusCode.DeadlineExceeded, "unary deadline retained");
            Require(CoreConnection.MutationOutcomeMayBeUnknown(new RpcException(new Status(StatusCode.DeadlineExceeded,""))), "timed out mutation requires authoritative readback");
            using (var cancel = new CancellationTokenSource())
            using (var stream = client.SubscribeInvalidations(new SubscribeInvalidationsRequest(), cancellationToken: cancel.Token))
            {
                var ready = await stream.ResponseHeadersAsync;
                Require(ready.GetValue("codex-pulse-stream-ready") == "1", "server stream ready before mutation");
                var settings = await client.SettingsAsync(new SettingsRequest());
                Require(settings.Snapshot.Providers.All(p => p.Intent == ProviderIntent.Disabled), "synthetic fixture never enables real providers");
                var draft = SettingsDraft.From(settings.Snapshot);
                draft.Ui.Locale = settings.Snapshot.Ui.Locale == "en-US" ? "zh-CN" : "en-US";
                var receipt = await client.UpdateSettingsAsync(draft);
                Require(receipt.Revision != settings.Snapshot.Revision, "mutation receipt advances authority revision");
                using var deadline = new CancellationTokenSource(TimeSpan.FromSeconds(5));
                Require(await stream.ResponseStream.MoveNext(deadline.Token) && stream.ResponseStream.Current.Sequence > 0 &&
                    stream.ResponseStream.Current.Version == "query-invalidation-v4", "actual invalidation arrives across processes");
                var readback = await client.SettingsAsync(new SettingsRequest());
                Require(readback.Snapshot.Revision == receipt.Revision && readback.Snapshot.Ui.Locale == draft.Ui.Locale, "mutation authority readback");
                cancel.Cancel();
                await Reject(async () => { while (await stream.ResponseStream.MoveNext(CancellationToken.None)) { } }, StatusCode.Cancelled, "stream cancellation reaches server");
            }
            var large = await client.HandshakeAsync(new HandshakeRequest { ContractVersion = CoreConnection.ContractVersion, ClientName = new string('a', 8 * 1024 * 1024) });
            Require(large.Transport == "grpc+namedpipe", "8 MiB request spans pipe buffers");
            await Reject(async () => await client.HandshakeAsync(new HandshakeRequest { ContractVersion = CoreConnection.ContractVersion, ClientName = new string('a', 17 * 1024 * 1024) }),
                StatusCode.ResourceExhausted, "16 MiB message limit retained");
            session.CloseParentPipe();
            Require(await session.Exit.WaitAsync(TimeSpan.FromSeconds(35)) == 0, "parent pipe EOF drains actual Helper");
        }
        await using (var restarted = await HelperSession.StartAsync(executable, normal, home, "dev"))
        {
            Require(restarted.State == CoreConnectionState.Connected, "new launch reconnects after previous Helper exit");
            Require(await restarted.StopAsync(), "authenticated shutdown drains database and exits zero");
        }
        var recovery = Path.Combine(root, "recovery");
        await using (var session = await HelperSession.StartAsync(executable, recovery, Path.Combine(recovery,"synthetic-home"), "dev"))
        {
            Require(session.State == CoreConnectionState.Recovery && session.Bootstrap.Recovery is not null, "future schema enters real recovery Bootstrap");
            var state = await session.Connection.Raw.MigrationRecoveryStateAsync(new MigrationRecoveryStateRequest());
            Require(!string.IsNullOrEmpty(state.Code), "recovery RPC available without normal application services");
            await Reject(async () => await session.Connection.Raw.SettingsAsync(new SettingsRequest()), StatusCode.FailedPrecondition, "normal queries unavailable during recovery");
            Require(await session.StopAsync(), "recovery Helper clean shutdown");
        }
    }

    public static async Task WirePeer(string executable, string runtime)
    {
        await using var session = await HelperSession.StartAsync(executable,runtime,Path.Combine(runtime,"synthetic-home"),"dev");
        var large = await session.Connection.Raw.ListSessionsAsync(new ListSessionsRequest());
        Require(large.Items.Single().SessionId.Length == 8 * 1024 * 1024, "8 MiB response arrives across real pipe");
        Require(large.Meta.Status == "partial" && large.MatchedCount.HasValue && large.MatchedCount.Value == 0 &&
            !large.MatchedTotals.TotalTokens.HasValue && large.MatchedTotals.TotalTokens.HasUnknownReason,
            "Go to C# partial zero and unknown preserved");
        await Reject(async () => await session.Connection.Raw.ListSessionsAsync(new ListSessionsRequest { Query = new QueryRequest { Page = new PageRequest { Limit = 17 } } }),
            StatusCode.ResourceExhausted, "oversized Go response rejected at 16 MiB");
        var ready = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var first = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var release = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var gap = new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);
        var resets = 0;
        var events = 0;
        using var cancellation = new CancellationTokenSource(TimeSpan.FromSeconds(10));
        var watch = session.WatchAsync(async change =>
        {
            if (change is null)
            {
                if (Interlocked.Increment(ref resets) == 1) ready.TrySetResult(); else gap.TrySetResult();
            }
            else if (Interlocked.Increment(ref events) == 1) { first.TrySetResult(); await release.Task; }
        }, cancellation.Token);
        await ready.Task.WaitAsync(cancellation.Token);
        await session.Connection.Raw.NotifyLifecycleAsync(new LifecycleNotificationRequest { Event = "application_did_become_active" });
        await first.Task.WaitAsync(cancellation.Token);
        await session.Connection.Raw.NotifyLifecycleAsync(new LifecycleNotificationRequest { Event = "application_did_become_active" });
        release.TrySetResult();
        await gap.Task.WaitAsync(cancellation.Token);
        Require(resets >= 2, "bounded stream sequence gap triggers authoritative invalidation");
        cancellation.Cancel();
        try { await watch; } catch (OperationCanceledException) { }
        Require(await session.StopAsync(), "contract peer shutdown");
    }
}
