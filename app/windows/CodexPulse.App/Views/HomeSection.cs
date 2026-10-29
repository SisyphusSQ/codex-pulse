using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using CodexPulse.CoreClient;
using Grpc.Core;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Windows.Storage.Pickers;

namespace CodexPulse.App.Views;

internal sealed class HomeSection:IDisposable
{
    public StackPanel View{get;}=new(){Spacing=12};
    private readonly MainWindow host;
    private Localization L=>host.Localization;
    private readonly HomeSwitchDraft draft;
    private readonly TextBlock state=PresentationControls.Text(""),impact=PresentationControls.Text("");
    private readonly TextBox target;
    private readonly ComboBox strategy;
    private readonly Button browse,plan,confirm,recover;
    private bool disposed;
    public HomeSection(MainWindow host)
    {
        this.host=host;draft=host.HomeSwitchDraft;View.Children.Add(state);
        View.Children.Add(PresentationControls.Text(L.Text("Home 操作独立于普通设置保存。先仅探测目录元数据，再确认目标和派生索引策略。两种策略均不修改 Codex 源文件或凭据。","Home actions are independent of ordinary settings. Probe directory metadata first, then confirm the target and index strategy. Neither strategy changes Codex source files or credentials.")));
        target=new(){Header=L.Text("目标 Codex Home（绝对目录）","Target Codex Home (absolute directory)"),Text=draft.Target};target.TextChanged+=(_,_)=>{draft.Target=target.Text;Publish();};View.Children.Add(target);
        browse=new(){Content=L.Text("选择目录…","Choose folder…")};browse.Click+=async(_,_)=>
        {
            var picker=new FolderPicker();picker.FileTypeFilter.Add("*");WinRT.Interop.InitializeWithWindow.Initialize(picker,WinRT.Interop.WindowNative.GetWindowHandle(host));
            var folder=await picker.PickSingleFolderAsync();if(!disposed && folder is not null)target.Text=folder.Path;
        };View.Children.Add(browse);
        strategy=new(){Header=L.Text("切换策略","Switch strategy"),HorizontalAlignment=HorizontalAlignment.Stretch};
        strategy.Items.Add(new ComboBoxItem{Content=L.Text("新建或复用目标的独立数据库，保留原事实","Use an independent target database; retain existing facts"),Tag="independent_database"});
        strategy.Items.Add(new ComboBoxItem{Content=L.Text("清空当前派生索引后重建，保留源文件","Clear and rebuild derived indexes; retain source files"),Tag="clear_and_rebuild"});
        strategy.SelectedItem=strategy.Items.Cast<ComboBoxItem>().First(item=>(string)item.Tag==draft.Strategy);strategy.SelectionChanged+=(_,_)=>{if(strategy.SelectedItem is ComboBoxItem{Tag:string value}){draft.Strategy=value;Publish();}};View.Children.Add(strategy);
        plan=new(){Content=L.Text("1. 检测并预览影响","1. Probe and preview impact")};plan.Click+=async(_,_)=>await Run(Plan);
        confirm=new(){Content=L.Text("2. 确认切换…","2. Confirm switch…")};confirm.Click+=async(_,_)=>await Run(Confirm);
        recover=new(){Content=L.Text("读取 journal 并继续或回滚恢复…","Recover the switch from its journal…")};recover.Click+=async(_,_)=>await Run(Recover);
        View.Children.Add(plan);View.Children.Add(impact);View.Children.Add(confirm);View.Children.Add(recover);draft.Changed+=Changed;Publish();
    }
    private void Changed()=>host.Dispatch(Publish);
    public void Publish()
    {
        if(disposed)return;
        var home=host.Settings?.Snapshot.Home;
        state.Text=(home?.Configured==true?L.Text("已配置 Home","Home configured"):L.Text("尚未配置 Home","Home not configured"))+" · "+(home?.SwitchStatus switch{"stable"=>L.Text("状态稳定","Stable"),"pending"=>L.Text("切换待完成","Switch pending"),_=>L.Text("需要恢复或重新读取","Recovery or reload required")})+" · "+(draft.Status switch
        {"running"=>L.Text("操作进行中，不能重复提交","Action running; do not submit again"),"planned"=>L.Text("探测完成，请核对影响","Probe complete; review the impact"),"plan_unknown"=>L.Text("探测回执未知，请重新检测","Probe receipt unknown; probe again"),"completed"=>L.Text("已读回目标 Home 完成切换","Target Home switch read back"),"rolled_back"=>L.Text("已读回安全回滚","Safe rollback read back"),"recovery_required"=>L.Text("切换尚未完成，保留恢复责任","Switch incomplete; recovery responsibility retained"),"unknown"=>L.Text("结果未确认，先重新读取设置","Result unconfirmed; reload Settings first"),"unavailable"=>L.Text("操作未完成，请检查目录和切换状态","Action incomplete; check the directory and switch state"),_=>""});
        target.IsEnabled=strategy.IsEnabled=browse.IsEnabled=!draft.Busy;
        plan.IsEnabled=!draft.Busy && Path.IsPathFullyQualified(draft.Target) && home?.SwitchStatus=="stable";
        confirm.IsEnabled=!draft.Busy && home?.SwitchStatus=="stable" && draft.CanConfirm(host.Settings?.Snapshot.Revision??"");
        recover.IsEnabled=!draft.Busy && home is not null && home.SwitchStatus!="stable";
        impact.Text=draft.Plan is not {} value?L.Text("尚未生成切换计划。","No switch plan yet."):!draft.CanConfirm(host.Settings?.Snapshot.Revision??"")?L.Text("路径、策略或设置版本已变化，请重新检测。","Path, strategy or settings revision changed. Probe again."):
            (value.PreservesOldFacts?L.Text("保留旧 Home 的独立数据库和事实。 ","Retain the old Home database and facts. "):"")+(value.ClearsDerivedFacts?L.Text("将清理当前数据库的派生索引并重建。 ","Clear and rebuild this database's derived indexes. "):"")+L.Text("源 JSONL、正文与 Agent 凭据不会删除或修改。","Source JSONL, content and Agent credentials remain unchanged.");
    }
    private async Task Run(Func<Task<string>> action)
    {
        if(!draft.Begin())return;var result="unavailable";
        try{result=await action();}
        catch(RpcException){draft.Clear();await host.ReloadSettingsAsync();}
        catch(IOException){draft.Clear();result="unknown";await host.ReloadSettingsAsync();}
        finally{draft.Finish(result);}
    }
    private async Task<string> Plan()
    {
        var revision=host.Settings?.Snapshot.Revision??"";draft.Clear();
        var request=new PlanHomeSwitchRequest{TargetPath=draft.Target,Strategy=draft.Strategy};
        var result=await host.Client.MutateAsync((core,deadline,ct)=>core.PlanHomeSwitchAsync(request,deadline:deadline,cancellationToken:ct));
        if(result.Outcome==MutationOutcome.Unknown || result.Receipt is null)return "plan_unknown";
        draft.Planned(result.Receipt,revision);return "planned";
    }
    private async Task<string> Confirm()
    {
        if(!draft.CanConfirm(host.Settings?.Snapshot.Revision??"") || draft.Plan is not {} plan)return "plan_unknown";
        var targetGeneration=plan.TargetGeneration;
        var previousRevision=host.Settings?.Snapshot.Revision;
        if(!await ProductDialog.Confirm(host,L.Text("确认切换 Codex Home","Confirm Codex Home switch"),draft.Target+"\n\n"+impact.Text,L.Text("确认切换","Confirm switch")))return "planned";
        if(disposed || !draft.CanConfirm(host.Settings?.Snapshot.Revision??""))return "plan_unknown";
        draft.Clear();
        var result=await host.Client.MutateAsync((core,deadline,ct)=>core.ConfirmHomeSwitchAsync(new(),deadline:deadline,cancellationToken:ct));
        var readback=await ReadAuthority();return Outcome(readback,targetGeneration,previousRevision);
    }
    private async Task<string> Recover()
    {
        if(!await ProductDialog.Confirm(host,L.Text("恢复未完成的 Home 切换","Recover an incomplete Home switch"),L.Text("将读取持久 journal 和运行时状态，由 Helper 按既有协议继续或安全回滚；不重新提交切换请求。","Read the durable journal and runtime state. The Helper continues or safely rolls back using its existing protocol; it does not replay the switch request."),L.Text("执行恢复","Recover")))return "idle";
        if(disposed)return "idle";draft.Clear();var previousRevision=host.Settings?.Snapshot.Revision;
        await host.Client.MutateAsync((core,deadline,ct)=>core.RecoverHomeSwitchAsync(new(),deadline:deadline,cancellationToken:ct));
        return Outcome(await ReadAuthority(),null,previousRevision);
    }
    private async Task<SettingsResponse> ReadAuthority()
    {
        var request=new SettingsRequest();var response=await host.Client.QueryAsync("Settings",request,host.Workspace.Context,["settings"],(core,deadline,ct)=>core.SettingsAsync(request,deadline:deadline,cancellationToken:ct),force:true);
        await host.ReloadSettingsAsync();return response;
    }
    private string Outcome(SettingsResponse response,string? generation,string? previousRevision)
    {
        var home=response.Snapshot.Home;
        if(home is null)return "unknown";
        if(home.SwitchStatus!="stable")return "recovery_required";
        if(response.Snapshot.Revision==previousRevision)return "unknown";
        if(home.HasLastSwitchOutcome && home.LastSwitchOutcome=="rolled_back")return "rolled_back";
        return home.HasLastSwitchOutcome && home.LastSwitchOutcome=="completed" && (generation is null || home.Generation==generation)?"completed":"unknown";
    }
    public void Dispose(){disposed=true;draft.Changed-=Changed;}
}
