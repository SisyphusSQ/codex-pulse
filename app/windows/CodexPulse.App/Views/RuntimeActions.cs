using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using CodexPulse.CoreClient;
using Grpc.Core;
using Microsoft.UI.Xaml.Controls;

namespace CodexPulse.App.Views;

internal sealed class RuntimeActions(MainWindow host,Func<Task> reload)
{
    private Localization L=>host.Localization;
    public TextBlock Status{get;}=PresentationControls.Text("");
    public Button Action(string action)
    {
        if(!RuntimePresentation.AllowedAction(action))throw new ArgumentException("runtime_action_unregistered");
        var label=action switch{"pause_backfill"=>L.Text("暂停历史补齐","Pause history backfill"),"pause_all"=>L.Text("暂停全部索引任务","Pause all indexing"),"resume"=>L.Text("恢复索引任务","Resume indexing"),_=>L.Text("立即对账来源","Reconcile sources now")};
        var button=new Button{Content=label};button.Click+=async(_,_)=>await Execute(button,async()=>
        {
            if(!await ProductDialog.Confirm(host,label,action=="reconcile"?L.Text("只对已确认 Home 执行既有来源对账，增量追平由后台继续；不会隐式全量重扫。","Reconcile sources for the confirmed Home. Incremental work continues in the background; this does not request a full rescan."):L.Text("通过 Helper 保存运行意图并等待协作边界；不修改源文件。","Persist runtime intent through the Helper and wait for cooperative boundaries. Source files remain unchanged."),label))return;
            Status.Text=L.Text("正在提交运行意图并等待读回…","Submitting runtime intent and waiting for readback…");
            var request=new RuntimeActionRequest{Action=action};var result=await host.Client.MutateAsync((core,deadline,ct)=>core.RunRuntimeActionAsync(request,deadline:deadline,cancellationToken:ct));
            Status.Text=result.Receipt is{ } receipt && result.Outcome!=MutationOutcome.Unknown && receipt.Action==action?
                L.Text("已收到运行回执：暂停范围 ","Runtime receipt: pause scope ")+receipt.PauseScope+" · "+receipt.SourceState+" · "+receipt.Transition+L.Text("。已请求页面重读，请核对各区块状态。",". Page readback requested; check each section's state."):
                L.Text("命令结果未确认，正在重读；不会自动重发。","Command result unconfirmed; reading back without replay.");
            await reload();
        });return button;
    }
    public Button Analyze()
    {
        var button=new Button{Content=L.Text("分析 Session 索引修复（dry-run）…","Analyze Session index repair (dry-run)…")};button.Click+=async(_,_)=>await Execute(button,async()=>
        {
            if(!await ProductDialog.Confirm(host,L.Text("只读修复分析","Read-only repair analysis"),L.Text("仅检查已确认 Home 的索引元数据并返回动作、冲突和历史计数。本操作不执行修复，不写入或删除 Codex 源文件。","Inspect confirmed Home index metadata and return action, conflict and history counts. This analysis does not execute repairs or change Codex source files."),L.Text("开始只读分析","Start read-only analysis")))return;
            Status.Text=L.Text("正在只读分析索引…","Analyzing the index read-only…");
            var result=await host.Client.MutateAsync((core,deadline,ct)=>core.AnalyzeSessionIndexRepairAsync(new(),deadline:deadline,cancellationToken:ct));
            Status.Text=result.Receipt is {} value?L.Text("分析完成 ","Analysis completed ")+L.Timestamp(value.AnalyzedAtMs)+L.Text(" · 动作 "," · Actions ")+value.ActionCount+L.Text(" · 冲突 "," · Conflicts ")+value.ConflictCount+L.Text(" · 历史 "," · Histories ")+value.HistoryCount+L.Text(" · 诊断 "," · Diagnostics ")+value.DiagnosticCount+(value.Noop?L.Text(" · 无待修动作"," · No pending repair actions"):L.Text(" · 未执行修复"," · Repair not executed")):
                L.Text("分析回执未知；请先重读，未执行修复。","Analysis receipt unknown. Reload first; repair was not executed.");
            await reload();
        });return button;
    }
    public Button? Recovery(string kind)
    {
        return RuntimePresentation.RecoveryRoute(kind) switch
        {"reconcile"=>Action("reconcile"),"analyze"=>Analyze(),"reload"=>Reload(),"settings"=>Settings(),_=>null};
    }
    private Button Reload(){var button=new Button{Content=L.Text("重新读取状态","Reload status")};button.Click+=async(_,_)=>await reload();return button;}
    private Button Settings(){var button=new Button{Content=L.Text("打开设置处理","Open Settings")};button.Click+=(_,_)=>host.Navigate(Feature.Settings);return button;}
    private async Task Execute(Button button,Func<Task> action)
    {
        if(!await host.RuntimeCommandGate.WaitAsync(0)){Status.Text=L.Text("另一个运行操作正在进行。","Another runtime action is running.");return;}
        button.IsEnabled=false;
        try{await action();}
        catch(RpcException error){Status.Text=L.Text("操作未完成：","Action incomplete: ")+(CoreConnection.ErrorDetail(error)?.Code??error.StatusCode.ToString());}
        catch(IOException){Status.Text=L.Text("连接中断，结果尚未确认；请重新读取。","Connection interrupted; result unconfirmed. Reload.");}
        finally{button.IsEnabled=true;host.RuntimeCommandGate.Release();}
    }
}
