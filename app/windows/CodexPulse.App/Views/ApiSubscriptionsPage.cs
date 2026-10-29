using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace CodexPulse.App.Views;

internal sealed class ApiSubscriptionsPage:FeaturePage
{
    private readonly ApiSubscriptionsController controller;
    private readonly StackPanel content=new(){Spacing=20};
    private readonly ComboBox period=new(),currency=new();
    private string selectedPeriod="month",selectedCurrency="";
    private bool rendering,disposed;
    public ApiSubscriptionsPage(MainWindow host):base(host)
    {
        controller=host.ApiSubscriptions;
        Body.Children.Add(Text(L.Text("官方 API 账户信息，独立于 Agent Session、Token 与中心上报。金额和采样估算均由 Helper 提供。","Official API account information, independent of Agent sessions, tokens and center reporting. Amounts and sampled estimates come from the Helper.")));
        var actions=new StackPanel{Orientation=Orientation.Horizontal,Spacing=10};var refresh=new Button{Content=L.Text("刷新两来源","Refresh both sources")};refresh.Click+=async(_,_)=>await LoadAsync(true);var settings=new Button{Content=L.Text("管理 API Key","Manage API keys")};settings.Click+=(_,_)=>Host.ShowApiCredentials();actions.Children.Add(refresh);actions.Children.Add(settings);Body.Children.Add(actions);
        period.Header=L.Text("DeepSeek 自然周期","DeepSeek calendar period");foreach(var kind in new[]{"today","week","month"})period.Items.Add(new ComboBoxItem{Tag=kind,Content=ApiSubscriptionPresentation.Period(kind,L)});period.SelectedIndex=2;
        period.SelectionChanged+=(_,_)=>{if(!rendering && period.SelectedItem is ComboBoxItem{Tag:string value}){selectedPeriod=value;Render();}};
        currency.Header=L.Text("DeepSeek 图表币种","DeepSeek chart currency");currency.SelectionChanged+=(_,_)=>{if(!rendering && currency.SelectedItem is string value){selectedCurrency=value;Render();}};
        var selectors=new StackPanel{Orientation=Orientation.Horizontal,Spacing=12};selectors.Children.Add(period);selectors.Children.Add(currency);Body.Children.Add(selectors);Body.Children.Add(content);controller.Changed+=Changed;Render();
    }
    public override bool DependsOn(string domain)=>domain is "api_subscriptions" or "api_credentials";
    public override async Task LoadAsync(bool force=false){await controller.ReloadAsync();if(!disposed)Render();}
    private void Changed()=>Host.Dispatch(Render);
    private void Render()
    {
        if(disposed)return;rendering=true;
        try
        {
            content.Children.Clear();State(controller.Current.State.Kind);content.Children.Add(Text(L.State(controller.Current.State.Kind)));
            if(controller.Current.State.Value is not {} value)return;
            var currencies=ApiSubscriptionPresentation.Currencies(value);if(!currencies.Contains(selectedCurrency))selectedCurrency=currencies.FirstOrDefault()??"";currency.Items.Clear();foreach(var item in currencies)currency.Items.Add(item);currency.SelectedItem=selectedCurrency;currency.IsEnabled=currencies.Count>0;
            content.Children.Add(ApiSubscriptionCharts.Calendar(ApiSubscriptionPresentation.Activity(value.ActivityCalendar,selectedCurrency,L),value.ActivityCalendar?.ReportingTimeZone??"",L));
            var deep=new StackPanel{Spacing=12};deep.Children.Add(Text("DeepSeek", "SubtitleTextBlockStyle"));Status(deep,value.DeepSeek?.Status);
            if(ApiSubscriptionPresentation.HasFacts(value.DeepSeek?.Status) && value.DeepSeek?.Balance is {} balance)
            {
                deep.Children.Add(Text(balance.IsAvailable?L.Text("账户可用","Account available"):L.Text("账户当前不可用","Account currently unavailable")));
                deep.Children.Add(PresentationControls.Table([L.Text("币种","Currency"),L.Text("总余额","Total"),L.Text("赠送余额","Granted"),L.Text("充值余额","Topped up")],balance.Balances.Select(item=>new[]{item.Currency,item.Total,item.Granted,item.ToppedUp})));
                if(value.DeepSeek.Periods.FirstOrDefault(item=>item.Kind==selectedPeriod) is {} range)
                {
                    deep.Children.Add(Text(ApiSubscriptionPresentation.Period(selectedPeriod,L)+" · "+L.Text("自然周期起点 ","Calendar boundary ")+L.Timestamp(range.StartsAtMs)+"\n"+L.Text("实际首个成功样本 ","First successful sample ")+L.Timestamp(range.HasBaselineAtMs?range.BaselineAtMs:null)));
                    deep.Children.Add(PresentationControls.Table([L.Text("币种","Currency"),L.Text("期初余额","Starting balance"),L.Text("余额变化","Balance change"),L.Text("总充值（采样估算）","Recharge (sampled)"),L.Text("总消耗（采样估算）","Consumption (sampled)")],range.Changes.Select(item=>new[]{item.Currency,item.StartingTotal,item.TotalDelta,item.TotalRecharged,item.TotalConsumed})));
                    if(range.Series.FirstOrDefault(item=>item.Currency==selectedCurrency) is {} series)deep.Children.Add(ApiSubscriptionCharts.Balance(series,L));
                }
                deep.Children.Add(Text(L.Text("充值与消耗是相邻成功样本的估算，不能还原完整流水。采样空档不推断变化；图线仅连接保留的观察点，不延伸至当前时间。","Recharge and consumption are estimates between successful samples, not a full transaction ledger. Sampling gaps do not imply changes. The line connects retained observations and does not extend to now.")));
            }
            content.Children.Add(PresentationControls.Card(deep));
            var go=new StackPanel{Spacing=12};go.Children.Add(Text("OpenCode Go","SubtitleTextBlockStyle"));Status(go,value.OpenCodeGo?.Status);
            if(ApiSubscriptionPresentation.HasFacts(value.OpenCodeGo?.Status) && value.OpenCodeGo?.Quota is {} quota)
                foreach(var kind in new[]{"five_hour","weekly","monthly"})
                {
                    var window=quota.Windows.FirstOrDefault(item=>item.Kind==kind);if(window is null)continue;
                    var expired=DateTimeOffset.Now.ToUnixTimeMilliseconds()>=window.ResetsAtMs;
                    var remaining=expired?null:QuotaPresentation.Percent(true,window.RemainingPercent);
                    go.Children.Add(PresentationControls.Text(ApiSubscriptionPresentation.Window(kind,L)+" · "+L.Text("已用 ","Used ")+(expired?"—":window.UsedPercent.ToString("0.##",L.Culture)+"%")+" · "+L.Text("剩余 ","Remaining ")+QuotaPresentation.PercentText(remaining,L),18));QuotaPage.AddProgress(go,ApiSubscriptionPresentation.Window(kind,L),remaining);
                    go.Children.Add(Text((expired?L.Text("等待新周期","Awaiting new cycle"):window.Status=="rate-limited"?L.Text("当前限流","Currently rate limited"):L.Text("来源正常","Source OK"))+" · "+L.Text("重置 ","Resets ")+L.Timestamp(window.ResetsAtMs)));
                }
            content.Children.Add(PresentationControls.Card(go));
        }
        finally{rendering=false;}
    }
    private void Status(StackPanel panel,APISubscriptionSourceStatus? status)
    {panel.Children.Add(Text(ApiSubscriptionPresentation.Status(status,L)+" · "+L.Text("最后成功 ","Last success ")+L.Timestamp(status?.HasLastSuccessAtMs==true?status.LastSuccessAtMs:null)));if(status?.HasFailureCode==true)panel.Children.Add(Text(ApiSubscriptionPresentation.Failure(status.FailureCode,L)));}
    public override void Cancel()=>controller.Cancel();
    public override void Dispose(){disposed=true;controller.Changed-=Changed;Cancel();}
}
