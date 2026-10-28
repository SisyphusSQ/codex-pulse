namespace CodexPulse.AppSupport;

public enum LoadKind { Idle, Loading, Ready, Empty, Partial, Stale, Unavailable, Cancelled, RecoveryBlocked }
public sealed record LoadState<T>(LoadKind Kind, T? Value = default, string? Reason = null) where T : class;

// Each view owns a slot. A replacement request invalidates the old completion even
// when a transport ignores cancellation; previous values survive only in the same context.
public sealed class QuerySlot<T> : IDisposable where T : class
{
    private readonly object gate = new();
    private CancellationTokenSource? request;
    private long sequence;
    private object? context;
    public LoadState<T> State { get; private set; } = new(LoadKind.Idle);
    public event Action? Changed;

    public async Task LoadAsync(object key, Func<CancellationToken, Task<T>> query,
        Func<T, LoadKind>? classify = null, CancellationToken cancellation = default)
    {
        CancellationTokenSource current;
        long version;
        T? previous;
        lock (gate)
        {
            request?.Cancel();
            current = CancellationTokenSource.CreateLinkedTokenSource(cancellation);
            request = current;
            version = ++sequence;
            previous = Equals(context, key) ? State.Value : null;
            context = key;
            State = new(LoadKind.Loading, previous);
        }
        Changed?.Invoke();
        try
        {
            var value = await query(current.Token).ConfigureAwait(false);
            current.Token.ThrowIfCancellationRequested();
            Publish(version, new(classify?.Invoke(value) ?? LoadKind.Ready, value));
        }
        catch (OperationCanceledException) { Publish(version, new(LoadKind.Cancelled, previous)); }
        catch (Grpc.Core.RpcException error)
        {
            var reason = CodexPulse.CoreClient.CoreConnection.ErrorDetail(error)?.Code ?? error.StatusCode.ToString();
            if (error.StatusCode == Grpc.Core.StatusCode.Cancelled) Publish(version, new(LoadKind.Cancelled, previous));
            else Publish(version, new(previous is null ? LoadKind.Unavailable : LoadKind.Stale, previous, reason));
        }
        catch (IOException) { Publish(version, new(previous is null ? LoadKind.Unavailable : LoadKind.Stale, previous, "connection")); }
        finally
        {
            lock (gate) { if (request == current) request = null; }
            current.Dispose();
        }
    }

    private void Publish(long version, LoadState<T> next)
    {
        lock (gate) { if (version != sequence) return; State = next; }
        Changed?.Invoke();
    }
    public void Cancel() { lock (gate) request?.Cancel(); }
    public void Clear(bool recovery = false)
    {
        lock (gate) { request?.Cancel(); request = null; sequence++; context = null; State = new(recovery ? LoadKind.RecoveryBlocked : LoadKind.Idle); }
        Changed?.Invoke();
    }
    public void Dispose() => Clear();
}
