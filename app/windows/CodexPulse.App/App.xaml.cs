using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace CodexPulse.App;

public partial class App : Application
{
    private Window? window;

    public App() => InitializeComponent();

    protected override void OnLaunched(LaunchActivatedEventArgs args)
    {
        try { window = new MainWindow(); }
        catch (ArgumentException)
        {
            window = new Window { Title = "Codex Pulse", Content = new TextBlock { Text = "启动参数无效 / Invalid startup arguments", Margin = new Thickness(32), TextWrapping = TextWrapping.Wrap } };
        }
        window.Activate();
    }
}
