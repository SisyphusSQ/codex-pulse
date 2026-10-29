using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace CodexPulse.App.Views;

internal sealed class AccountQuotasPage:FeaturePage
{
    private readonly DataSection<CodexAccountQuotasResponse> accounts;
    private bool disposed;
    public AccountQuotasPage(MainWindow host):base(host)
    {
        Body.Children.Add(PresentationControls.Caption(L.Text("查看当前账号与历史账号最后采集的额度。历史快照只读，不会触发在线刷新。","View the last collected quota for current and previous accounts. Historical snapshots are read-only and do not refresh online.")));
        accounts=new(host,L.Text("账号额度快照","Account quota snapshots"),Render,"account-quotas",showHeader:false);Body.Children.Add(accounts.View);
        var manage=new Button{Content=L.Text("管理订阅资料与保留策略","Manage subscription details and retention")};manage.Click+=(_,_)=>Host.Navigate(Feature.Settings);Body.Children.Add(manage);
    }
    public override bool DependsOn(string domain)=>domain is "quota" or "quota_codex" or "account" or "settings";
    public override async Task LoadAsync(bool force=false)
    {
        var context=Host.Workspace.Context with{Provider="codex"};var request=new CodexAccountQuotasRequest{EvaluatedAtMs=DateTimeOffset.Now.ToUnixTimeMilliseconds(),TimeZone=context.TimeZone};
        await accounts.Slot.LoadAsync(context.Generation,ct=>Host.Client.QueryAsync("ListCodexAccountQuotas",request,context,["quota","account","settings"],(core,deadline,cancel)=>core.ListCodexAccountQuotasAsync(request,deadline:deadline,cancellationToken:cancel),ct,force),r=>DashboardPresentation.Kind(r.Meta,r.Accounts.Count==0));
        if(!disposed)State(accounts.Slot.State.Kind);
    }
    private void Render(StackPanel panel,CodexAccountQuotasResponse response)
    {
        if(response.Accounts.Count==0){panel.Children.Add(Text(L.Text("尚无已确认且包含额度事实的账号。手工资料不会形成额度卡片。","No confirmed accounts with quota facts yet. Manual-only profiles do not create quota cards.")));return;}
        var grid=new Grid{ColumnSpacing=16,RowSpacing=16};
        foreach(var snapshot in response.Accounts)
        {
            var account=snapshot.Account;var body=new StackPanel{Spacing=10};
            body.Children.Add(PresentationControls.Caption(account.Current?L.Text("当前账号","Current account"):L.Text("历史快照 · 只读","Historical snapshot · Read-only")));
            body.Children.Add(PresentationControls.Heading(QuotaPresentation.Plan(account.HasResolvedPlan?account.ResolvedPlan:null,L)));
            var identity=Text(QuotaPresentation.AccountTitle(account,L));identity.TextWrapping=TextWrapping.NoWrap;identity.TextTrimming=TextTrimming.CharacterEllipsis;ToolTipService.SetToolTip(identity,identity.Text);body.Children.Add(identity);
            if(account.HasDisplayEmail && account.DisplayEmail!=identity.Text)body.Children.Add(PresentationControls.Caption(account.DisplayEmail));
            body.Children.Add(PresentationControls.Caption(QuotaPresentation.SubscriptionDate(account,L)));
            foreach(var window in QuotaPresentation.Windows(snapshot,response.EvaluatedAtMs))
            {
                var title=QuotaPresentation.WindowName(window.LimitId,null,window.HasWindowMinutes?window.WindowMinutes:null,L);
                var expired=window.HasResetsAtMs && window.ResetsAtMs<=response.EvaluatedAtMs;
                var remaining=QuotaPresentation.Percent(window.HasRemainingPercent,window.RemainingPercent);
                body.Children.Add(PresentationControls.Caption(title+" · "+(expired?L.Text("已结束 · 上次剩余","Ended · Last remaining"):L.Text("剩余","Remaining"))));
                var number=Text(QuotaPresentation.PercentText(remaining,L));number.FontSize=28;number.FontWeight=Microsoft.UI.Text.FontWeights.SemiBold;body.Children.Add(number);
                if(!expired)QuotaPage.AddProgress(body,title,remaining);
                body.Children.Add(PresentationControls.Caption(L.Text("重置 ","Resets ")+L.Timestamp(window.HasResetsAtMs?window.ResetsAtMs:null)+" · "+QuotaPresentation.Freshness(window.Freshness,L)));
                if(window.Conflict is not("" or "none"))body.Children.Add(Text(L.Text("来源冲突：","Source conflict: ")+window.Conflict));
            }
            body.Children.Add(PresentationControls.Caption(L.Text("采集时间 ","Collected ")+L.Timestamp(snapshot.HasLastCollectedAtMs?snapshot.LastCollectedAtMs:null)));
            if(account.Current)
            {
                var refresh=new Button{Content=L.Text("刷新当前账号额度","Refresh current account quota")};
                refresh.Click+=async(_,_)=>
                {
                    refresh.IsEnabled=false;
                    try
                    {
                        var request=new QuotaRefreshRequest{Provider=new(){Provider="codex"},Source="quota"};var result=await Host.Client.MutateAsync((core,deadline,ct)=>core.RequestQuotaRefreshAsync(request,deadline:deadline,cancellationToken:ct));
                        Host.ShowNotice(result.Receipt?.Fetched==true?"已取得新数据，正在读取快照。":"本次未确认取得新数据，正在重新读取快照。",result.Receipt?.Fetched==true?"New data fetched; reading snapshots.":"New data was not confirmed; reading snapshots.",InfoBarSeverity.Informational);
                        if(!disposed)await LoadAsync(true);
                    }
                    catch(Grpc.Core.RpcException){Host.ShowNotice("刷新未完成，请核对最新快照。","Refresh incomplete. Check the latest snapshot.");}
                    catch(IOException){Host.ShowNotice("连接中断，刷新结果待确认。","Connection interrupted; refresh result unconfirmed.");}
                    finally{if(!disposed)refresh.IsEnabled=true;}
                };body.Children.Add(refresh);
            }
            var card=PresentationControls.Card(body);card.VerticalAlignment=VerticalAlignment.Top;grid.Children.Add(card);
        }
        void Layout(double width)
        {
            var count=Math.Max(1,Math.Min(grid.Children.Count,(int)((width+16)/316)));grid.ColumnDefinitions.Clear();grid.RowDefinitions.Clear();
            for(var i=0;i<count;i++)grid.ColumnDefinitions.Add(new(){Width=new GridLength(Math.Max(0,Math.Min(340,(width-16*(count-1))/count)))});
            for(var i=0;i<(grid.Children.Count+count-1)/count;i++)grid.RowDefinitions.Add(new(){Height=GridLength.Auto});
            for(var i=0;i<grid.Children.Count;i++){Grid.SetColumn((FrameworkElement)grid.Children[i],i%count);Grid.SetRow((FrameworkElement)grid.Children[i],i/count);}
        }
        grid.SizeChanged+=(_,args)=>Layout(args.NewSize.Width);Layout(700);panel.Children.Add(grid);
    }
    public override void Cancel()=>accounts.Slot.Cancel();
    public override void Dispose(){disposed=true;accounts.Dispose();}
}
