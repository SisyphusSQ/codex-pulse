using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Grpc.Core;

internal static class SettingsTests
{
    private static void Require(bool value,string name){if(!value)throw new InvalidOperationException(name);Console.WriteLine("PASS "+name);}
    private static AsyncUnaryCall<T> Reply<T>(Task<T> task) where T:class=>new(task,Task.FromResult(new Metadata()),()=>Status.DefaultSuccess,()=>new Metadata(),()=>{});
    private sealed class Peer:CoreService.CoreServiceClient
    {
        public SettingsResponse Value=new(){Snapshot=new(){Revision="1",Home=new(){Configured=true,Generation="1",SwitchStatus="stable"},Online=new(){QuotaEnabled=true},Refresh=new(){QuotaIntervalSeconds=300,ResetCreditsIntervalSeconds=1800,ReconcileIntervalSeconds=1800,JsonlDebounceMilliseconds=3000},Updates=new(){Channel="stable",CheckIntervalSeconds=3600},Ui=new(){Locale="en-US",LaunchBehavior="main_window",OverviewRange="quota_week"},CodexAccounts=new(){RetainQuotaHistory=true},Providers={new SettingsProviderSnapshot{Provider="codex",Intent=ProviderIntent.Auto}}}};
        public Func<UpdateSettingsRequest,SettingsUpdateReceipt> Update=_=>new(){Result="applied"};public int Writes;
        public Func<Task<SettingsResponse>>? Read;
        public override AsyncUnaryCall<SettingsResponse> SettingsAsync(SettingsRequest request,CallOptions options)=>Reply(Read?.Invoke()??Task.FromResult(Value.Clone()));
        public override AsyncUnaryCall<SettingsUpdateReceipt> UpdateSettingsAsync(UpdateSettingsRequest request,CallOptions options)
        {Writes++;try{return Reply(Task.FromResult(Update(request)));}catch(RpcException error){return Reply(Task.FromException<SettingsUpdateReceipt>(error));}}
    }
    public static async Task Run()
    {
        var peer=new Peer();var context=new QueryContext("codex",new(2026,10,28),new(2026,10,29),"Asia/Hong_Kong",1);
        using var editor=new SettingsEditor(new(()=>peer),()=>context);await editor.ReloadAsync();editor.Draft!.Refresh.QuotaIntervalSeconds=120;editor.Edited();
        peer.Value.Snapshot.Revision="2";peer.Value.Snapshot.Ui.Locale="zh-CN";peer.Value.Snapshot.CodexAccounts.RetainQuotaHistory=false;
        await editor.ReloadAsync();Require(editor.Draft.ExpectedRevision=="1" && editor.Draft.Refresh.QuotaIntervalSeconds==120,"settings refresh retains dirty values and the original CAS revision");
        editor.Rebase();Require(editor.Draft!.ExpectedRevision=="2" && editor.Draft.Refresh.QuotaIntervalSeconds==120 && editor.Draft.Ui.Locale=="zh-CN" && !editor.Draft.CodexAccounts.RetainQuotaHistory,"three-way settings rebase preserves edited fields and adopts untouched independent changes");
        peer.Update=_=>throw new RpcException(new(StatusCode.Aborted,"conflict"));await editor.SaveAsync();
        Require(editor.Action==SettingsAction.Conflict && editor.Draft.Refresh.QuotaIntervalSeconds==120,"settings conflict is not replayed and keeps user input");
        peer.Update=_=>new(){Result="applied",Revision="3"};await editor.SaveAsync();Require(editor.Action==SettingsAction.ReadbackChanged && editor.Dirty,"settings applied receipt without matching snapshot is not success");
        peer.Update=request=>{peer.Value.Snapshot.Refresh=request.Refresh is {} refresh?new SettingsRefreshSnapshot{QuotaIntervalSeconds=refresh.QuotaIntervalSeconds,ResetCreditsIntervalSeconds=refresh.ResetCreditsIntervalSeconds,ReconcileIntervalSeconds=refresh.ReconcileIntervalSeconds,JsonlDebounceMilliseconds=refresh.JsonlDebounceMilliseconds}:null;peer.Value.Snapshot.Revision="3";return new(){Result="applied",Revision="3"};};
        await editor.SaveAsync();Require(editor.Action==SettingsAction.Applied && !editor.Dirty,"settings success requires authoritative values and resets the draft baseline");
        editor.Draft!.Ui.OverviewRange="today";editor.Edited();
        peer.Update=request=>{peer.Value.Snapshot.Ui.OverviewRange=request.Ui.OverviewRange;peer.Value.Snapshot.Revision="4";throw new RpcException(new(StatusCode.Unavailable,"lost"));};await editor.SaveAsync();
        Require(editor.Action==SettingsAction.ReadbackConfirmed && peer.Writes==4 && !editor.Dirty,"lost settings receipt uses readback without a second mutation");
        peer.Update=_=>new();await editor.SaveAsync();Require(editor.Action==SettingsAction.Unknown,"unrecognized settings receipt is never labeled applied");
        editor.Draft!.Ui.Locale="en-US";editor.Edited();
        peer.Update=request=>{peer.Value.Snapshot.Ui.Locale=request.Ui.Locale;peer.Value.Snapshot.Revision="5";return new(){Result="applied",Revision="5"};};
        var writeRead=new TaskCompletionSource<SettingsResponse>(TaskCreationOptions.RunContinuationsAsynchronously);var displayRead=new TaskCompletionSource<SettingsResponse>(TaskCreationOptions.RunContinuationsAsynchronously);var reads=0;
        peer.Read=()=>Interlocked.Increment(ref reads)==1?writeRead.Task:displayRead.Task;
        var saving=editor.SaveAsync();var loading=editor.ReloadAsync();writeRead.SetResult(peer.Value.Clone());await saving;
        Require(editor.Action==SettingsAction.Applied && !editor.Dirty,"page refresh cannot cancel a mutation's independent authoritative readback");
        displayRead.SetResult(peer.Value.Clone());await loading;peer.Read=null;
        editor.Draft!.Refresh.QuotaIntervalSeconds=300;editor.Edited();peer.Value.Snapshot.Revision="6";peer.Update=_=>throw new RpcException(new(StatusCode.Unavailable,"stale CAS"));await editor.SaveAsync();
        Require(editor.Action==SettingsAction.Conflict && editor.Draft.Refresh.QuotaIntervalSeconds==300,"an unavailable mutation with a changed authoritative revision is displayed as conflict without replay");
        var home=new HomeSwitchDraft{Target=@"C:\synthetic\home",Strategy="independent_database"};home.Planned(new(){Strategy="independent_database",TargetGeneration="2",PreservesOldFacts=true},"4");
        Require(home.CanConfirm("4") && !home.CanConfirm("5"),"Home confirmation binds the plan to the settings revision");
        home.Target=@"C:\synthetic\other";Require(!home.CanConfirm("4"),"changing a Home target invalidates its prior plan");home.Target=@"C:\synthetic\home";home.Strategy="clear_and_rebuild";Require(!home.CanConfirm("4"),"changing Home strategy cannot confirm another strategy's plan");
        Require(home.Begin() && !home.Begin(),"Home commands share a single in-flight gate across page reconstruction");home.Finish("unknown");Require(!home.Busy && home.Status=="unknown","uncertain Home operation remains explicit after releasing the gate");
        Require(RuntimePresentation.AllowedAction("pause_all") && !RuntimePresentation.AllowedAction("delete") && RuntimePresentation.RecoveryRoute("repair_store")=="analyze" && RuntimePresentation.RecoveryRoute("shell") is null,"runtime and recovery actions accept only the fixed registry");
        var l=new Localization();l.Set("en-US");Require(RuntimePresentation.Phase("invented",l)=="Unknown task","unknown runtime phases cannot masquerade as known work");
        Require(RuntimeCopy.State("active",l)=="Active source" && RuntimeCopy.State("completed",l)=="Succeeded","local source states do not fall back to unknown");
        Require(RuntimeCopy.Bytes(new(){Value=4096},l)=="4.00 KiB" && RuntimeCopy.Bytes(new(),l)=="—","small storage values do not round to zero and missing bytes remain unknown");
    }
}
