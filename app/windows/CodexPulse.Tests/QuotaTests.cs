using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Grpc.Core;

internal static class QuotaTests
{
    private static void Require(bool condition,string name){if(!condition)throw new InvalidOperationException(name);Console.WriteLine("PASS "+name);}
    private static AsyncUnaryCall<T> Reply<T>(Task<T> response) where T:class=>new(response,Task.FromResult(new Metadata()),()=>Status.DefaultSuccess,()=>new Metadata(),()=>{});
    private sealed class Peer:CoreService.CoreServiceClient
    {
        public CodexSubscriptionAccountsResponse Accounts=new();
        public Func<CodexSubscriptionMutationReceipt> Mutation=()=>new(){Result=CodexSubscriptionMutationResult.Applied};
        public Func<Task<CodexSubscriptionAccountsResponse>>? Read;
        public int Writes,Reads;
        public override AsyncUnaryCall<CodexSubscriptionAccountsResponse> ListCodexSubscriptionAccountsAsync(CodexSubscriptionAccountsRequest request,CallOptions options){Reads++;return Reply(Read?.Invoke()??Task.FromResult(Accounts.Clone()));}
        public override AsyncUnaryCall<CodexSubscriptionMutationReceipt> UpdateCodexSubscriptionAccountAsync(UpdateCodexSubscriptionAccountRequest request,CallOptions options)
        {Writes++;try{return Reply(Task.FromResult(Mutation()));}catch(RpcException error){return Reply(Task.FromException<CodexSubscriptionMutationReceipt>(error));}}
    }
    public static async Task Run()
    {
        var l=new Localization();l.Set("zh-CN");
        var binding=new CodexAccountBinding{State="confirmed",AccountScope="private-test-scope",BindingGeneration=7};
        Require(QuotaPresentation.SameAccount(binding,binding.Clone()) && !QuotaPresentation.SameAccount(binding,withGeneration(8)),"quota identity requires confirmed scope and generation");
        Require(!QuotaPresentation.SameAccount(binding,new(){State="pending",AccountScope=binding.AccountScope,BindingGeneration=7}),"pending identity cannot reuse previous quota");
        Require(QuotaPresentation.Percent(false,0) is null && QuotaPresentation.Percent(true,double.NaN) is null && QuotaPresentation.Percent(true,0)==0,"quota absent invalid and real zero remain distinct");
        Require(QuotaPresentation.Freshness("current",l)==QuotaPresentation.Freshness("fresh",l),"reset inventory current freshness is recognized as a verified value");
        Require(QuotaPresentation.PercentText(.00001,l)!="0%" && QuotaPresentation.PercentText(99.99999,l)!="100%","small remaining values do not falsely round to endpoints");
        var quota=new CurrentQuota{EvaluatedAtMs=1000};quota.Windows.Add(new CurrentWindow{LimitId="codex",WindowMinutes=90,ResetsAtMs=500,RemainingPercent=50,Freshness="fresh"});quota.Windows.Add(new CurrentWindow{LimitId="codex",WindowMinutes=90,ResetsAtMs=2000,RemainingPercent=75,Freshness="stale"});quota.Windows.Add(new CurrentWindow{LimitId="model",WindowMinutes=43200});
        Require(QuotaPresentation.Windows(quota).Count==2 && QuotaPresentation.Windows(quota)[0].RemainingPercent==75 && QuotaPresentation.Duration(90,l)=="90 分钟","dynamic windows prefer active cycle without assuming fixed periods");
        var account=new CodexSubscriptionAccount{AccountId="detected:test",Detected=true,Current=true,DetectedAccountId="test",DetectedEmail="auto@example.invalid",AutomaticPlan=CodexSubscriptionPlan.Plus,DetectedRevision=4};
        var draft=SubscriptionDraft.Edit(account);draft.Alias="draft";
        Require(!draft.Fields().HasEmail && !draft.Fields().HasPlan && draft.UpdateRequest().NewManualEntryId==draft.ManualId,"editing alias never converts automatic email or plan into overrides");
        var newer=account.Clone();newer.DetectedRevision=5;draft.Rebase(newer);
        Require(draft.Alias=="draft" && draft.Original?.DetectedRevision==5,"revision rebase preserves unsaved manual fields");
        draft.DateKind=CodexSubscriptionDateKind.NextRenewal;draft.MembershipDate="2000-01-31";
        Require(draft.Fields().MembershipDate=="2000-01-31","monthly renewal stores the civil day anchor");
        var refused=false;try{SubscriptionDraft.DeleteRequest(account);}catch(ArgumentException){refused=true;}
        Require(refused,"current account deletion is refused before RPC");
        var peer=new Peer();peer.Accounts.Accounts.Add(account);
        var context=new QueryContext("codex",new(2026,10,28),new(2026,10,29),"Asia/Hong_Kong",1);
        using var controller=new SubscriptionController(new(()=>peer),()=>context,"home-test");controller.BeginEdit(account);controller.Draft!.Alias="draft";
        peer.Mutation=()=>new(){Result=CodexSubscriptionMutationResult.Conflict,Reason="revision_changed"};
        await controller.SaveAsync();Require(controller.Action==SubscriptionAction.Conflict && controller.Draft?.Alias=="draft" && peer.Reads==1,"mutation conflict forces authority readback and retains draft");
        peer.Mutation=()=>new(){Result=CodexSubscriptionMutationResult.Applied};await controller.SaveAsync();
        Require(controller.Action==SubscriptionAction.ReadbackChanged && controller.Draft is not null,"applied receipt without matching list is not optimistic success");
        peer.Mutation=()=>{peer.Accounts.Accounts[0].Alias="draft";throw new RpcException(new(StatusCode.Unavailable,"transport"));};
        await controller.SaveAsync();Require(controller.Action==SubscriptionAction.ReadbackConfirmed && controller.Draft is null && peer.Writes==3,"lost mutation receipt is never replayed and matching authoritative list is distinguished");
        controller.BeginEdit(peer.Accounts.Accounts[0]);peer.Mutation=()=>new();await controller.SaveAsync();
        Require(controller.Action==SubscriptionAction.Unknown && controller.Draft is not null,"unknown receipt cannot be presented as applied");
        var invalid=new Google.Rpc.Status{Code=(int)StatusCode.InvalidArgument,Message="invalid request"};invalid.Details.Add(Google.Protobuf.WellKnownTypes.Any.Pack(new ErrorDetail{Code="validation",Field="manual.email"}));
        peer.Mutation=()=>throw invalid.ToRpcException();await controller.SaveAsync();
        Require(controller.Action==SubscriptionAction.Unavailable && controller.Field=="manual.email" && controller.Draft is not null,"validation preserves the draft and safe responsible field");
        peer.Mutation=()=>new(){Result=CodexSubscriptionMutationResult.Applied};
        var writeRead=new TaskCompletionSource<CodexSubscriptionAccountsResponse>(TaskCreationOptions.RunContinuationsAsynchronously);
        var displayRead=new TaskCompletionSource<CodexSubscriptionAccountsResponse>(TaskCreationOptions.RunContinuationsAsynchronously);var reads=0;
        peer.Read=()=>Interlocked.Increment(ref reads)==1?writeRead.Task:displayRead.Task;
        var writes=peer.Writes;var saving=controller.SaveAsync();context=context with{Generation=2};var loading=controller.ReloadAsync();
        writeRead.SetResult(peer.Accounts.Clone());await saving;
        Require(controller.Action==SubscriptionAction.Applied && controller.Draft is null && peer.Writes==writes+1,"account invalidation and page refresh cannot cancel a subscription command's authority readback");
        displayRead.SetResult(new());await loading;
        Require(controller.List.State.Value?.Accounts.Count==1,"an older page response cannot replace the command's confirmed account list");
        controller.BeginEdit(peer.Accounts.Accounts[0]);peer.Read=()=>Task.FromException<CodexSubscriptionAccountsResponse>(new RpcException(new(StatusCode.Unavailable,"readback lost")));
        await controller.SaveAsync();Require(controller.Action==SubscriptionAction.Unknown && controller.Draft is not null,"applied subscription receipt with unavailable authority readback preserves the draft and stays unknown");
        writeRead=new(TaskCreationOptions.RunContinuationsAsynchronously);peer.Read=()=>writeRead.Task;
        saving=controller.SaveAsync();controller.Dispose();writeRead.SetResult(peer.Accounts.Clone());await saving;
        Require(controller.List.State.Kind==LoadKind.Idle && controller.Draft is null,"a disposed Home coordinator cannot publish a late subscription readback");
        CodexAccountBinding withGeneration(ulong generation){var copy=binding.Clone();copy.BindingGeneration=generation;return copy;}
    }
}
