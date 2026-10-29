using System.Security.Cryptography;
using System.Text;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.Windows.AppLifecycle;
using CodexPulse.CoreClient;
using CodexPulse.Distribution;

namespace CodexPulse.App;

public partial class App : Application
{
    private Window? window;
    private AppInstance? instance;
    private volatile bool pendingActivation;
    private string? pendingUpdateNonce;

    public App() => InitializeComponent();

    protected override async void OnLaunched(LaunchActivatedEventArgs args)
    {
        try
        {
            var options=PlatformOptions.Parse();
            var key=Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(options.Runtime.ToUpperInvariant())));
            instance=AppInstance.FindOrRegisterForKey("CodexPulse-"+key);
            if(!instance.IsCurrent)
            {
                await instance.RedirectActivationToAsync(AppInstance.GetCurrent().GetActivatedEventArgs());
                Exit();return;
            }
            if(options.UpdateNonce is {} idleNonce){UpdateRuntime.IdleShutdown(options.Runtime,idleNonce);Exit();return;}
            instance.Activated+=(_,activation)=>
            {
                var nonce=activation.Data is Windows.ApplicationModel.Activation.ILaunchActivatedEventArgs launch?WindowsCommandLine.UpdateNonce(launch.Arguments):null;
                if(window is MainWindow main)main.Dispatch(()=>{if(nonce is not null)_=main.QuitForUpdateAsync(nonce);else main.ShowMain();});
                else if(nonce is not null)pendingUpdateNonce=nonce;else pendingActivation=true;
            };
            var host=new MainWindow(options);window=host;
            await host.StartHostedAsync();
            if(pendingUpdateNonce is {} pending)await host.QuitForUpdateAsync(pending);else if(pendingActivation)host.ShowMain();
            return;
        }
        catch (ArgumentException)
        {
            window = new Window { Title = "Codex Pulse", Content = new TextBlock { Text = "启动参数无效 / Invalid startup arguments", Margin = new Thickness(32), TextWrapping = TextWrapping.Wrap } };
        }
        window.Activate();
    }
}
