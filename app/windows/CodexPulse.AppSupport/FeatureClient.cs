using System.Security.Cryptography;
using Codexpulse.Core.V1;
using CodexPulse.CoreClient;
using Google.Protobuf;
using Grpc.Core;

namespace CodexPulse.AppSupport;

public enum MutationOutcome { Applied, Unknown }
public sealed record MutationResult<T>(MutationOutcome Outcome, T? Receipt) where T : class;

public sealed class FeatureClient(Func<CoreService.CoreServiceClient> connection)
{
    private sealed record Entry(IMessage Value, int Bytes, long Order, string[] Domains);
    private sealed record CacheKey(string Method, QueryContext Context, string RequestHash);
    private readonly object gate = new();
    private readonly Dictionary<CacheKey, Entry> cache = [];
    private long cacheVersion, order;
    private int bytes;
    private static readonly HashSet<string> Commands = CoreService.Descriptor.Methods
        .Where(method => method.Name.StartsWith("Update") || method.Name.StartsWith("Create") || method.Name.StartsWith("Delete") ||
            method.Name.StartsWith("Link") || method.Name.StartsWith("Unlink") || method.Name.StartsWith("Clear") ||
            method.Name.StartsWith("Request") || method.Name.StartsWith("Run") || method.Name.StartsWith("Confirm") ||
            method.Name.StartsWith("Plan") || method.Name.StartsWith("Analyze") || method.Name.StartsWith("Recover") || method.Name.StartsWith("MigrationRecovery") ||
            method.Name is "PairReporting" or "ConfigureReporting" or "SyncReportingNow" or "FullSyncReporting" or "NotifyLifecycle" or "Shutdown")
        .Select(method => method.Name).ToHashSet();

    public async Task<T> QueryAsync<T>(string method, IMessage request, QueryContext context, string[] domains,
        Func<CoreService.CoreServiceClient, DateTime, CancellationToken, AsyncUnaryCall<T>> invoke,
        CancellationToken cancellation = default, bool force = false) where T : class, IMessage<T>
    {
        if (Commands.Contains(method)) throw new ArgumentException("mutation_cannot_be_cached");
        cancellation.ThrowIfCancellationRequested();
        var key = new CacheKey(method, context, Convert.ToHexString(SHA256.HashData(request.ToByteArray())));
        long version;
        lock (gate)
        {
            if (!force && cache.TryGetValue(key, out var cached)) return ((T)cached.Value).Clone();
            version = cacheVersion;
        }
        using var call = invoke(connection(), DateTime.UtcNow.AddSeconds(30), cancellation);
        var value = await call.ResponseAsync.ConfigureAwait(false);
        cancellation.ThrowIfCancellationRequested();
        var size = value.CalculateSize();
        lock (gate)
        {
            if (version == cacheVersion && size <= 32 * 1024 * 1024)
            {
                if (cache.Remove(key, out var old)) bytes -= old.Bytes;
                while (cache.Count > 0 && (cache.Count >= 64 || bytes + size > 32 * 1024 * 1024))
                {
                    var oldest = cache.MinBy(entry => entry.Value.Order);
                    bytes -= oldest.Value.Bytes; cache.Remove(oldest.Key);
                }
                cache[key] = new(value.Clone(), size, ++order, domains.ToArray()); bytes += size;
            }
        }
        return value;
    }

    public void Invalidate(string? domain = null)
    {
        lock (gate)
        {
            cacheVersion++;
            foreach (var key in cache.Where(entry => domain is null || entry.Value.Domains.Contains(domain)).Select(entry => entry.Key).ToArray())
            { bytes -= cache[key].Bytes; cache.Remove(key); }
        }
    }

    public async Task<MutationResult<T>> MutateAsync<T>(Func<CoreService.CoreServiceClient, DateTime, CancellationToken, AsyncUnaryCall<T>> invoke,
        CancellationToken cancellation = default) where T : class
    {
        try
        {
            using var call = invoke(connection(), DateTime.UtcNow.AddSeconds(30), cancellation);
            var receipt = await call.ResponseAsync.ConfigureAwait(false);
            Invalidate();
            return new(MutationOutcome.Applied, receipt);
        }
        catch (RpcException error) when (CoreConnection.MutationOutcomeMayBeUnknown(error))
        {
            Invalidate();
            return new(MutationOutcome.Unknown, null);
        }
    }
}
