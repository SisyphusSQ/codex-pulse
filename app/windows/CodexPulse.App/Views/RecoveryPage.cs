using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Grpc.Core;
using Microsoft.UI.Xaml.Controls;

namespace CodexPulse.App.Views;

internal sealed class RecoveryPage:FeaturePage
{
    private readonly DataSection<MigrationRecoverySnapshot> recovery;
    private readonly TextBlock feedback;
    private MigrationRestoreConfirmation? confirmation;
    private bool disposed,busy;
    public RecoveryPage(MainWindow host):base(host)
    {
        Body.Children.Add(Text(L.Text("数据库迁移恢复","Database migration recovery"),"SubtitleTextBlockStyle"));
        Body.Children.Add(Text(L.Text("普通业务服务尚未装配。可重试迁移，或先准备备份再显式确认恢复。成功后必须重新启动 Helper；源 Codex 文件保持只读。","Normal business services are not loaded. Retry migration, or prepare a backup and explicitly confirm restoration. Successful recovery requires a new Helper process. Codex source files remain read-only.")));
        feedback=Text("");Body.Children.Add(feedback);
        recovery=new(host,L.Text("恢复状态","Recovery state"),Render,"migration-recovery");Body.Children.Add(recovery.View);
    }
    public override bool DependsOn(string domain)=>false;
    public override async Task LoadAsync(bool force=false)
    {
        var request=new MigrationRecoveryStateRequest();
        await recovery.Slot.LoadAsync(Host.Workspace.Generation,ct=>Host.Client.QueryAsync("MigrationRecoveryState",request,Host.Workspace.Context,[],(core,deadline,cancel)=>core.MigrationRecoveryStateAsync(request,deadline:deadline,cancellationToken:cancel),ct,true));
        if(recovery.Slot.State.Value?.Phase!="awaiting_confirmation")confirmation=null;
        if(!disposed)State(LoadKind.RecoveryBlocked);
    }
    private void Render(StackPanel panel,MigrationRecoverySnapshot snapshot)
    {
        panel.Children.Add(Text(L.Text("数据库版本 ","Database version ")+$"{snapshot.CurrentVersion} → {snapshot.TargetVersion}"+L.Text(" · 阶段 "," · Phase ")+snapshot.Phase));
        panel.Children.Add(Text(L.Text("有限失败分类：","Bounded failure classification: ")+snapshot.Stage+" / "+snapshot.Code));
        if(snapshot.AuditWarning)panel.Children.Add(Text(L.Text("恢复审计存在警告，请保留当前数据库与备份并核对状态。","Recovery audit has a warning. Keep the database and backups and check the state.")));
        var retry=new Button{Content=L.Text("重试迁移","Retry migration"),IsEnabled=snapshot.CanRetry && !busy};retry.Click+=async(_,_)=>await Run(async()=>
        {await Host.Client.MutateAsync((core,deadline,ct)=>core.MigrationRecoveryRetryAsync(new(),deadline:deadline,cancellationToken:ct));await ReadbackAndRestart();});panel.Children.Add(retry);
        foreach(var backup in snapshot.Backups)
        {
            var row=new StackPanel{Spacing=6};row.Children.Add(Text(backup.Name+" · "+backup.SizeBytes.ToString("N0",L.Culture)+" B · "+L.Timestamp(backup.ModifiedAtMs)));
            var prepare=new Button{Content=L.Text("准备此备份的恢复","Prepare this backup"),IsEnabled=snapshot.Phase=="failed" && !busy};prepare.Click+=async(_,_)=>await Run(async()=>
            {
                confirmation=null;var request=new MigrationRecoveryPrepareRequest{BackupName=backup.Name};var result=await Host.Client.MutateAsync((core,deadline,ct)=>core.MigrationRecoveryPrepareAsync(request,deadline:deadline,cancellationToken:ct));
                if(result.Receipt is {} receipt && result.Outcome!=MutationOutcome.Unknown)confirmation=receipt;
                feedback.Text=confirmation is not null?L.Text("备份已验证；请核对名称、大小与影响，再确认恢复。","Backup verified. Review its name, size and impact before confirming."):L.Text("准备回执未知；先读回状态，必要时取消准备再重试。","Prepare receipt unknown. Read state; cancel preparation before retrying if needed.");await LoadAsync(true);
            });row.Children.Add(prepare);panel.Children.Add(PresentationControls.Card(row));
        }
        if(snapshot.Phase=="awaiting_confirmation")
        {
            var restore=new Button{Content=L.Text("确认恢复已验证备份…","Confirm restoration of verified backup…"),IsEnabled=confirmation is not null && !busy};restore.Click+=async(_,_)=>await Run(async()=>
            {
                if(confirmation is not {} prepared)return;
                if(!await ProductDialog.Confirm(Host,L.Text("恢复本机数据库备份","Restore local database backup"),prepared.Backup.Name+"\n"+prepared.Backup.SizeBytes.ToString("N0",L.Culture)+L.Text(" 字节\n\nHelper 将在受保护目录中校验、迁移备份，保留失败数据库的安全副本后替换派生数据库。成功后重新启动 Helper；不会修改 Codex 原始数据。"," bytes\n\nThe Helper verifies and migrates the backup in protected storage, preserves a safety copy of the failed database, and replaces the derived database. It then requires a new Helper process. Codex source data remains unchanged."),L.Text("确认恢复","Confirm restore")) || disposed)return;
                confirmation=null;var request=new MigrationRecoveryConfirmRequest{ConfirmationToken=prepared.ConfirmationToken};
                await Host.Client.MutateAsync((core,deadline,ct)=>core.MigrationRecoveryConfirmAsync(request,deadline:deadline,cancellationToken:ct));await ReadbackAndRestart();
            });panel.Children.Add(restore);
            var cancel=new Button{Content=L.Text("取消已准备的恢复","Cancel prepared restoration"),IsEnabled=!busy};cancel.Click+=async(_,_)=>await Run(async()=>
            {await Host.Client.MutateAsync((core,deadline,ct)=>core.MigrationRecoveryCancelAsync(new(),deadline:deadline,cancellationToken:ct));confirmation=null;await LoadAsync(true);});panel.Children.Add(cancel);
        }
        if(snapshot.Phase=="restart_required")
        {var restart=new Button{Content=L.Text("重新启动 Helper 并连接","Restart Helper and connect"),IsEnabled=!busy};restart.Click+=async(_,_)=>await Host.StartAsync();panel.Children.Add(restart);}
        var exit=new Button{Content=L.Text("安全退出应用","Quit safely"),IsEnabled=snapshot.CanExit && !busy};exit.Click+=async(_,_)=>await Run(async()=>
        {await Host.Client.MutateAsync((core,deadline,ct)=>core.MigrationRecoveryExitAsync(new(),deadline:deadline,cancellationToken:ct));Host.Close();});panel.Children.Add(exit);
    }
    private async Task ReadbackAndRestart()
    {
        await LoadAsync(true);if(disposed)return;
        if(recovery.Slot.State.Kind==LoadKind.Ready && recovery.Slot.State.Value?.Phase=="restart_required")await Host.StartAsync();
        else feedback.Text=L.Text("恢复状态已尝试读回；尚未确认可重启，保留数据库并核对。","Recovery readback attempted; restart is not confirmed. Keep the database and check the state.");
    }
    private async Task Run(Func<Task> action)
    {
        if(busy || disposed)return;busy=true;recovery.RefreshPresentation();
        try{await action();}
        catch(RpcException){if(!disposed){feedback.Text=L.Text("恢复操作未完成，正在重新读取状态。","Recovery incomplete. Reading the state again.");await LoadAsync(true);}}
        catch(IOException){if(!disposed){feedback.Text=L.Text("连接中断，恢复结果待确认；不会重发。","Connection interrupted; recovery result unconfirmed. No replay.");await LoadAsync(true);}}
        finally{busy=false;if(!disposed){await LoadAsync(true);recovery.RefreshPresentation();}}
    }
    public override void Cancel()=>recovery.Slot.Cancel();
    public override void Dispose(){disposed=true;confirmation=null;recovery.Dispose();}
}
