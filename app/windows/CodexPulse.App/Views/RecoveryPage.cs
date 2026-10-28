using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Grpc.Core;
using Microsoft.UI.Xaml.Controls;

namespace CodexPulse.App.Views;

internal sealed class RecoveryPage(MainWindow host) : FeaturePage(host)
{
    public override bool DependsOn(string domain) => false;
    public override Task LoadAsync(bool force = false)
    {
        Body.Children.Clear();
        var snapshot = Host.Session?.Bootstrap.Recovery;
        Body.Children.Add(Text(L.Text("数据库恢复", "Database recovery"), "SubtitleTextBlockStyle"));
        Body.Children.Add(Text(L.Text("本机数据库尚未打开。完成恢复后重新连接，或安全退出应用。", "The local database is not open. Reconnect after recovery or quit safely.")));
        if (snapshot is not null)
            Body.Children.Add(Text(L.Text("数据库版本", "Database version") + $" {snapshot.CurrentVersion} → {snapshot.TargetVersion}"));
        var retry = new Button { Content = L.Text("重试并重新连接", "Retry and reconnect"), IsEnabled = snapshot?.CanRetry == true };
        retry.Click += async (_, _) =>
        {
            retry.IsEnabled = false;
            try
            {
                var result = await Host.Client.MutateAsync((client, deadline, cancellation) =>
                    client.MigrationRecoveryRetryAsync(new MigrationRecoveryRetryRequest(), deadline: deadline, cancellationToken: cancellation));
                var state = await Host.Session!.Connection.Raw.MigrationRecoveryStateAsync(new MigrationRecoveryStateRequest(), deadline: DateTime.UtcNow.AddSeconds(30));
                if (result.Receipt?.RestartRequired == true || state.Phase == "restart_required") await Host.StartAsync();
                else Host.ShowNotice("恢复尚未完成，请保留当前数据库并稍后重试。", "Recovery has not completed. Keep the current database and retry later.");
            }
            catch (RpcException) { Host.ShowNotice("恢复未完成，可重试或退出。", "Recovery has not completed. Retry or quit."); }
            finally { retry.IsEnabled = snapshot?.CanRetry == true; }
        };
        Body.Children.Add(retry);
        var exit = new Button { Content = L.Text("退出应用", "Quit app"), IsEnabled = snapshot?.CanExit != false };
        exit.Click += (_, _) => Host.Close();
        Body.Children.Add(exit);
        State(LoadKind.RecoveryBlocked);
        return Task.CompletedTask;
    }
}
