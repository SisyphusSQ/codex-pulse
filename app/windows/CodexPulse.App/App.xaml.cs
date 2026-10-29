using System.Security.Cryptography;
using System.Text;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;
using Microsoft.Windows.AppLifecycle;

namespace CodexPulse.App;

public partial class App : Application
{
    private Window? window;
    private AppInstance? instance;
    private volatile bool pendingActivation;

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
            instance.Activated+=(_,_)=>{if(window is MainWindow main)main.Dispatch(main.ShowMain);else pendingActivation=true;};
            var host=new MainWindow(options);window=host;
            await host.StartHostedAsync();
            if(pendingActivation)host.ShowMain();
            return;
        }
        catch (ArgumentException)
        {
            window = new Window { Title = "Codex Pulse", Content = new TextBlock { Text = "启动参数无效 / Invalid startup arguments", Margin = new Thickness(32), TextWrapping = TextWrapping.Wrap } };
        }
        window.Activate();
    }
}
