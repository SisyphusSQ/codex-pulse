using CodexPulse.AppSupport;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace CodexPulse.App.Views;

internal sealed class StartupSection
{
    public StackPanel View{get;}=new(){Spacing=12};
    private readonly MainWindow host;
    private readonly StartupRegistration registration;
    private readonly TextBlock status=PresentationControls.Text("");
    private readonly Button enable=new(),disable=new();
    private Localization L=>host.Localization;
    public StartupSection(MainWindow host)
    {
        this.host=host;
        registration=new(new RegistryStartupStore(),"CodexPulse-"+WindowPreferences.Key(host.Options.Runtime).Split('\\')[^1],Environment.ProcessPath!,host.Options.Runtime,host.Options.Home);
        View.Children.Add(PresentationControls.Text(L.Text("登录启动只影响当前用户，并在后台打开当前安装的应用。系统禁用需在 Windows 启动应用设置中重新允许。","Login startup applies only to the current user and opens this installation in the background. Re-enable system-disabled entries in Windows startup settings.")));
        View.Children.Add(status);
        enable.Content=L.Text("启用登录启动…","Enable login startup…");disable.Content=L.Text("移除登录启动…","Remove login startup…");
        enable.Click+=async(_,_)=>await Set(true);disable.Click+=async(_,_)=>await Set(false);View.Children.Add(enable);View.Children.Add(disable);
        var reload=new Button{Content=L.Text("读取系统状态","Read system state")};reload.Click+=(_,_)=>Read();View.Children.Add(reload);
        View.Children.Add(new HyperlinkButton{Content=L.Text("打开 Windows 启动应用设置","Open Windows startup settings"),NavigateUri=new Uri("ms-settings:startupapps")});Read();
    }
    private string Label(StartupState value)=>value switch
    {
        StartupState.Off=>L.Text("未配置登录启动","Login startup is not configured"),
        StartupState.Configured=>L.Text("已配置登录启动（已从系统读回；实际启动仍由 Windows 管理）","Login startup configured (read back from the system; launch remains controlled by Windows)"),
        StartupState.DisabledBySystem=>L.Text("登录项存在，Windows 已禁用；请在系统设置中允许","Entry exists but Windows disabled it; allow it in system settings"),
        StartupState.DifferentCommand=>L.Text("登录项内容已变化；未自动覆盖","Startup command changed; it was not overwritten"),
        _=>L.Text("登录项已配置；系统许可状态未知","Startup configured; system approval status unknown")
    };
    public void Read()
    {
        try{var value=registration.Read();status.Text=Label(value);enable.IsEnabled=value==StartupState.Off;disable.IsEnabled=value is StartupState.Configured or StartupState.DisabledBySystem or StartupState.ApprovalUnknown;}
        catch(Exception e)when(e is IOException or UnauthorizedAccessException or System.Security.SecurityException){status.Text=L.Text("无法读取登录启动状态。","Cannot read login startup state.");enable.IsEnabled=disable.IsEnabled=false;}
    }
    private async Task Set(bool enabled)
    {
        if(!await ProductDialog.Confirm(host,L.Text("更改 Windows 登录启动","Change Windows login startup"),enabled?L.Text("仅为当前用户添加此 runtime 的登录启动项，不启用独立后台服务。","Add a startup entry for this runtime and current user. No independent service is installed."):L.Text("仅移除此 runtime 的登录项。","Remove only this runtime's startup entry."),enabled?L.Text("启用并读回","Enable and read back"):L.Text("移除并读回","Remove and read back")))return;
        try{status.Text=Label(registration.Set(enabled));}
        catch(Exception e)when(e is IOException or UnauthorizedAccessException or System.Security.SecurityException){status.Text=L.Text("登录项修改未确认，请重新读取。","Startup change not confirmed. Read system state again.");return;}
        Read();
    }
}
