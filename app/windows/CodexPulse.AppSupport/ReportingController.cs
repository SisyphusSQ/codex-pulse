using Codexpulse.Core.V1;
using CodexPulse.CoreClient;
using Grpc.Core;

namespace CodexPulse.AppSupport;

public enum ReportingAction{Idle,Running,Applied,ReadbackConfirmed,ReadbackChanged,CommandAccepted,Unknown,Unavailable}
public sealed class ReportingDraft
{
    public string Endpoint{get;set;}="";
    public bool AllowHttp{get;set;}
    public bool Enabled{get;set;}
    public long IntervalSeconds{get;set;}=60;
    public long HistoryStartAtMs{get;set;}
    public static ReportingDraft From(ReportingStatusResponse value)=>new(){Endpoint=value.Endpoint,AllowHttp=value.AllowHttp,Enabled=value.Enabled,IntervalSeconds=value.IntervalSeconds,HistoryStartAtMs=value.HistoryStartAtMs};
    public bool SamePair(ReportingStatusResponse value)=>Endpoint==value.Endpoint && AllowHttp==value.AllowHttp;
    public bool SameConfiguration(ReportingStatusResponse value)=>Enabled==value.Enabled && IntervalSeconds==value.IntervalSeconds && HistoryStartAtMs==value.HistoryStartAtMs;
}

// Device settings belong to the runtime, not to a page or a Codex Home. The
// one-time code is a method-local request, never a draft, cache or preference.
public sealed class ReportingController(FeatureClient client,Func<QueryContext> context):IDisposable
{
    public QuerySlot<ReportingStatusResponse> Current{get;}=new();
    public ReportingDraft Draft{get;private set;}=new();
    private ReportingStatusResponse? baseline;
    public ReportingAction Action{get;private set;}
    public string? Field{get;private set;}
    public string? Reason{get;private set;}
    public bool Busy=>Volatile.Read(ref mutating)!=0;
    public bool PairDirty=>baseline is not null && !Draft.SamePair(baseline);
    public bool ConfigurationDirty=>baseline is not null && !Draft.SameConfiguration(baseline);
    public bool HistoryLocked=>Current.State.Value is {} value && (value.HasLastAttemptAtMs || value.HasLastSuccessAtMs || value.PendingBatches>0);
    public event Action? Changed;
    private int mutating;
    private bool disposed;
    private Task<ReportingStatusResponse> ReadAsync(CancellationToken cancellation=default)
    {
        var request=new Empty();return client.QueryAsync("ReportingStatus",request,context(),["settings","reporting"],
            (core,deadline,ct)=>core.ReportingStatusAsync(request,deadline:deadline,cancellationToken:ct),cancellation,true);
    }
    public async Task ReloadAsync()
    {
        if(disposed)return;
        await Current.LoadAsync("reporting",ReadAsync);
        if(disposed || Busy || Current.State.Kind!=LoadKind.Ready || Current.State.Value is not {} value)return;
        if(baseline is null)Adopt(value);
        else if(!PairDirty && !ConfigurationDirty)
        {
            var changed=!Draft.SamePair(value) || !Draft.SameConfiguration(value);
            baseline=value.Clone();
            if(changed)Draft=ReportingDraft.From(value);
        }
        Changed?.Invoke();
    }
    private void Adopt(ReportingStatusResponse value){baseline=value.Clone();Draft=ReportingDraft.From(value);}
    public void Reset(){if(!Busy && Current.State.Kind==LoadKind.Ready && Current.State.Value is {} value){Adopt(value);Action=ReportingAction.Idle;Changed?.Invoke();}}
    public void Edited(){if(!Busy)Action=ReportingAction.Idle;Changed?.Invoke();}
    private async Task<ReportingStatusResponse> ReadMutationResultAsync()
    {
        var response=await ReadAsync();
        if(!disposed)await Current.LoadAsync("reporting",_=>Task.FromResult(response));
        return response;
    }
    public async Task PairAsync(string oneTimeCode)
    {
        var before=Current.State.Value?.Clone();
        var keepConfiguration=ConfigurationDirty;
        var desired=Draft;
        var request=new PairReportingRequest{Endpoint=Draft.Endpoint.Trim(),AllowHttp=Draft.AllowHttp,Code=oneTimeCode.Trim()};
        try
        {
            await ExecuteAsync((core,d,ct)=>core.PairReportingAsync(request,deadline:d,cancellationToken:ct),
                (receipt,value,unknown)=>!string.IsNullOrEmpty(value.ClientId) && !value.Enabled && value.AllowHttp==request.AllowHttp && EndpointMatches(request.Endpoint,value.Endpoint) &&
                    (unknown?value.ClientId!=before?.ClientId:receipt?.ClientId==value.ClientId),
                value=>
                {
                    Adopt(value);
                    if(keepConfiguration){Draft.IntervalSeconds=desired.IntervalSeconds;Draft.HistoryStartAtMs=desired.HistoryStartAtMs;}
                });
        }
        finally{request.Code="";}
    }
    public Task ConfigureAsync(bool clearPending=false)
    {
        var current=Current.State.Value??throw new ArgumentException("reporting_status_required");
        var keepPair=PairDirty;var endpoint=Draft.Endpoint;var allowHttp=Draft.AllowHttp;
        var interval=Draft.IntervalSeconds;
        var request=new ConfigureReportingRequest{Enabled=clearPending?false:Draft.Enabled,IntervalSeconds=clearPending?current.IntervalSeconds:interval,HistoryStartAtMs=clearPending?current.HistoryStartAtMs:Draft.HistoryStartAtMs,ClearPending=clearPending};
        if(request.IntervalSeconds is <15 or >3600 || request.HistoryStartAtMs<0)throw new ArgumentException("reporting_configuration_invalid");
        return ExecuteAsync((core,d,ct)=>core.ConfigureReportingAsync(request,deadline:d,cancellationToken:ct),
            (receipt,value,unknown)=>value.ClientId==current.ClientId && value.Endpoint==current.Endpoint && value.Enabled==request.Enabled && value.IntervalSeconds==request.IntervalSeconds && value.HistoryStartAtMs==request.HistoryStartAtMs && (!clearPending || value.PendingBatches==0) && (unknown || receipt?.ClientId==value.ClientId),
            value=>{Adopt(value);if(keepPair){Draft.Endpoint=endpoint;Draft.AllowHttp=allowHttp;}if(clearPending)Draft.IntervalSeconds=interval;});
    }
    public Task SyncAsync(bool full)
    {
        var current=Current.State.Value??throw new ArgumentException("reporting_status_required");
        return ExecuteAsync((core,d,ct)=>full?core.FullSyncReportingAsync(new(),deadline:d,cancellationToken:ct):core.SyncReportingNowAsync(new(),deadline:d,cancellationToken:ct),
            (receipt,value,unknown)=>!unknown && receipt is not null && receipt.ClientId==value.ClientId && receipt.Endpoint==value.Endpoint && value.ClientId==current.ClientId && value.Enabled,_=>{},true);
    }
    private async Task ExecuteAsync(Func<CoreService.CoreServiceClient,DateTime,CancellationToken,AsyncUnaryCall<ReportingStatusResponse>> invoke,
        Func<ReportingStatusResponse?,ReportingStatusResponse,bool,bool> confirm,Action<ReportingStatusResponse> adopt,bool command=false)
    {
        if(disposed || Interlocked.CompareExchange(ref mutating,1,0)!=0)return;
        Action=ReportingAction.Running;Field=Reason=null;Changed?.Invoke();
        try
        {
            var result=await client.MutateAsync(invoke);
            ReportingStatusResponse value;
            try{value=await ReadMutationResultAsync();}
            catch(Exception e)when(e is RpcException or IOException){Action=ReportingAction.Unknown;Reason="readback_unavailable";return;}
            if(disposed)return;
            var unknown=result.Outcome==MutationOutcome.Unknown;
            if(!confirm(result.Receipt,value,unknown)){Action=unknown?ReportingAction.Unknown:ReportingAction.ReadbackChanged;return;}
            adopt(value);
            Action=unknown?ReportingAction.ReadbackConfirmed:command?ReportingAction.CommandAccepted:ReportingAction.Applied;
        }
        catch(RpcException e)
        {
            var detail=CoreConnection.ErrorDetail(e);Field=detail?.HasField==true?detail.Field:null;Reason=detail?.Code??e.StatusCode.ToString();Action=ReportingAction.Unavailable;
            try{await ReadMutationResultAsync();}catch(Exception failure)when(failure is RpcException or IOException){Reason="readback_unavailable";}
        }
        catch(IOException){Action=ReportingAction.Unknown;Reason="connection";try{await ReadMutationResultAsync();}catch(Exception e)when(e is RpcException or IOException){Reason="readback_unavailable";}}
        finally{Interlocked.Exchange(ref mutating,0);if(!disposed)Changed?.Invoke();}
    }
    public static bool EndpointMatches(string left,string right)=>Uri.TryCreate(left,UriKind.Absolute,out var a) && Uri.TryCreate(right,UriKind.Absolute,out var b) &&
        a.AbsolutePath=="/" && b.AbsolutePath=="/" && string.IsNullOrEmpty(a.UserInfo) && string.IsNullOrEmpty(b.UserInfo) && string.IsNullOrEmpty(a.Query) && string.IsNullOrEmpty(a.Fragment) &&
        Uri.Compare(a,b,UriComponents.SchemeAndServer,UriFormat.SafeUnescaped,StringComparison.OrdinalIgnoreCase)==0;
    public static Uri? WebAddress(ReportingStatusResponse value)=>Uri.TryCreate(value.Endpoint,UriKind.Absolute,out var uri) &&
        (uri.Scheme=="https" || uri.Scheme=="http" && value.AllowHttp) && string.IsNullOrEmpty(uri.UserInfo) && uri.AbsolutePath=="/" && string.IsNullOrEmpty(uri.Query) && string.IsNullOrEmpty(uri.Fragment)?uri:null;
    public void Dispose(){disposed=true;Current.Dispose();Draft=new();baseline=null;Changed=null;}
}

public static class ReportingCopy
{
    public static string State(string value,Localization l)=>value switch
    {
        "processing"=>l.Text("中心正在处理，保留待确认批次","Center processing; batches remain unconfirmed"),
        "publication_failed"=>l.Text("中心处理失败，其他同步继续","Center publication failed; other sync continues"),
        "disabled"=>l.Text("已关闭","Disabled"),"ready"=>l.Text("已准备，等待增量同步","Ready; awaiting incremental sync"),
        "partial"=>l.Text("部分来源可用","Some sources available"),"offline"=>l.Text("连接失败，等待有界重试","Offline; awaiting bounded retry"),
        "reconnect_required"=>l.Text("凭证已失效，请重新配对","Credential invalid; pair again"),"protocol_rejected"=>l.Text("协议不兼容，请先更新中心","Protocol rejected; update the center first"),
        "queue_full"=>l.Text("队列已满，保留待发送数据","Queue full; pending data retained"),"source_budget_exceeded"=>l.Text("来源快照超过总预算","Source snapshot exceeds budget"),
        "source_unavailable"=>l.Text("本机来源尚不可用","Local source unavailable"),"storage_unavailable"=>l.Text("本机同步存储不可用","Local sync storage unavailable"),
        _=>l.Text("状态未知","Unknown state")
    };
}
