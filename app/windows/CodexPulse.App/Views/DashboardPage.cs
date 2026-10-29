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
    private StackPanel trendPlot=new(){Spacing=12};
    private bool costDistribution;
    private bool stackedTrend;
    public DashboardPage(MainWindow host):base(host)
    {
        summary=new(host,L.Text("当前范围汇总","Current range summary"),Render,"dashboard-summary",showHeader:false,showStatus:false);
        Body.Children.Add(summary.View);
    }
    public override bool DependsOn(string domain)=>domain is "index" or "settings" or "account" or "quota" or "quota_codex" or "quota_cursor" or "quota_grok";
    public override async Task LoadAsync(bool force=false)
    {
        State(LoadKind.Loading);
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
        activity=new(){Spacing=12};distribution=new(){Spacing=12};trendPlot=new(){Spacing=12};
        var coverage=response.Coverage;
        var totals=new StackPanel {Spacing=12};
        totals.Children.Add(PresentationControls.MetricStrip(
            (L.Text("已知 Token","Known tokens")+$" ({coverage?.KnownProviderCount}/{coverage?.TotalProviderCount})",L.Tokens(response.Totals?.TotalTokens)),
            (L.Text("API 等价估算成本","API-equivalent estimate")+$" ({coverage?.KnownCostProviderCount}/{coverage?.TotalProviderCount})",L.Money(response.Totals?.EstimatedUsdMicros)),
            (L.Text("活跃客户端","Active clients"),L.Number(response.Totals?.ActiveProviderCount))));
        totals.Children.Add(PresentationControls.Caption(L.Text("Token 覆盖：","Token coverage: ")+Coverage(coverage?.TokenState)+" · "+L.Text("成本覆盖：","Cost coverage: ")+Coverage(coverage?.CostState)));
        var totalsCard=PresentationControls.Card(totals);totalsCard.VerticalAlignment=VerticalAlignment.Top;
        var quotaCards=new List<UIElement>();
        foreach(var quota in response.Quotas)
        {
            var card=new StackPanel {Spacing=12};
            var link=new HyperlinkButton { Content=MainWindow.ProviderName(quota.Provider), Padding=new Thickness(0), HorizontalAlignment=HorizontalAlignment.Left };
            ToolTipService.SetToolTip(link,L.Text("打开客户端概览","Open client overview"));
            link.Click+=(_,_)=>Host.OpenProvider(quota.Provider);card.Children.Add(link);
            card.Children.Add(QuotaSummary.Create(quota.Current,quota.Provider,L,compact:true));quotaCards.Add(PresentationControls.Card(card));
        }
        if(quotaCards.Count>0) panel.Children.Add(PresentationControls.Split(totalsCard,PresentationControls.Columns(quotaCards,320),1100));
        else panel.Children.Add(totalsCard);
        var activityCard=new StackPanel { Spacing=12 };
        var picker=new ComboBox { MinWidth=160, HorizontalAlignment=HorizontalAlignment.Right };
        Microsoft.UI.Xaml.Automation.AutomationProperties.SetName(picker,L.Text("年度活动客户端","Annual activity client"));
        picker.Items.Add(new ComboBoxItem { Tag="",Content=L.Text("全部客户端","All clients") });
        foreach(var provider in Host.Workspace.EnabledProviders) picker.Items.Add(new ComboBoxItem{Tag=provider,Content=MainWindow.ProviderName(provider)});
        picker.SelectedItem=picker.Items.Cast<ComboBoxItem>().FirstOrDefault(item=>(string)item.Tag==activityProvider)??picker.Items[0];
        picker.SelectionChanged+=(_,_)=>{ if(picker.SelectedItem is ComboBoxItem {Tag:string provider}) {activityProvider=provider;RenderActivity();} };
        activityCard.Children.Add(PresentationControls.SectionHeader(L.Text("年度活动","Annual activity"),picker));
        activityCard.Children.Add(activity);panel.Children.Add(PresentationControls.Card(activityCard));RenderActivity();
        var trend=new StackPanel {Spacing=16};
        var trendMode=new ComboBox {MinWidth=160};
        Microsoft.UI.Xaml.Automation.AutomationProperties.SetName(trendMode,L.Text("趋势视图","Trend view"));
        trendMode.Items.Add(L.Text("总量趋势","Total trend"));trendMode.Items.Add(L.Text("按客户端堆叠","Stacked by client"));trendMode.SelectedIndex=stackedTrend?1:0;
        trendMode.SelectionChanged+=(_,_)=>{stackedTrend=trendMode.SelectedIndex==1;RenderTrend();};
        trend.Children.Add(PresentationControls.SectionHeader(L.Text("Token 趋势","Token trend"),trendMode));
        trend.Children.Add(trendPlot);RenderTrend();
        var metric=new ComboBox { MinWidth=160 };
        Microsoft.UI.Xaml.Automation.AutomationProperties.SetName(metric,L.Text("构成指标","Distribution metric"));
        metric.Items.Add(L.Text("Token","Tokens"));metric.Items.Add(L.Text("API 等价估算成本","API-equivalent estimate"));metric.SelectedIndex=costDistribution?1:0;
        metric.SelectionChanged+=(_,_)=>{costDistribution=metric.SelectedIndex==1;RenderDistribution();};
        var composition=new StackPanel {Spacing=16};composition.Children.Add(PresentationControls.SectionHeader(L.Text("用量构成","Usage composition"),metric));composition.Children.Add(distribution);
        panel.Children.Add(PresentationControls.Card(trend));panel.Children.Add(PresentationControls.Card(composition));RenderDistribution();
        var details=new StackPanel {Spacing=12};
        details.Children.Add(PresentationControls.Heading(L.Text("客户端明细","Client details")));
        details.Children.Add(PresentationControls.Caption(L.Text("API 等价估算与官方金额分别展示","API estimates and reported amounts are shown separately")));
        details.Children.Add(PresentationControls.Table([L.Text("客户端","Client"),"Token",L.Text("API 等价估算","API estimate"),L.Text("官方金额","Reported amount"),L.Text("覆盖","Coverage")],
            response.Providers.Select(provider=>new[]{MainWindow.ProviderName(provider.Provider),L.Tokens(provider.Totals?.TotalTokens),L.Money(provider.Totals?.EstimatedUsdMicros),L.Money(provider.ReportedUsdMicros),Coverage(provider.CoverageState)+" / "+Coverage(provider.CostState)})));
        panel.Children.Add(PresentationControls.Card(details));
        if(response.Providers.Count==0) panel.Children.Add(Text(L.Text("尚未启用客户端，可在设置中启用来源。","No clients are enabled. Enable a source in Settings.")));
    }
    private void RenderActivity()
    {
        activity.Children.Clear();if(last is null)return;
        var complete=DashboardPresentation.Complete(activityProvider==""?last.ActivityCoverageState:last.ActivityProviderCoverage.FirstOrDefault(value=>value.Provider==activityProvider)?.CoverageState);
        var days=DashboardPresentation.Calendar(last.Activity.Select(point=>((NumericValue?)point.StartAtMs,activityProvider==""?point.TotalTokens:point.Shares.FirstOrDefault(share=>share.Provider==activityProvider)?.TotalTokens,(NumericValue?)null)),DateOnly.FromDateTime(DateTime.Today),last.ReportingTimeZone,complete);
        activity.Children.Add(PresentationControls.Caption(L.Text("最近 365 天 · ","Last 365 days · ")+(complete?L.Text("完整覆盖","Complete coverage"):L.Text("部分或未知；缺失日不计零","Partial or unknown; missing days remain unknown"))));
        activity.Children.Add(Charts.Calendar(days,L));
    }
    private void RenderTrend()
    {
        trendPlot.Children.Clear();if(last is null)return;
        trendPlot.Children.Add(Charts.Bars(last.Trend.Select(point=>new ChartDatum(point.Key,DashboardPresentation.Value(point.TotalTokens),
            DashboardPresentation.Reconciled(DashboardPresentation.Value(point.TotalTokens),point.Shares.Select(share=>DashboardPresentation.Value(share.TotalTokens) is {} value?new ChartSegment(MainWindow.ProviderName(share.Provider),value,share.Provider):null)))).ToArray(),L,"Token",true,continuous:!stackedTrend,showLegend:stackedTrend));
    }
    private void RenderDistribution()
    {
        distribution.Children.Clear();if(last is null)return;
        var clients=new StackPanel {Spacing=12};var modelPanel=new StackPanel {Spacing=12};
        distribution.Children.Add(PresentationControls.Columns([clients,modelPanel],420));
        clients.Children.Add(PresentationControls.Heading(L.Text("客户端","Clients")));
        var total=DashboardPresentation.Value(costDistribution?last.Totals?.EstimatedUsdMicros:last.Totals?.TotalTokens);
        var shares=last.Distribution.Select(share=>(Share:share,Value:DashboardPresentation.Value(costDistribution?share.EstimatedUsdMicros:share.TotalTokens))).ToArray();
        if(shares.Any(share=>share.Value is null)) clients.Children.Add(Text(L.Text("有客户端的构成数值未知。","Some client shares are unknown.")));
        else clients.Children.Add(Charts.Distribution(shares.Select(share=>new ChartSegment(MainWindow.ProviderName(share.Share.Provider),share.Value!.Value,share.Share.Provider)).ToArray(),total,L,costDistribution));
        modelPanel.Children.Add(PresentationControls.Heading(L.Text("模型","Models")));
        var models=last.Models.Select(model=>(Model:model,Value:DashboardPresentation.Value(costDistribution?model.Totals?.EstimatedUsdMicros:model.Totals?.TotalTokens))).ToArray();
        if(models.Any(model=>model.Value is null) || total is null) {modelPanel.Children.Add(Text(L.Text("模型构成未知。","Model breakdown is unknown.")));return;}
        var entries=models.Select(model=>new ChartSegment(MainWindow.ProviderName(model.Model.Provider)+" · "+DashboardPresentation.Name(model.Model.Model,L.Text("未归类模型","Unattributed model")),model.Value!.Value,model.Model.Provider+":"+model.Model.DimensionKey)).ToList();
        var known=entries.Aggregate(System.Numerics.BigInteger.Zero,(sum,item)=>sum+item.Value);
        if(known<=total && known>=0 && total-known<=long.MaxValue)entries.Add(new(L.Text("其他已知模型","Other known models"),(long)(total.Value-known),"other"));
        modelPanel.Children.Add(Charts.Distribution(entries,total,L,costDistribution));
    }
    public override void Cancel()=>summary.Slot.Cancel();
    private string Coverage(string? state)=>state switch
    {
        "complete" or "ready"=>L.Text("完整","Complete"),
        "partial"=>L.Text("部分覆盖","Partial"),
        "empty"=>L.Text("暂无数据","No data"),
        _=>L.Text("未知","Unknown")
    };
    public override void Dispose()=>summary.Dispose();
}
