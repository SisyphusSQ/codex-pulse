using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace CodexPulse.App.Views;

internal sealed class DashboardPage : FeaturePage
{
    private readonly DataSection<DashboardSummaryResponse> summary;
    private DashboardSummaryResponse? last;
    private string activityProvider="";
    private StackPanel activity=new(){Spacing=12};
    private StackPanel distribution=new(){Spacing=12};
    private bool costDistribution;
    public DashboardPage(MainWindow host):base(host)
    {
        Body.Children.Add(Text(L.Text("跨客户端查看 Token、API 等价估算成本与独立额度。","Compare tokens, API-equivalent estimates, and independent client quotas.")));
        summary=new(host,L.Text("当前范围汇总","Current range summary"),Render,"dashboard-summary");
        Body.Children.Add(summary.View);
    }
    public override bool DependsOn(string domain)=>domain is "index" or "settings" or "account" or "quota" or "quota_codex" or "quota_cursor" or "quota_grok";
    public override async Task LoadAsync(bool force=false)
    {
        var context=Host.Workspace.Context with { Provider="" };
        var request=new DashboardSummaryRequest { Range=DashboardPresentation.LocalRange(context), Granularity=context.Preset=="today"?"hour":"day",
            ActivityRange=DashboardPresentation.AnnualRange(context.TimeZone,DateOnly.FromDateTime(DateTime.Today)) };
        await summary.Slot.LoadAsync(context,token=>Host.Client.QueryAsync("DashboardSummary",request,context,["index","quota","quota_codex","quota_cursor","quota_grok","settings","account"],
            (client,deadline,cancel)=>client.DashboardSummaryAsync(request,deadline:deadline,cancellationToken:cancel),token,force),value=>DashboardPresentation.Kind(value.Meta,value.Coverage?.OverallState=="empty"));
        State(summary.Slot.State.Kind);
    }
    private void Render(StackPanel panel,DashboardSummaryResponse response)
    {
        last=response;
        activity=new(){Spacing=12};distribution=new(){Spacing=12};
        var coverage=response.Coverage;
        panel.Children.Add(Text(L.Text("统计范围 ","Range ")+L.Timestamp(response.Range?.StartAtMs)+" — "+L.Timestamp(response.Range?.EndAtMs)+" · "+response.ReportingTimeZone));
        panel.Children.Add(PresentationControls.Metrics(
            (L.Text("已知 Token","Known tokens")+$" ({coverage?.KnownProviderCount}/{coverage?.TotalProviderCount})",L.Number(response.Totals?.TotalTokens)),
            (L.Text("API 等价估算成本","API-equivalent estimate")+$" ({coverage?.KnownCostProviderCount}/{coverage?.TotalProviderCount})",L.Money(response.Totals?.EstimatedUsdMicros)),
            (L.Text("活跃客户端","Active clients"),L.Number(response.Totals?.ActiveProviderCount))));
        panel.Children.Add(Text(L.Text("Token 覆盖：","Token coverage: ")+(coverage?.TokenState??"unknown")+" · "+L.Text("成本覆盖：","Cost coverage: ")+(coverage?.CostState??"unknown")));
        var activityCard=new StackPanel { Spacing=12 };
        activityCard.Children.Add(Text(L.Text("年度 Token 活动 · 独立的最近 365 天","Annual token activity · Independent last 365 days"),"SubtitleTextBlockStyle"));
        var picker=new ComboBox { Header=L.Text("年度活动客户端","Annual activity client"),MinWidth=180 };
        picker.Items.Add(new ComboBoxItem { Tag="",Content=L.Text("全部客户端","All clients") });
        foreach(var provider in Host.Workspace.EnabledProviders) picker.Items.Add(new ComboBoxItem{Tag=provider,Content=MainWindow.ProviderName(provider)});
        picker.SelectedItem=picker.Items.Cast<ComboBoxItem>().FirstOrDefault(item=>(string)item.Tag==activityProvider)??picker.Items[0];
        picker.SelectionChanged+=(_,_)=>{ if(picker.SelectedItem is ComboBoxItem {Tag:string provider}) {activityProvider=provider;RenderActivity();} };
        activityCard.Children.Add(picker);activityCard.Children.Add(activity);panel.Children.Add(PresentationControls.Card(activityCard));RenderActivity();

        panel.Children.Add(Text(L.Text("各客户端额度","Client quotas"),"SubtitleTextBlockStyle"));
        foreach(var quota in response.Quotas)
        {
            var card=new StackPanel {Spacing=12};
            var link=new Button { Content=MainWindow.ProviderName(quota.Provider)+" · "+L.Text("打开概览","Open overview") };
            link.Click+=(_,_)=>Host.OpenProvider(quota.Provider);card.Children.Add(link);
            card.Children.Add(QuotaSummary.Create(quota.Current,quota.Provider,L));panel.Children.Add(PresentationControls.Card(card));
        }
        panel.Children.Add(Text(L.Text("Token 趋势 · 按客户端","Token trend · By client"),"SubtitleTextBlockStyle"));
        panel.Children.Add(Charts.Bars(response.Trend.Select(point=>new ChartDatum(point.Key,DashboardPresentation.Value(point.TotalTokens),
            DashboardPresentation.Reconciled(DashboardPresentation.Value(point.TotalTokens),point.Shares.Select(share=>DashboardPresentation.Value(share.TotalTokens) is {} value?new ChartSegment(MainWindow.ProviderName(share.Provider),value,share.Provider):null)))).ToArray(),L,"Token",true));
        var metric=new ComboBox { Header=L.Text("构成指标","Distribution metric"),MinWidth=180 };
        metric.Items.Add(L.Text("Token","Tokens"));metric.Items.Add(L.Text("API 等价估算成本","API-equivalent estimate"));metric.SelectedIndex=costDistribution?1:0;
        metric.SelectionChanged+=(_,_)=>{costDistribution=metric.SelectedIndex==1;RenderDistribution();};panel.Children.Add(metric);panel.Children.Add(distribution);RenderDistribution();
        panel.Children.Add(Text(L.Text("客户端明细 · 估算与官方金额独立","Client details · Estimates and reported amounts are separate"),"SubtitleTextBlockStyle"));
        panel.Children.Add(PresentationControls.Table([L.Text("客户端","Client"),"Token",L.Text("API 等价估算","API estimate"),L.Text("官方金额","Reported amount"),L.Text("覆盖","Coverage")],
            response.Providers.Select(provider=>new[]{MainWindow.ProviderName(provider.Provider),L.Number(provider.Totals?.TotalTokens),L.Money(provider.Totals?.EstimatedUsdMicros),L.Money(provider.ReportedUsdMicros),provider.CoverageState+" / "+provider.CostState})));
        if(response.Providers.Count==0) panel.Children.Add(Text(L.Text("尚未启用客户端，可在设置中启用来源。","No clients are enabled. Enable a source in Settings.")));
    }
    private void RenderActivity()
    {
        activity.Children.Clear();if(last is null)return;
        var complete=DashboardPresentation.Complete(activityProvider==""?last.ActivityCoverageState:last.ActivityProviderCoverage.FirstOrDefault(value=>value.Provider==activityProvider)?.CoverageState);
        var days=DashboardPresentation.Calendar(last.Activity.Select(point=>((NumericValue?)point.StartAtMs,activityProvider==""?point.TotalTokens:point.Shares.FirstOrDefault(share=>share.Provider==activityProvider)?.TotalTokens,(NumericValue?)null)),DateOnly.FromDateTime(DateTime.Today),last.ReportingTimeZone,complete);
        activity.Children.Add(Text(L.Text("年度覆盖：","Annual coverage: ")+(complete?L.Text("完整","Complete"):L.Text("部分或未知；缺失日不计零","Partial or unknown; missing days remain unknown"))));
        activity.Children.Add(Charts.Calendar(days,L));
    }
    private void RenderDistribution()
    {
        distribution.Children.Clear();if(last is null)return;
        distribution.Children.Add(Text(L.Text("工具分布","Client distribution"),"SubtitleTextBlockStyle"));
        var total=DashboardPresentation.Value(costDistribution?last.Totals?.EstimatedUsdMicros:last.Totals?.TotalTokens);
        var shares=last.Distribution.Select(share=>(Share:share,Value:DashboardPresentation.Value(costDistribution?share.EstimatedUsdMicros:share.TotalTokens))).ToArray();
        if(shares.Any(share=>share.Value is null)) distribution.Children.Add(Text(L.Text("有客户端的构成数值未知。","Some client shares are unknown.")));
        else distribution.Children.Add(Charts.Distribution(shares.Select(share=>new ChartSegment(MainWindow.ProviderName(share.Share.Provider),share.Value!.Value,share.Share.Provider)).ToArray(),total,L,costDistribution));
        distribution.Children.Add(Text(L.Text("工具与模型用量 · 模型分布","Client and model usage · Model distribution"),"SubtitleTextBlockStyle"));
        var models=last.Models.Select(model=>(Model:model,Value:DashboardPresentation.Value(costDistribution?model.Totals?.EstimatedUsdMicros:model.Totals?.TotalTokens))).ToArray();
        if(models.Any(model=>model.Value is null) || total is null) {distribution.Children.Add(Text(L.Text("模型构成未知。","Model breakdown is unknown.")));return;}
        var entries=models.Select(model=>new ChartSegment(MainWindow.ProviderName(model.Model.Provider)+" · "+DashboardPresentation.Name(model.Model.Model,L.Text("未归类模型","Unattributed model")),model.Value!.Value,model.Model.Provider+":"+model.Model.DimensionKey)).ToList();
        var known=entries.Aggregate(System.Numerics.BigInteger.Zero,(sum,item)=>sum+item.Value);
        if(known<=total && known>=0 && total-known<=long.MaxValue)entries.Add(new(L.Text("其他已知模型","Other known models"),(long)(total.Value-known),"other"));
        distribution.Children.Add(Charts.Distribution(entries,total,L,costDistribution));
    }
    public override void Cancel()=>summary.Slot.Cancel();
    public override void Dispose()=>summary.Dispose();
}
