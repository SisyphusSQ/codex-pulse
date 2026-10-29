using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Grpc.Core;

internal static class ApiSubscriptionTests
{
    private static void Require(bool value,string name){if(!value)throw new InvalidOperationException(name);Console.WriteLine("PASS "+name);}
    private static AsyncUnaryCall<T> Reply<T>(Task<T> task)=>new(task,Task.FromResult(new Metadata()),()=>Status.DefaultSuccess,()=>new Metadata(),()=>{});
    private sealed class Peer:CoreService.CoreServiceClient
    {
        public APICredentialStatusResponse StatusValue=new();
        public APISubscriptionsCurrentResponse Data=new(){DeepSeek=new(){Status=new(){State="current"},Balance=new(){IsAvailable=true}},OpenCodeGo=new(){Status=new(){State="stale",FailureCode="rate_limit"}}};
        public UpdateAPICredentialRequest? Request;
        public int Writes,Reads;
        public bool LoseResponse;
        public Func<Task<APISubscriptionsCurrentResponse>>? Read;
        public override AsyncUnaryCall<APICredentialStatusResponse> APICredentialStatusAsync(APICredentialStatusRequest request,CallOptions options)=>Reply(Task.FromResult(StatusValue.Clone()));
        public override AsyncUnaryCall<APISubscriptionsCurrentResponse> APISubscriptionsCurrentAsync(APISubscriptionsCurrentRequest request,CallOptions options){Reads++;return Reply(Read?.Invoke()??Task.FromResult(Data.Clone()));}
        public override AsyncUnaryCall<APICredentialStatusResponse> UpdateAPICredentialAsync(UpdateAPICredentialRequest request,CallOptions options)
        {
            Writes++;Request=request;var configured=request.ActionCase==UpdateAPICredentialRequest.ActionOneofCase.Secret;
            if(request.Service=="deepseek")StatusValue.DeepSeekConfigured=configured;else StatusValue.OpenCodeGoConfigured=configured;
            return Reply(LoseResponse?Task.FromException<APICredentialStatusResponse>(new RpcException(new(StatusCode.Unavailable,"lost"))):Task.FromResult(StatusValue.Clone()));
        }
    }
    public static async Task Run()
    {
        var peer=new Peer();var context=new QueryContext("codex",new(2026,10,29),new(2026,10,30),"Asia/Shanghai",1);var restarts=0;
        using var controller=new ApiSubscriptionsController(new(()=>peer),()=>context,()=>{Require(peer.Request?.ActionCase==UpdateAPICredentialRequest.ActionOneofCase.None,"credential mutation request drops its secret before restarting the Helper");restarts++;return Task.FromResult(true);});
        await controller.ReloadAsync();Require(controller.Current.State.Kind==LoadKind.Partial && controller.Current.State.Value?.DeepSeek.Status.State=="current","one API source failure preserves the other source's successful value");
        var bytes=System.Text.Encoding.UTF8.GetBytes("synthetic-key");await controller.UpdateAsync("deepseek",bytes);
        Require(bytes.All(value=>value==0) && controller.Action==ApiCredentialAction.Applied && restarts==1 && controller.Credentials.State.Value?.DeepSeekConfigured==true,"saving zeroes the caller buffer and converges after restart plus authoritative status readback");
        peer.LoseResponse=true;var writes=peer.Writes;await controller.UpdateAsync("deepseek",System.Text.Encoding.UTF8.GetBytes("replacement"));
        Require(controller.Action==ApiCredentialAction.Unknown && peer.Writes==writes+1,"an already-configured flag cannot confirm an unknown replacement key and the mutation is not replayed");
        await controller.UpdateAsync("deepseek",[],true);Require(controller.Action==ApiCredentialAction.ReadbackConfirmed && controller.Credentials.State.Value?.DeepSeekConfigured==false,"an absent key after restart confirms an unknown deletion without replay");
        peer.LoseResponse=false;var old=new TaskCompletionSource<APISubscriptionsCurrentResponse>(TaskCreationOptions.RunContinuationsAsynchronously);peer.Read=()=>old.Task;var loading=controller.ReloadAsync();peer.Read=null;
        await controller.UpdateAsync("opencode_go",System.Text.Encoding.UTF8.GetBytes("new-generation"));old.SetResult(new(){DeepSeek=new(){Status=new(){State="stale"}}});await loading;
        Require(controller.Current.State.Value?.DeepSeek.Status.State=="current","a late response for the previous credential generation cannot overwrite post-restart data");
        using var failed=new ApiSubscriptionsController(new(()=>peer),()=>context,()=>Task.FromResult(false));await failed.ReloadAsync();var reads=peer.Reads;
        await failed.UpdateAsync("deepseek",System.Text.Encoding.UTF8.GetBytes("pending-restart"));await failed.ReloadAsync();
        Require(failed.Action==ApiCredentialAction.RestartRequired && failed.Current.State.Value is null && peer.Reads==reads,"uncertain Helper restart hides all old-generation data and prevents queries against the old sampler");
        failed.ResetConnection();await failed.ReloadAsync();Require(failed.Current.State.Value is not null,"explicit successful reconnection reopens independent API queries");
        var l=new Localization();l.Set("en-US");var calendar=new APISubscriptionActivityCalendar{ReportingTimeZone="Asia/Shanghai"};
        calendar.Days.Add(new APISubscriptionActivityDay{DateKey="2026-10-27"});
        var zero=new APISubscriptionActivityDay{DateKey="2026-10-28",OpenCodeGo=new(){SampleCount=1,MaxFiveHourUsedPercent=0,LatestFiveHourRemainingPercent=100}};zero.DeepSeek.Add(new DeepSeekDailyActivity{Currency="USD",SampleCount=1,TotalConsumed="0",TotalRecharged="0"});calendar.Days.Add(zero);
        var active=new APISubscriptionActivityDay{DateKey="2026-10-29",OpenCodeGo=new(){SampleCount=2,MaxFiveHourUsedPercent=90,LatestFiveHourRemainingPercent=20}};active.DeepSeek.Add(new DeepSeekDailyActivity{Currency="CNY",SampleCount=1,TotalConsumed="99",TotalRecharged="0"});calendar.Days.Add(active);
        var cells=ApiSubscriptionPresentation.Activity(calendar,"USD",l);
        Require(cells[0].Intensity==-1 && cells[1].Intensity==0 && cells[2].Intensity>0,"the unified API calendar distinguishes no sample, known zero and the independently active Go source");
        Require(cells[2].Detail.Contains("—") && !cells[2].Detail.Contains("99"),"selected-currency activity never combines another currency into consumption");
        Require(ApiSubscriptionPresentation.Amount("0.000000000000000001")==.000000000000000001m && ApiSubscriptionPresentation.DecimalText("1.2300")=="1.2300","decimal chart conversion and raw text retain tiny values and original trailing precision");
        Require(!ApiSubscriptionPresentation.HasFacts(new(){State="unconfigured"}) && ApiSubscriptionPresentation.HasFacts(new(){State="stale"}),"unconfigured sources cannot display history while stale same-generation facts remain visible");
        Require(ApiSubscriptionPresentation.Failure("untrusted secret response",l)=="Source failure","unexpected failure content is never echoed into API source messages");
    }
}
