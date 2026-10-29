using Codexpulse.Core.V1;
using CodexPulse.CoreClient;
using Google.Protobuf;
using Google.Protobuf.Reflection;
using Grpc.Core;

namespace CodexPulse.AppSupport;

public enum SettingsAction { Idle,Saving,Applied,ReconcileRequired,Conflict,Unknown,ReadbackChanged,ReadbackConfirmed,Unavailable }

// Global preferences are distinct from immediate account, Home and platform commands.
public sealed class SettingsEditor(FeatureClient client,Func<QueryContext> context):IDisposable
{
    public QuerySlot<SettingsResponse> Current{get;}=new();
    public UpdateSettingsRequest? Draft{get;private set;}
    private UpdateSettingsRequest? baseline;
    public SettingsAction Action{get;private set;}
    public string? Field{get;private set;}
    public bool Busy=>Volatile.Read(ref saving)!=0;
    public bool Dirty=>Draft is not null && baseline is not null && !Draft.Equals(baseline);
    public event Action? Changed;
    private int saving;
    private bool disposed;
    public void Observe(SettingsResponse response)
    {
        if(disposed)return;
        if(Draft is null || !Dirty && !Busy){baseline=SettingsDraft.From(response.Snapshot);Draft=baseline.Clone();}
        else if(!Busy && baseline?.ExpectedRevision!=response.Snapshot.Revision)Action=SettingsAction.Conflict;
        Changed?.Invoke();
    }
    public async Task ReloadAsync()
    {
        var scope=context();var request=new SettingsRequest();
        await Current.LoadAsync("preferences",ct=>client.QueryAsync("Settings",request,scope,["settings"],(core,deadline,cancel)=>core.SettingsAsync(request,deadline:deadline,cancellationToken:cancel),ct,true));
        if(!disposed && Current.State.Kind==LoadKind.Ready && Current.State.Value is {} response)Observe(response);
    }
    private async Task<SettingsResponse> ReadMutationResultAsync()
    {
        // Page invalidations may replace their query slot while a write is in
        // progress. The command's bounded authority read is not a page request.
        var request=new SettingsRequest();var scope=context();
        var response=await client.QueryAsync("Settings",request,scope,["settings"],(core,deadline,ct)=>core.SettingsAsync(request,deadline:deadline,cancellationToken:ct),force:true);
        if(Current.State.Kind==LoadKind.Ready && Current.State.Value is {} current && ulong.TryParse(current.Snapshot.Revision,out var currentRevision) && ulong.TryParse(response.Snapshot.Revision,out var revision) && currentRevision>revision)return current;
        if(!disposed)await Current.LoadAsync("preferences",_=>Task.FromResult(response));
        return response;
    }
    public void Edited(){if(!Busy && Action!=SettingsAction.Conflict)Action=SettingsAction.Idle;Changed?.Invoke();}
    public void Reset()
    {if(Busy || Current.State.Kind!=LoadKind.Ready || Current.State.Value is not {} value)return;baseline=SettingsDraft.From(value.Snapshot);Draft=baseline.Clone();Action=SettingsAction.Idle;Changed?.Invoke();}
    public void Rebase()
    {
        if(Busy || Draft is null || baseline is null || Current.State.Kind!=LoadKind.Ready || Current.State.Value is not {} latest)return;
        var next=SettingsDraft.From(latest.Snapshot);Merge(baseline,Draft,next);baseline=SettingsDraft.From(latest.Snapshot);Draft=next;Action=SettingsAction.Idle;Changed?.Invoke();
    }
    private static void Merge(IMessage before,IMessage edited,IMessage target)
    {
        foreach(var field in before.Descriptor.Fields.InDeclarationOrder())
        {
            if(field.Name=="expected_revision")continue;
            if(field.IsRepeated)
            {
                if(before is not UpdateSettingsRequest b || edited is not UpdateSettingsRequest e || target is not UpdateSettingsRequest t)continue;
                foreach(var provider in e.Providers)
                {
                    var old=b.Providers.FirstOrDefault(item=>item.Provider==provider.Provider);var current=t.Providers.FirstOrDefault(item=>item.Provider==provider.Provider);
                    if(current is not null && old?.Intent!=provider.Intent)current.Intent=provider.Intent;
                }
            }
            else if(field.FieldType==FieldType.Message)Merge((IMessage)field.Accessor.GetValue(before),(IMessage)field.Accessor.GetValue(edited),(IMessage)field.Accessor.GetValue(target));
            else if(!Equals(field.Accessor.GetValue(before),field.Accessor.GetValue(edited)))field.Accessor.SetValue(target,field.Accessor.GetValue(edited));
        }
    }
    public static bool SameValues(UpdateSettingsRequest left,UpdateSettingsRequest right)
    {
        var a=left.Clone();var b=right.Clone();a.ExpectedRevision=b.ExpectedRevision="";
        var ap=a.Providers.OrderBy(item=>item.Provider,StringComparer.Ordinal).Select(item=>item.Clone()).ToArray();var bp=b.Providers.OrderBy(item=>item.Provider,StringComparer.Ordinal).Select(item=>item.Clone()).ToArray();
        a.Providers.Clear();b.Providers.Clear();a.Providers.Add(ap);b.Providers.Add(bp);return a.Equals(b);
    }
    public async Task SaveAsync()
    {
        if(disposed || Draft is null || Interlocked.CompareExchange(ref saving,1,0)!=0)return;
        var sent=Draft.Clone();Action=SettingsAction.Saving;Field=null;Changed?.Invoke();
        try
        {
            var result=await client.MutateAsync((core,deadline,ct)=>core.UpdateSettingsAsync(sent,deadline:deadline,cancellationToken:ct));
            SettingsResponse response;
            try{response=await ReadMutationResultAsync();}
            catch(Exception error)when(error is RpcException or IOException){Action=SettingsAction.Unknown;return;}
            if(disposed)return;
            if(result.Outcome!=MutationOutcome.Unknown && result.Receipt?.Result is not("applied" or "applied_reconcile_required")){Action=SettingsAction.Unknown;return;}
            if(!SameValues(sent,SettingsDraft.From(response.Snapshot))){Action=response.Snapshot.Revision!=sent.ExpectedRevision?SettingsAction.Conflict:SettingsAction.ReadbackChanged;return;}
            baseline=SettingsDraft.From(response.Snapshot);Draft=baseline.Clone();Action=result.Outcome==MutationOutcome.Unknown?SettingsAction.ReadbackConfirmed:result.Receipt?.Result=="applied_reconcile_required"?SettingsAction.ReconcileRequired:SettingsAction.Applied;
        }
        catch(RpcException error)
        {
            var detail=CoreConnection.ErrorDetail(error);Field=detail?.HasField==true?detail.Field:null;
            Action=error.StatusCode==StatusCode.Aborted || detail?.Code=="conflict"?SettingsAction.Conflict:SettingsAction.Unavailable;
            await ReloadAsync();
        }
        catch(IOException){Action=SettingsAction.Unknown;await ReloadAsync();}
        finally{Interlocked.Exchange(ref saving,0);if(!disposed)Changed?.Invoke();}
    }
    public void Dispose(){disposed=true;Current.Dispose();Draft=null;baseline=null;Changed=null;}
}

public sealed class HomeSwitchDraft
{
    public string Target{get;set;}="";
    public string Strategy{get;set;}="independent_database";
    public HomeSwitchPlanReceipt? Plan{get;private set;}
    private string? target,strategy,revision;
    private int busy;
    public bool Busy=>Volatile.Read(ref busy)!=0;
    public string Status{get;private set;}="idle";
    public event Action? Changed;
    public bool Begin(){if(Interlocked.CompareExchange(ref busy,1,0)!=0)return false;Status="running";Changed?.Invoke();return true;}
    public void Finish(string status){Status=status;Interlocked.Exchange(ref busy,0);Changed?.Invoke();}
    public void Planned(HomeSwitchPlanReceipt value,string currentRevision)
    {if(value.Strategy!=Strategy || string.IsNullOrEmpty(value.TargetGeneration))throw new IOException("home_plan_invalid");Plan=value.Clone();target=Target;strategy=Strategy;revision=currentRevision;}
    public bool CanConfirm(string currentRevision)=>Plan is not null && Target==target && Strategy==strategy && currentRevision==revision;
    public void Clear()=>Plan=null;
}

public static class RuntimePresentation
{
    public static bool AllowedAction(string action)=>action is "pause_backfill" or "pause_all" or "resume" or "reconcile";
    public static string? RecoveryRoute(string kind)=>kind switch{"retry"=>"reload","check_source"=>"reconcile","repair_store"=>"analyze","choose_home" or "grant_permission" or "free_space"=>"settings",_=>null};
    public static string Phase(string phase,Localization l)=>phase switch
    {"discover"=>l.Text("发现来源","Discovering sources"),"fast_bootstrap"=>l.Text("快速索引","Fast indexing"),"history_backfill"=>l.Text("历史补齐","History backfill"),"reconcile"=>l.Text("来源对账","Reconciling sources"),"live"=>l.Text("增量追平","Incremental indexing"),"maintenance"=>l.Text("维护","Maintenance"),_=>l.Text("未知任务","Unknown task")};
}
