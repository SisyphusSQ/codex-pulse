using System.Security.Cryptography;
using System.Text;
using Codexpulse.Core.V1;
using Grpc.Core;

namespace CodexPulse.CoreClient;

public enum CoreConnectionState { Starting, Connected, Recovery, Disconnected, Stopped, Uncertain }

public sealed class HelperSession : IAsyncDisposable
{
    private readonly NativeHelper process;
    private readonly CancellationTokenSource lifetime = new();
    public CoreConnection Connection { get; }
    public uint ProcessId => process.Id;
    public string PipeName { get; }
    public CoreConnectionState State { get; private set; } = CoreConnectionState.Starting;
    public BootstrapResponse Bootstrap { get; private set; } = new();
    public Task<int> Exit => process.Exit;
    public int? ShutdownExitCode{get;private set;}
    public string ShutdownReason{get;private set;}="not_observed";
    public event Action<CoreConnectionState>? StateChanged;

    private HelperSession(NativeHelper process, string pipeName, string token)
    {
        this.process = process;
        PipeName = pipeName;
        Connection = new CoreConnection(pipeName, process.Id, token);
    }

    public static async Task<HelperSession> StartAsync(string executable, string runtime, string home, string version,
        CancellationToken cancellation = default)
    {
        PrivateRuntime.Ensure(runtime);
        var pipeName = "codex-pulse-" + Guid.NewGuid().ToString("N");
        var token = Convert.ToHexStringLower(RandomNumberGenerator.GetBytes(32));
        var process = NativeHelper.Start(executable, pipeName, runtime, home);
        var session = new HelperSession(process, pipeName, token);
        var frame = Encoding.ASCII.GetBytes(token + "\n");
        try
        {
            await process.AuthenticateAsync(frame, cancellation).ConfigureAwait(false);
            await session.Connection.HandshakeAsync(version, cancellation).ConfigureAwait(false);
            await session.RefreshBootstrapAsync(cancellation).ConfigureAwait(false);
            _ = session.ObserveExitAsync();
            return session;
        }
        catch
        {
            process.CloseParentPipe();
            try { await process.Exit.WaitAsync(TimeSpan.FromSeconds(20), CancellationToken.None).ConfigureAwait(false); }
            catch (TimeoutException) { process.Terminate(); await process.Exit.ConfigureAwait(false); }
            session.Connection.Dispose();
            process.Dispose();
            session.lifetime.Dispose();
            throw;
        }
        finally { CryptographicOperations.ZeroMemory(frame); }
    }

    public async Task RefreshBootstrapAsync(CancellationToken cancellation = default)
    {
        Bootstrap = await Connection.Raw.BootstrapAsync(new BootstrapRequest(), deadline: DateTime.UtcNow.AddSeconds(30),
            cancellationToken: cancellation).ResponseAsync.ConfigureAwait(false);
        SetState(Bootstrap.Mode == "recovery" ? CoreConnectionState.Recovery : CoreConnectionState.Connected);
    }

    private async Task ObserveExitAsync()
    {
        try { await process.Exit.ConfigureAwait(false); }
        catch (IOException) { SetState(CoreConnectionState.Uncertain); return; }
        if (!lifetime.IsCancellationRequested) SetState(CoreConnectionState.Disconnected);
    }

    // A fresh subscription always invalidates cached queries. The bounded broker may coalesce
    // events; sequence is global across domains, so a gap requires authoritative queries.
    public async Task WatchAsync(Func<QueryInvalidationEvent?, Task> invalidate, CancellationToken cancellation)
    {
        using var linked = CancellationTokenSource.CreateLinkedTokenSource(cancellation, lifetime.Token);
        ulong sequence = 0;
        var failures = 0;
        while (!linked.IsCancellationRequested && !process.Exit.IsCompleted)
        {
            try
            {
                using var stream = Connection.Raw.SubscribeInvalidations(new SubscribeInvalidationsRequest { AfterSequence = sequence },
                    cancellationToken: linked.Token);
                var headers = await stream.ResponseHeadersAsync.ConfigureAwait(false);
                if (headers.GetValue("codex-pulse-stream-ready") != "1") throw new IOException("invalidation_not_ready");
                await invalidate(null).ConfigureAwait(false);
                while (await stream.ResponseStream.MoveNext(linked.Token).ConfigureAwait(false))
                {
                    var change = stream.ResponseStream.Current;
                    if (change.Version != "query-invalidation-v4") throw new IOException("invalidation_contract_mismatch");
                    if (sequence != 0 && change.Sequence != sequence + 1) await invalidate(null).ConfigureAwait(false);
                    sequence = change.Sequence;
                    failures = 0;
                    await invalidate(change).ConfigureAwait(false);
                }
                throw new RpcException(new Status(StatusCode.Unavailable, "invalidation_stream_ended"));
            }
            catch (RpcException error) when (error.StatusCode == StatusCode.Cancelled && linked.IsCancellationRequested) { break; }
            catch (RpcException error) when (error.StatusCode is StatusCode.Unavailable or StatusCode.DeadlineExceeded)
            {
                if (++failures > 5) { SetState(CoreConnectionState.Disconnected); throw; }
                await Task.Delay(TimeSpan.FromMilliseconds(200 * (1 << (failures - 1))), linked.Token).ConfigureAwait(false);
            }
            catch (RpcException) { SetState(CoreConnectionState.Disconnected); throw; }
        }
    }

    public async Task<bool> StopAsync(string reason = "client_exit")
    {
        lifetime.Cancel();
        try
        {
            await Connection.Raw.ShutdownAsync(new ShutdownRequest { Reason = reason },
                deadline: DateTime.UtcNow.AddSeconds(3)).ResponseAsync.ConfigureAwait(false);
        }
        catch (RpcException) { /* Exit status after parent EOF determines whether draining completed. */ }
        process.CloseParentPipe();
        try
        {
            var code = await process.Exit.WaitAsync(TimeSpan.FromSeconds(35)).ConfigureAwait(false);
            ShutdownExitCode=code;
            var empty=await process.DrainJobAsync().ConfigureAwait(false);
            ShutdownReason=code!=0?"helper_exit_failed":!empty?"descendants_forced":"clean";
            SetState(code == 0 && empty ? CoreConnectionState.Stopped : CoreConnectionState.Uncertain);
            return code == 0 && empty;
        }
        catch (TimeoutException)
        {
            process.Terminate();
            await process.Exit.ConfigureAwait(false);
            SetState(CoreConnectionState.Uncertain);
            ShutdownReason="helper_exit_timeout";
            return false;
        }
        catch(IOException){ShutdownReason="job_drain_uncertain";SetState(CoreConnectionState.Uncertain);return false;}
    }

    public void CloseParentPipe() => process.CloseParentPipe();
    private void SetState(CoreConnectionState state) { State = state; StateChanged?.Invoke(state); }
    public async ValueTask DisposeAsync()
    {
        try
        {
            if (!process.Exit.IsCompleted) await StopAsync().ConfigureAwait(false);
            await process.DrainJobAsync().ConfigureAwait(false);
        }
        finally{lifetime.Dispose();Connection.Dispose();process.Dispose();}
    }
}
