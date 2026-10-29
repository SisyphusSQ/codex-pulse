using System.Security.Cryptography;
using Codexpulse.Core.V1;
using CodexPulse.CoreClient;
using Google.Protobuf;
using Grpc.Core;

namespace CodexPulse.AppSupport;

public enum ApiCredentialAction { Idle, Saving, Applied, ReadbackConfirmed, Unknown, RestartRequired, Unavailable }

// Runtime-scoped state contains configuration flags and business responses only.
// Secrets are method-local and never enter a draft, query cache or preference.
public sealed class ApiSubscriptionsController : IDisposable
{
    private readonly FeatureClient client;
    private readonly Func<QueryContext> context;
    private readonly Func<Task<bool>> restart;
    private int mutating;
    private long epoch;
    private bool disposed, restartRequired;
    public QuerySlot<APICredentialStatusResponse> Credentials { get; } = new();
    public QuerySlot<APISubscriptionsCurrentResponse> Current { get; } = new();
    public ApiCredentialAction Action { get; private set; }
    public bool Busy => Volatile.Read(ref mutating) != 0;
    public event Action? Changed;
    public ApiSubscriptionsController(FeatureClient client, Func<QueryContext> context, Func<Task<bool>> restart)
    {
        this.client=client;this.context=context;this.restart=restart;
        Credentials.Changed+=Notify;Current.Changed+=Notify;
    }
    private void Notify(){if(!disposed)Changed?.Invoke();}
    private QueryContext Scope()=>context() with { Provider="", Generation=epoch };
    private Task<APICredentialStatusResponse> ReadStatus()
    {
        var request=new APICredentialStatusRequest();
        return client.QueryAsync("APICredentialStatus",request,Scope(),["api_credentials"],
            (core,d,ct)=>core.APICredentialStatusAsync(request,deadline:d,cancellationToken:ct),force:true);
    }
    public Task ReloadStatusAsync()=>Busy || disposed?Task.CompletedTask:Credentials.LoadAsync(epoch,_=>ReadStatus());
    public async Task ReloadAsync()
    {
        if(Busy || disposed || restartRequired)return;
        await Task.WhenAll(ReloadStatusAsync(),LoadData());
    }
    private Task LoadData()
    {
        var request=new APISubscriptionsCurrentRequest{EvaluatedAtMs=DateTimeOffset.Now.ToUnixTimeMilliseconds()};
        return Current.LoadAsync(epoch,ct=>client.QueryAsync("APISubscriptionsCurrent",request,Scope(),["api_credentials","api_subscriptions"],
            (core,d,c)=>core.APISubscriptionsCurrentAsync(request,deadline:d,cancellationToken:c),ct,true),ApiSubscriptionPresentation.Kind);
    }
    public void ResetConnection(){epoch++;client.Invalidate("api_credentials");Current.Clear();Credentials.Clear();restartRequired=false;if(Action==ApiCredentialAction.RestartRequired)Action=ApiCredentialAction.Unknown;}
    public async Task UpdateAsync(string service,byte[] secret,bool delete=false)
    {
        UpdateAPICredentialRequest? request=null;
        try
        {
            if(service is not ("deepseek" or "opencode_go") || (!delete && secret.Length is <1 or >4096) || (delete && secret.Length!=0))throw new ArgumentException("api_credential_invalid");
            if(disposed || Interlocked.CompareExchange(ref mutating,1,0)!=0)return;
            Action=ApiCredentialAction.Saving;epoch++;Current.Clear();Credentials.Clear();client.Invalidate("api_credentials");Notify();
            request=new(){Service=service};if(delete)request.Delete=true;else request.Secret=ByteString.CopyFrom(secret);
            CryptographicOperations.ZeroMemory(secret);
            restartRequired=true;
            MutationResult<APICredentialStatusResponse> result;
            try { result=await client.MutateAsync((core,d,ct)=>core.UpdateAPICredentialAsync(request,deadline:d,cancellationToken:ct)); }
            finally { request.ClearAction(); }
            if(disposed)return;
            restartRequired=true;
            if(!await restart()){Action=ApiCredentialAction.RestartRequired;return;}
            if(disposed)return;
            restartRequired=false;
            var status=await ReadStatus();
            if(disposed)return;
            await Credentials.LoadAsync(epoch,_=>Task.FromResult(status));
            var configured=Configured(service,status);
            Action=result.Outcome==MutationOutcome.Unknown
                ?delete && !configured?ApiCredentialAction.ReadbackConfirmed:ApiCredentialAction.Unknown
                :configured!=delete?ApiCredentialAction.Applied:ApiCredentialAction.Unknown;
            await LoadData();
        }
        catch(RpcException){restartRequired=false;if(!disposed)Action=ApiCredentialAction.Unavailable;}
        catch(IOException){if(!disposed)Action=ApiCredentialAction.RestartRequired;}
        finally
        {
            request?.ClearAction();CryptographicOperations.ZeroMemory(secret);
            if(request is not null){Interlocked.Exchange(ref mutating,0);Notify();}
        }
    }
    public static bool Configured(string service,APICredentialStatusResponse value)=>service=="deepseek"?value.DeepSeekConfigured:service=="opencode_go" && value.OpenCodeGoConfigured;
    public void Cancel(){if(!Busy){Current.Cancel();Credentials.Cancel();}}
    public void Dispose(){disposed=true;Credentials.Dispose();Current.Dispose();Changed=null;}
}
