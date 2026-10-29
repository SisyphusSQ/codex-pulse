using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using CodexPulse.CoreClient;
using Grpc.Core;

internal static class ControlsTests
{
    private static void Require(bool value,string name){if(!value)throw new InvalidOperationException(name);Console.WriteLine("PASS "+name);}
    public static async Task Run(string executable,string root)
    {
        var normal=Path.Combine(root,"normal");var homeA=Path.Combine(normal,"synthetic-home");var homeB=Path.Combine(normal,"另一个 Home");Directory.CreateDirectory(homeB);
        await using(var session=await HelperSession.StartAsync(executable,normal,homeA,"dev"))
        {
            var core=session.Connection.Raw;var context=new QueryContext("",new(2026,10,28),new(2026,10,29),"Asia/Hong_Kong",1);
            using var editor=new SettingsEditor(new(()=>core),()=>context);await editor.ReloadAsync();
            Require(editor.Current.State.Value!.Snapshot.Providers.All(item=>item.Intent==ProviderIntent.Disabled),"controlled fixtures disable every provider before launching the Helper");
            editor.Draft!.Ui.Locale="zh-CN";editor.Edited();await editor.SaveAsync();Require(editor.Action==SettingsAction.Applied,"actual C# settings command has an authoritative Go readback");
            var stale=editor.Draft.Clone();editor.Draft.Refresh.QuotaIntervalSeconds=120;editor.Edited();await editor.SaveAsync();
            var conflict=false;try{await core.UpdateSettingsAsync(stale,deadline:DateTime.UtcNow.AddSeconds(10));}catch(RpcException error){conflict=error.StatusCode==StatusCode.Unavailable;}var authoritative=await core.SettingsAsync(new());
            Require(conflict && authoritative.Snapshot.Revision!=stale.ExpectedRevision && authoritative.Snapshot.Refresh.QuotaIntervalSeconds==120,"actual Go settings CAS rejects stale values and retains the authoritative snapshot");
            editor.Draft!.ExpectedRevision=stale.ExpectedRevision;editor.Draft.Refresh.QuotaIntervalSeconds=300;editor.Edited();await editor.SaveAsync();
            Require(editor.Action==SettingsAction.Conflict && editor.Draft.Refresh.QuotaIntervalSeconds==300,"Windows settings presents a real Core unavailable CAS response as a revision conflict after readback");
            foreach(var (path,strategy) in new[]{(homeA,"independent_database"),(homeB,"independent_database"),(homeA,"independent_database"),(homeB,"clear_and_rebuild")})
            {
                var plan=await core.PlanHomeSwitchAsync(new(){TargetPath=path,Strategy=strategy},deadline:DateTime.UtcNow.AddSeconds(15));
                Require(plan.Strategy==strategy && plan.PreservesOldFacts==(strategy=="independent_database") && plan.ClearsDerivedFacts==(strategy=="clear_and_rebuild"),"actual Home plan exposes the selected strategy's impact");
                var receipt=await core.ConfirmHomeSwitchAsync(new(),deadline:DateTime.UtcNow.AddSeconds(30));var readback=await core.SettingsAsync(new(),deadline:DateTime.UtcNow.AddSeconds(10));
                Require(receipt.Result=="completed" && readback.Snapshot.Home.Configured && readback.Snapshot.Home.SwitchStatus=="stable" && readback.Snapshot.Home.Generation==plan.TargetGeneration,"actual Windows Home switch and return read back the planned generation");
            }
            var sources=await core.ListSourcesAsync(new(){Query=new(){Page=new(){Limit=20}}},deadline:DateTime.UtcNow.AddSeconds(10));
            var jobs=await core.ListJobsAsync(new(){Query=new(){Page=new(){Limit=20}}},deadline:DateTime.UtcNow.AddSeconds(10));
            var health=await core.DataHealthAsync(new(){EvaluatedAtMs=DateTimeOffset.Now.ToUnixTimeMilliseconds()},deadline:DateTime.UtcNow.AddSeconds(10));
            var projection=await core.HealthProjectionAsync(new(),deadline:DateTime.UtcNow.AddSeconds(10));
            Require(sources.Summary.Total.HasValue && jobs.Summary.Total.HasValue && health.EvaluatedAtMs.HasValue,"source job and Data Health control surfaces remain available with every provider disabled");
            Require(projection.HasValue || !projection.HasLevel,"missing health projection cannot claim a healthy level");
            Require(await session.StopAsync(),"controlled normal Helper drains and exits");
        }
        var recovery=Path.Combine(root,"recovery");
        await using(var session=await HelperSession.StartAsync(executable,recovery,Path.Combine(recovery,"synthetic-home"),"dev"))
        {
            var core=session.Connection.Raw;Require(session.State==CoreConnectionState.Recovery,"real Windows Helper starts in recovery-only mode for a future schema");
            var state=await core.MigrationRecoveryStateAsync(new());Require(state.Backups.Any(item=>item.Name=="known-good.db"),"recovery lists the protected known-good fixture backup");
            var prepared=await core.MigrationRecoveryPrepareAsync(new(){BackupName="known-good.db"});Require(!string.IsNullOrEmpty(prepared.ConfirmationToken),"restore preparation returns an in-memory confirmation capability");
            await core.MigrationRecoveryCancelAsync(new());state=await core.MigrationRecoveryStateAsync(new());Require(state.Phase=="failed","cancel restores the explicit recovery phase without replacing the database");
            prepared=await core.MigrationRecoveryPrepareAsync(new(){BackupName="known-good.db"});
            var receipt=await core.MigrationRecoveryConfirmAsync(new(){ConfirmationToken=prepared.ConfirmationToken},deadline:DateTime.UtcNow.AddSeconds(30));state=await core.MigrationRecoveryStateAsync(new());
            Require(receipt.RestartRequired && state.Phase=="restart_required","confirmed backup restoration requires a fresh Helper process");
            var gated=false;try{await core.SettingsAsync(new());}catch(RpcException error){gated=error.StatusCode==StatusCode.FailedPrecondition;}Require(gated,"restoration cannot hot-load normal services in the old recovery process");
            Require(await session.StopAsync(),"restored recovery Helper exits cleanly");
        }
        await using(var restarted=await HelperSession.StartAsync(executable,recovery,Path.Combine(recovery,"synthetic-home"),"dev"))
        {Require(restarted.State==CoreConnectionState.Connected && (await restarted.Connection.Raw.SettingsAsync(new())).Snapshot.Revision!="","fresh Helper opens restored database and normal settings");Require(await restarted.StopAsync(),"restarted restored Helper drains cleanly");}
    }
}
