using CodexPulse.AppSupport;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace CodexPulse.App.Views;

internal sealed class WindowsUpdatesSection:IDisposable
{
    private readonly MainWindow host;
    private Localization L=>host.Localization;
    public StackPanel View{get;}=new(){Spacing=12};
    private readonly TextBlock status=PresentationControls.Text(""),detail=PresentationControls.Caption("");
    private readonly Button check=new(),install=new(),cancel=new();
    private readonly ProgressBar progress=new(){Minimum=0,Maximum=100};
    private bool disposed;
    public WindowsUpdatesSection(MainWindow host)
    {
        this.host=host;View.Children.Add(status);View.Children.Add(detail);View.Children.Add(progress);
        check.Content=L.Text("检查 Windows 更新","Check Windows updates");check.Click+=async(_,_)=>await host.WindowsUpdates.CheckAsync();
        install.Content=L.Text("下载并安装已签名更新…","Download and install signed update…");install.Click+=async(_,_)=>
        {
            if(host.WindowsUpdates.Candidate is not {} candidate)return;
            if(await ProductDialog.Confirm(host,L.Text("安装 Windows 更新","Install Windows update"),candidate.Manifest.Version+"\n"+L.Text("将从固定 HTTPS 来源下载并核对包签名及全部文件。只有 App 与 Helper 干净退出才会切换版本；runtime 数据保留。","Download from the fixed HTTPS origin and verify the package signature and every file. Switch versions only after clean App/Helper shutdown; retain runtime data."),L.Text("下载并安装","Download and install")) && !disposed)await host.WindowsUpdates.InstallAsync();
        };
        cancel.Content=L.Text("取消下载/检查","Cancel download/check");cancel.Click+=(_,_)=>host.WindowsUpdates.Cancel();
        var actions=new StackPanel{Orientation=Orientation.Horizontal,Spacing=8};actions.Children.Add(check);actions.Children.Add(install);actions.Children.Add(cancel);
        View.Children.Add(new ScrollViewer{Content=actions,HorizontalScrollBarVisibility=ScrollBarVisibility.Auto,VerticalScrollBarVisibility=ScrollBarVisibility.Disabled});host.WindowsUpdates.Changed+=Changed;Publish();
    }
    private void Changed()=>host.Dispatch(Publish);
    private void Publish()
    {
        if(disposed)return;var value=host.WindowsUpdates;
        status.Text=value.Kind switch{WindowsUpdateKind.Development=>L.Text("当前为未安装的开发构建。","This is an uninstalled development build."),WindowsUpdateKind.Unconfigured=>L.Text("此安装尚未配置受信任更新源。","This installation has no trusted update source configured."),WindowsUpdateKind.Checking=>L.Text("正在检查签名版本信息…","Checking signed version information…"),WindowsUpdateKind.Current=>L.Text("当前通道已是最新版本。","Up to date on this channel."),WindowsUpdateKind.Available=>L.Text("可用版本：","Available version: ")+value.Candidate?.Manifest.Version,WindowsUpdateKind.Downloading=>L.Text("正在下载并验证…","Downloading and verifying…"),WindowsUpdateKind.Preparing=>L.Text("正在核对全部文件与同包版本…","Verifying all files and the paired bundle…"),WindowsUpdateKind.Handoff=>L.Text("正在安全退出，更新执行器将继续。","Shutting down safely; the update worker will continue."),WindowsUpdateKind.Cancelled=>L.Text("本次操作已取消。","Operation cancelled."),WindowsUpdateKind.Failed=>L.Text("更新未完成，当前程序与数据保持保留。请检查来源或重试。","Update incomplete. Programs and data are retained. Check the source or retry."),_=>L.Text("可以手动检查更新。","Ready to check for updates.")};
        detail.Text="Windows x64 · "+host.BundleVersion+(value.Installed is {} current?" · build "+current.Version.BuildNumber+" · "+current.Release.Manifest.CoreContract+"\n"+current.Release.Manifest.DataCompatibility:"")+"\n"+(value.DevelopmentTrust?L.Text("受控开发包签名；不代表 Windows 公信任签名或公开更新已经验收。","Controlled development package signature; Windows public code-signing trust and public updates are not verified."):L.Text("更新使用安装内置签名根；Windows 代码签名是独立门禁。","Updates use the installation's pinned signing root. Windows code signing is a separate gate."));
        if(value.Error=="update_changed_confirm_again")status.Text=L.Text("版本信息已变化，请再次检查并确认新版本。","Release information changed. Check and confirm the new version.");
        if(value.LastOutcome is {} outcome && outcome.State is not("ready" or "installed" or "uninstalled"))detail.Text+="\n"+L.Text("上次更新未完成或需要恢复。已保留程序版本和 runtime；请重试，或使用 Setup 的显式回退/恢复说明。数据库不会自动降级。","The previous update was incomplete or requires recovery. Program versions and runtime were retained. Retry, or follow Setup's explicit rollback/recovery instructions. The database is never downgraded automatically.");
        check.IsEnabled=value.Configured && !value.Busy;install.IsEnabled=!value.Busy && value.Kind==WindowsUpdateKind.Available && value.Candidate is not null;cancel.IsEnabled=value.Busy && value.Kind is WindowsUpdateKind.Checking or WindowsUpdateKind.Downloading;progress.Value=value.Progress*100;
        install.Visibility=value.Candidate is not null?Visibility.Visible:Visibility.Collapsed;
        cancel.Visibility=cancel.IsEnabled?Visibility.Visible:Visibility.Collapsed;
        progress.Visibility=value.Busy?Visibility.Visible:Visibility.Collapsed;
        progress.IsIndeterminate=value.Kind is WindowsUpdateKind.Checking or WindowsUpdateKind.Preparing or WindowsUpdateKind.Handoff;
    }
    public void Dispose(){disposed=true;host.WindowsUpdates.Changed-=Changed;}
}
