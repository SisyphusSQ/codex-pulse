using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Grpc.Core;

internal static class ReportingTests
{
    private static void Require(bool value,string name){if(!value)throw new InvalidOperationException(name);Console.WriteLine("PASS "+name);}
    private static AsyncUnaryCall<T> Reply<T>(Task<T> task)where T:class=>new(task,Task.FromResult(new Metadata()),()=>Status.DefaultSuccess,()=>new Metadata(),()=>{});
    private sealed class Peer:CoreService.CoreServiceClient
    {
        public ReportingStatusResponse Value=new(){State="disabled",IntervalSeconds=60};
        public Func<Task<ReportingStatusResponse>>? Read;
        public bool LosePair,LoseSync;
        public int Writes;
        public PairReportingRequest? PairRequest;
        public ConfigureReportingRequest? Configuration;
        public override AsyncUnaryCall<ReportingStatusResponse> ReportingStatusAsync(Empty request,CallOptions options)=>Reply(Read?.Invoke()??Task.FromResult(Value.Clone()));
        public override AsyncUnaryCall<ReportingStatusResponse> PairReportingAsync(PairReportingRequest request,CallOptions options)
        {Writes++;PairRequest=request;Value.Endpoint=request.Endpoint.TrimEnd('/');Value.AllowHttp=request.AllowHttp;Value.ClientId="device-"+Writes;Value.Enabled=false;return Reply(LosePair?Task.FromException<ReportingStatusResponse>(new RpcException(new(StatusCode.Unavailable,"lost"))):Task.FromResult(Value.Clone()));}
        public override AsyncUnaryCall<ReportingStatusResponse> ConfigureReportingAsync(ConfigureReportingRequest request,CallOptions options)
        {Writes++;Configuration=request.Clone();Value.Enabled=request.Enabled;Value.IntervalSeconds=request.IntervalSeconds;Value.HistoryStartAtMs=request.HistoryStartAtMs;if(request.ClearPending)Value.PendingBatches=Value.PendingBytes=0;return Reply(Task.FromResult(Value.Clone()));}
        public override AsyncUnaryCall<ReportingStatusResponse> SyncReportingNowAsync(Empty request,CallOptions options)
        {Writes++;Value.LastSuccessAtMs=1234;return Reply(LoseSync?Task.FromException<ReportingStatusResponse>(new RpcException(new(StatusCode.Unavailable,"lost"))):Task.FromResult(Value.Clone()));}
    }
    public static async Task Run()
    {
        var peer=new Peer();var context=new QueryContext("codex",new(2026,10,28),new(2026,10,29),"Asia/Hong_Kong",1);
        using var controller=new ReportingController(new(()=>peer),()=>context);await controller.ReloadAsync();
        var initialDraft=controller.Draft;peer.Value.LastSuccessAtMs=123;await controller.ReloadAsync();Require(ReferenceEquals(initialDraft,controller.Draft),"background progress updates do not recreate an unchanged reporting draft or reset its date controls");
        controller.Draft.Endpoint="https://center.invalid/";controller.Draft.IntervalSeconds=120;controller.Draft.HistoryStartAtMs=1000;controller.Edited();
        await controller.PairAsync("synthetic-one-time-code");
        Require(controller.Action==ReportingAction.Applied && peer.PairRequest?.Code=="" && !controller.Draft.Enabled,"pairing clears its transient request code and never enables reporting implicitly");
        Require(controller.Draft.IntervalSeconds==120 && controller.Draft.HistoryStartAtMs==1000,"pairing preserves independently edited synchronization choices");
        controller.Draft.Endpoint="https://next.invalid";controller.Draft.AllowHttp=true;controller.Draft.Enabled=true;controller.Edited();await controller.ConfigureAsync();
        Require(controller.Action==ReportingAction.Applied && controller.Draft.Endpoint=="https://next.invalid" && controller.Draft.AllowHttp && peer.Value.Endpoint=="https://center.invalid","saving sync configuration cannot pair or discard the independent endpoint draft");
        peer.Value.PendingBatches=2;peer.Value.HistoryStartAtMs=1000;await controller.ReloadAsync();controller.Draft.HistoryStartAtMs=2000;controller.Draft.IntervalSeconds=180;controller.Edited();await controller.ConfigureAsync(true);
        Require(peer.Configuration is{Enabled:false,ClearPending:true,HistoryStartAtMs:1000,IntervalSeconds:120} && peer.Value.PendingBatches==0,"explicit queue clearing disables reporting and preserves its authoritative history range and interval");
        Require(controller.Draft.IntervalSeconds==180 && controller.ConfigurationDirty,"clearing the queue retains an independently edited interval without saving it");
        controller.Reset();controller.Draft.Endpoint="https://center.invalid";peer.LosePair=true;var writes=peer.Writes;await controller.PairAsync("synthetic-another-code");
        Require(controller.Action==ReportingAction.ReadbackConfirmed && peer.Writes==writes+1 && peer.PairRequest?.Code=="","a lost pair response is read back without replaying the consumed code");
        controller.Draft.Enabled=true;await controller.ConfigureAsync();peer.LoseSync=true;writes=peer.Writes;await controller.SyncAsync(false);
        Require(controller.Action==ReportingAction.Unknown && peer.Writes==writes+1 && controller.Current.State.Value?.HasLastSuccessAtMs==true,"a newer successful sync timestamp does not fabricate acknowledgment of an unknown command");
        var writeRead=new TaskCompletionSource<ReportingStatusResponse>(TaskCreationOptions.RunContinuationsAsynchronously);var pageRead=new TaskCompletionSource<ReportingStatusResponse>(TaskCreationOptions.RunContinuationsAsynchronously);var reads=0;
        peer.Read=()=>Interlocked.Increment(ref reads)==1?writeRead.Task:pageRead.Task;controller.Draft.IntervalSeconds=180;
        var saving=controller.ConfigureAsync();var loading=controller.ReloadAsync();writeRead.SetResult(peer.Value.Clone());await saving;
        Require(controller.Action==ReportingAction.Applied && controller.Draft.IntervalSeconds==180,"page refresh cannot cancel a reporting mutation's authority readback");pageRead.SetResult(new());await loading;
        Require(controller.Current.State.Value?.ClientId==peer.Value.ClientId,"a stale page response cannot overwrite the confirmed reporting device");
        peer.Read=null;peer.Value.State="processing";await controller.ReloadAsync();var l=new Localization();l.Set("en-US");
        Require(ReportingCopy.State("processing",l).Contains("unconfirmed") && ReportingCopy.State("publication_failed",l).Contains("failed") && ReportingCopy.State("invented",l)=="Unknown state","processing, publication failure and unknown states cannot appear as final delivery success");
        Require(ReportingController.WebAddress(new(){Endpoint="http://10.0.0.1:18089",AllowHttp=false}) is null && ReportingController.WebAddress(new(){Endpoint="https://user:secret@center.invalid"}) is null && ReportingController.WebAddress(new(){Endpoint="https://center.invalid"}) is not null,"opening the center respects saved transport consent and rejects credential-bearing addresses");
        Require(typeof(ReportingDraft).GetProperties().All(value=>!value.Name.Contains("Code") && !value.Name.Contains("Credential")),"reporting drafts contain neither pairing codes nor device credentials");
    }
}
