using System.Text;
using CodexPulse.AppSupport;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace CodexPulse.App.Views;

internal sealed class ApiCredentialsSection:IDisposable
{
    private readonly MainWindow host;
    private Localization L=>host.Localization;
    private ApiSubscriptionsController Controller=>host.ApiSubscriptions;
    public StackPanel View{get;}=new(){Spacing=12};
    private readonly TextBlock feedback=new(){TextWrapping=TextWrapping.Wrap};
    private readonly List<(string Service,TextBlock Status,PasswordBox Input,Button Save,Button Delete)> controls=[];
    private bool disposed;
    public ApiCredentialsSection(MainWindow host)
    {
        this.host=host;
        View.Children.Add(PresentationControls.Text(L.Text("两个来源分别配置，独立于 Agent 账本。密钥仅保存在 Helper 的 ACL 私有 credentials.db（明文 SQLite，非静态加密）；保存或删除后会安全重启本机服务。","Each source is configured independently of Agent usage. Keys are stored only in the Helper's ACL-private credentials.db (plaintext SQLite, without encryption at rest). Saving or deleting safely restarts the local service.")));
        foreach(var (service,name) in new[]{("deepseek","DeepSeek"),("opencode_go","OpenCode Go")})
        {
            var panel=new StackPanel{Spacing=8};panel.Children.Add(PresentationControls.Text(name,18));var status=PresentationControls.Text("");panel.Children.Add(status);
            var input=new PasswordBox{Header=name+" API Key",MaxLength=4096,PasswordRevealMode=PasswordRevealMode.Hidden};panel.Children.Add(input);
            var save=new Button{Content=L.Text("保存并重启本机服务","Save and restart local service")};
            save.Click+=async(_,_)=>{if(Controller.Busy || input.Password.Length==0)return;var bytes=Encoding.UTF8.GetBytes(input.Password);input.Password="";try{await Controller.UpdateAsync(service,bytes);}catch(ArgumentException){feedback.Text=L.Text("密钥须为 1–4096 字节。","The key must contain 1–4096 bytes.");}};
            var delete=new Button{Content=L.Text("删除密钥并重启…","Delete key and restart…")};
            delete.Click+=async(_,_)=>{input.Password="";if(await ProductDialog.Confirm(host,L.Text("删除 API Key","Delete API key"),name+L.Text(" 将停止新请求；本来源历史按凭据代次隔离，不再展示旧密钥数据。"," will stop making requests. History is isolated by credential generation; data for the old key will no longer be displayed."),L.Text("删除并重启","Delete and restart")) && !disposed)await Controller.UpdateAsync(service,[],true);};
            var actions=new StackPanel{Orientation=Orientation.Horizontal,Spacing=8};actions.Children.Add(save);actions.Children.Add(delete);panel.Children.Add(actions);View.Children.Add(PresentationControls.Card(panel));controls.Add((service,status,input,save,delete));
        }
        View.Children.Add(feedback);Controller.Changed+=Changed;Publish();
    }
    public Task LoadAsync()=>Controller.ReloadStatusAsync();
    private void Changed()=>host.Dispatch(Publish);
    private void Publish()
    {
        if(disposed)return;
        foreach(var item in controls){var status=Controller.Credentials.State;item.Status.Text=status.Value is {} value?ApiSubscriptionsController.Configured(item.Service,value)?L.Text("已配置（不读取已保存密钥）","Configured (saved key cannot be read back)"):L.Text("未配置","Not configured"):L.State(status.Kind);item.Input.IsEnabled=item.Save.IsEnabled=!Controller.Busy;item.Delete.IsEnabled=!Controller.Busy && status.Value is {} current && ApiSubscriptionsController.Configured(item.Service,current);}
        feedback.Text=Controller.Action switch{ApiCredentialAction.Saving=>L.Text("正在保存、重启并读回配置…","Saving, restarting and reading back configuration…"),ApiCredentialAction.Applied=>L.Text("已保存，已重启并读回配置状态。","Saved; restarted and configuration read back."),ApiCredentialAction.ReadbackConfirmed=>L.Text("回执未知；重启后的状态已确认删除。","Receipt unknown; state after restart confirms deletion."),ApiCredentialAction.Unknown=>L.Text("保存结果未确认；不会自动重发。配置标记无法证明新密钥内容，请核对来源状态。","Save is unconfirmed and will not be replayed. A configured flag cannot prove the new key's contents. Check source status."),ApiCredentialAction.RestartRequired=>L.Text("凭据可能已保存，但本机服务未完成安全重启。请重新连接后读回；旧代次数据已隐藏。","Credentials may be saved, but the local service did not safely restart. Reconnect and read back. Old-generation data is hidden."),ApiCredentialAction.Unavailable=>L.Text("凭据操作未完成，请重查配置。","Credential operation incomplete. Reload configuration."),_=>""};
    }
    public void Dispose(){disposed=true;Controller.Changed-=Changed;foreach(var item in controls)item.Input.Password="";}
}
