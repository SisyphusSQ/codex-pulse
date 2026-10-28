using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace CodexPulse.App.Views;

internal sealed class OverviewPage : FeaturePage
{
    private readonly DataSection<QuotaCurrentResponse> quota;
    private readonly DataSection<UsageCostResponse> annual, usage;
    private readonly DataSection<SessionListResponse> sessions;
    private readonly DataSection<ProjectListResponse> projects;
    private readonly DataSection<InvocationUsageResponse>? invocations;
    private CancellationTokenSource? loading;
    private readonly string provider;
    private readonly TextBlock rangeLabel;
    private readonly StackPanel activity=new(){Spacing=12};
    private UsageCostResponse? currentUsage;
    private bool activitySessions;
    private UTCTimeRange? range;
    private bool disposed;
    private readonly HashSet<string> pendingInvalidations=[];

    public OverviewPage(MainWindow host):base(host)
    {
        provider=Host.Workspace.Provider??"codex";
        rangeLabel=Text("");Body.Children.Add(rangeLabel);
        quota=new(host,L.Text("当前额度","Current quota"),(panel,response)=>panel.Children.Add(QuotaSummary.Create(response.Current,provider,L)),"overview-quota");
        if(provider!="dsh") Body.Children.Add(PresentationControls.Card(quota.View));
        annual=new(host,L.Text("年度 Token 活动 · 独立的最近 365 天","Annual token activity · Independent last 365 days"),RenderAnnual,"overview-annual");
        Body.Children.Add(PresentationControls.Card(annual.View));
        usage=new(host,L.Text("当前范围用量与 API 等价成本","Current range usage and API-equivalent cost"),RenderUsage,"overview-usage");
        Body.Children.Add(usage.View);
        var activityCard=new StackPanel {Spacing=12};
        activityCard.Children.Add(Text(L.Text("当前范围活动分布","Current range activity"),"SubtitleTextBlockStyle"));
        var metric=new ComboBox {Header=L.Text("活动指标","Activity metric"),MinWidth=180};
        metric.Items.Add(L.Text("Token 消耗","Token usage"));metric.Items.Add(L.Text("会话数量","Session count"));metric.SelectedIndex=0;
        metric.SelectionChanged+=(_,_)=>{activitySessions=metric.SelectedIndex==1;RenderActivity();};
        activityCard.Children.Add(metric);activityCard.Children.Add(activity);Body.Children.Add(PresentationControls.Card(activityCard));
        sessions=new(host,L.Text("高消耗会话 · 前 10","High-usage sessions · Top 10"),RenderSessions,"overview-sessions");
        projects=new(host,L.Text("项目摘要 · 前 5","Project summary · Top 5"),RenderProjects,"overview-projects");
        var rankings=new Grid {ColumnSpacing=16,RowSpacing=20};rankings.Children.Add(sessions.View);rankings.Children.Add(projects.View);
        rankings.SizeChanged+=(_,args)=>PresentationControls.Layout(rankings,2,args.NewSize.Width<900?220:440);
        PresentationControls.Layout(rankings,2,600);Body.Children.Add(rankings);
        if(provider is "codex" or "dsh")
        {
            invocations=new(host,L.Text("调用画像","Invocation profile"),RenderInvocations,"overview-invocations");Body.Children.Add(PresentationControls.Card(invocations.View));
        }
    }
    public override bool DependsOn(string domain)=>domain is "index" or "settings" or "account" or "quota" || domain=="quota_"+provider;
    public override void Invalidate(IReadOnlyCollection<string> domains)=>pendingInvalidations.UnionWith(domains);
    public override async Task LoadAsync(bool force=false)
    {
        State(LoadKind.Loading);
        loading?.Cancel();var source=new CancellationTokenSource();loading=source;var token=source.Token;
        var context=Host.Workspace.Context;
        var quotaOnly=pendingInvalidations.Count>0 && pendingInvalidations.All(domain=>domain=="quota" || domain=="quota_"+provider);
        pendingInvalidations.Clear();
        var now=DateTimeOffset.Now;
        var annualContext=context with {Start=DateOnly.FromDateTime(now.LocalDateTime).AddDays(-364),EndExclusive=DateOnly.FromDateTime(now.LocalDateTime).AddDays(1),Preset="annual"};
        var annualRequest=new UsageCostRequest {Provider=new(){Provider=provider},Range=DashboardPresentation.AnnualRange(context.TimeZone,DateOnly.FromDateTime(now.LocalDateTime)),Granularity="day",TokenTotalsOnly=true};
        var annualTask=quotaOnly && annual.Slot.State.Value is not null?Task.CompletedTask:annual.Slot.LoadAsync(annualContext,cancel=>Host.Client.QueryAsync("UsageCost",annualRequest,annualContext,["index","account"],
            (client,deadline,ct)=>client.UsageCostAsync(annualRequest,deadline:deadline,cancellationToken:ct),cancel,force),response=>DashboardPresentation.Kind(response.Meta),token);
        var quotaRequest=new QuotaCurrentRequest {Provider=new(){Provider=provider},EvaluatedAtMs=now.ToUnixTimeMilliseconds()};
        var quotaTask=provider=="dsh"?Task.CompletedTask:quota.Slot.LoadAsync((provider,context.Generation),cancel=>Host.Client.QueryAsync("QuotaCurrent",quotaRequest,context,["quota","quota_"+provider,"account"],
            (client,deadline,ct)=>client.QuotaCurrentAsync(quotaRequest,deadline:deadline,cancellationToken:ct),cancel,force),response=>DashboardPresentation.Kind(response.Meta),token);
        try
        {
            if(quotaOnly && context.Preset is not ("quota_week" or "quota_month")) {await quotaTask;State(usage.Slot.State.Kind);return;}
            if(context.Preset is "quota_week" or "quota_month") await quotaTask;
            token.ThrowIfCancellationRequested();
            var resolution=DashboardPresentation.ResolveRange(context,quota.Slot.State.Value?.Current,now);
            range=resolution.Exact;
            var capturedRange=range.Clone();
            if(!disposed) rangeLabel.Text=(resolution.Fallback?L.Text("额度周期不可用；当前明确回退为最近 ","Quota period unavailable; explicitly using the last ")+(context.Preset=="quota_month"?"30":"7")+L.Text(" 天。 "," days. "):"")+L.Timestamp(range.StartAtMs)+" — "+L.Timestamp(range.EndAtMs)+" · "+range.TimeZone;
            var request=new UsageCostRequest {Provider=new(){Provider=provider},ExactRange=capturedRange,Granularity=resolution.Granularity,IncludeActivityDistribution=true};
            var usageTask=usage.Slot.LoadAsync(context,cancel=>Host.Client.QueryAsync("UsageCost",request,context,["index","account"],
                (client,deadline,ct)=>client.UsageCostAsync(request,deadline:deadline,cancellationToken:ct),cancel,force),response=>DashboardPresentation.Kind(response.Meta,DashboardPresentation.Value(response.Totals?.TotalTokens)==0),token);
            var sessionRequest=new ListSessionsRequest {Query=new(){Provider=new(){Provider=provider},ExactTimeRange=capturedRange.Clone(),Page=new(){Limit=10}}};
            sessionRequest.Query.Sort.Add(new SortTerm{Field=provider=="cursor"?"lastActivityAt":"totalTokens",Direction="desc"});
            var sessionTask=sessions.Slot.LoadAsync(context,cancel=>Host.Client.QueryAsync("ListSessions",sessionRequest,context,["index","account"],
                (client,deadline,ct)=>client.ListSessionsAsync(sessionRequest,deadline:deadline,cancellationToken:ct),cancel,force),response=>DashboardPresentation.Kind(response.Meta,response.Items.Count==0),token);
            var projectRequest=new ListProjectsRequest {Query=new(){Provider=new(){Provider=provider},ExactTimeRange=capturedRange.Clone(),Page=new(){Limit=5}}};
            projectRequest.Query.Sort.Add(new SortTerm{Field=provider=="cursor"?"lastActivityAt":"totalTokens",Direction="desc"});
            var projectTask=projects.Slot.LoadAsync(context,cancel=>Host.Client.QueryAsync("ListProjects",projectRequest,context,["index","account"],
                (client,deadline,ct)=>client.ListProjectsAsync(projectRequest,deadline:deadline,cancellationToken:ct),cancel,force),response=>DashboardPresentation.Kind(response.Meta,response.Items.Count==0),token);
            var invocationTask=Task.CompletedTask;
            if(invocations is not null)
            {
                var invocationRequest=new InvocationUsageRequest {Provider=new(){Provider=provider},Range=capturedRange.Clone(),Granularity=resolution.Granularity,SourceClass="all",TopLimit=10};
                invocationTask=invocations.Slot.LoadAsync(context,cancel=>Host.Client.QueryAsync("InvocationUsage",invocationRequest,context,["index","account"],
                    (client,deadline,ct)=>client.InvocationUsageAsync(invocationRequest,deadline:deadline,cancellationToken:ct),cancel,force),response=>DashboardPresentation.Kind(response.Meta),token);
            }
            await Task.WhenAll(annualTask,quotaTask,usageTask,sessionTask,projectTask,invocationTask);
            if(!disposed) State(usage.Slot.State.Kind);
        }
        finally { if(ReferenceEquals(loading,source))loading=null;source.Dispose(); }
    }
    private void RenderAnnual(StackPanel panel,UsageCostResponse response)
    {
        var complete=DashboardPresentation.Complete(response.Meta?.Status);
        var days=DashboardPresentation.Calendar(response.Trend.Select(point=>((NumericValue?)point.StartAtMs,point.Totals?.TotalTokens,point.Totals?.TurnCount)),DateOnly.FromDateTime(DateTime.Today),response.ReportingTimeZone,complete);
        var statistics=DashboardPresentation.Statistics(days,DashboardPresentation.Value(response.Totals?.TotalTokens));
        string Number(long? value)=>value?.ToString("N0",L.Culture)??"—";
        var valid=statistics.Total is not null;
        panel.Children.Add(PresentationControls.Metrics((L.Text("365 天 Token","365-day tokens"),Number(statistics.Total)),(L.Text("峰值日 Token","Peak-day tokens"),Number(statistics.Peak)),
            (L.Text("活跃天数","Active days"),valid?statistics.ActiveDays.ToString(L.Culture):"—"),(L.Text("当前 / 最长连续天数","Current / longest streak"),valid?$"{statistics.CurrentStreak} / {statistics.LongestStreak}":"—")));
        panel.Children.Add(Text(statistics.Complete?L.Text("年度日桶完整。","Annual daily buckets are complete."):L.Text("按本机已返回的日桶统计；缺失日期仍为未知。","Statistics use returned local daily buckets; missing dates remain unknown.")));
        panel.Children.Add(Charts.Calendar(days,L));
    }
    private void RenderUsage(StackPanel panel,UsageCostResponse response)
    {
        currentUsage=response;
        panel.Children.Add(PresentationControls.Metrics(("Token",L.Number(response.Totals?.TotalTokens)),(L.Text("API 等价估算成本","API-equivalent estimate"),L.Money(response.Totals?.EstimatedUsdMicros)),
            (L.Text("未定价轮数","Unpriced turns"),L.Number(response.Totals?.UnpricedTurnCount))));
        panel.Children.Add(PresentationControls.Table([L.Text("输入","Input"),L.Text("缓存输入","Cached input"),L.Text("输出","Output"),L.Text("推理","Reasoning")],
            [new[]{L.Number(response.Totals?.InputTokens),L.Number(response.Totals?.CachedInputTokens),L.Number(response.Totals?.OutputTokens),L.Number(response.Totals?.ReasoningTokens)}]));
        panel.Children.Add(Text(L.Text("估算不是订阅账单；缓存写入及长上下文等差异不在当前折算合同内。","Estimates are not subscription bills; cache-write, long-context, and other differences are outside this estimate.")));
        panel.Children.Add(Text(L.Text("计价来源：","Pricing source: ")+(response.HasPricingSource?response.PricingSource:"—")+" · "+string.Join(", ",response.PricingVersions)));
        if(response.UnpricedReasons.Count>0) panel.Children.Add(Text(L.Text("未定价原因：","Unpriced reasons: ")+string.Join(" · ",response.UnpricedReasons.Select(reason=>reason.Reason+" "+L.Number(reason.Count)))));
        if(response.HasDegradedReason) panel.Children.Add(Text(L.Text("部分覆盖：","Partial coverage: ")+response.DegradedReason));
        if(response.ReportedUsdMicros?.HasValue==true)
            panel.Children.Add(Text(L.Text("官方报告金额（独立）：","Reported amount (separate): ")+L.Money(response.ReportedUsdMicros)+" · "+response.ReportedCostSource+" · "+L.Timestamp(response.DataAsOfMs?.HasValue==true?response.DataAsOfMs.Value:null)));
        if(provider=="cursor")
        {
            panel.Children.Add(Text(L.Text("Cursor 官方用量池 · 与 API 等价估算分开","Cursor reported usage pools · Separate from API estimates"),"SubtitleTextBlockStyle"));
            string PoolName(string id)=>id switch{"cursor.models"=>"Cursor Models","cursor.other_models"=>"Other Models",_=>L.Text("未归类用量池","Unclassified usage pool")};
            panel.Children.Add(PresentationControls.Table([L.Text("用量池","Usage pool"),"Token",L.Text("官方金额","Reported charge"),"Cursor Token fee"],
                response.CursorUsagePools.Select(pool=>new[]{PoolName(pool.PoolId),L.Number(pool.Totals?.TotalTokens),L.Money(pool.ReportedUsdMicros),L.Money(pool.CursorTokenFeeUsdMicros)})));
            if(response.CursorBilling is {} billing)
            {
                panel.Children.Add(Text(L.Text("官方账期：","Official billing period: ")+L.Timestamp(billing.BillingCycleStartAtMs?.HasValue==true?billing.BillingCycleStartAtMs.Value:null)+" — "+L.Timestamp(billing.BillingCycleEndAtMs?.HasValue==true?billing.BillingCycleEndAtMs.Value:null)));
                panel.Children.Add(PresentationControls.Metrics((L.Text("官方账期消费","Reported cycle spend"),L.Money(billing.TotalSpendUsdMicros)),(L.Text("官方余额","Reported remaining"),L.Money(billing.RemainingUsdMicros)),(L.Text("官方账期上限","Reported cycle limit"),L.Money(billing.LimitUsdMicros))));
            }
        }
        panel.Children.Add(Text(L.Text("每日 Token 趋势 · 按模型","Daily token trend · By model"),"SubtitleTextBlockStyle"));
        panel.Children.Add(Charts.Bars(DashboardPresentation.UsageTrend(response),L,"Token",true));
        panel.Children.Add(Text(L.Text("模型构成","Model composition"),"SubtitleTextBlockStyle"));
        var models=response.Models.Select(model=>(Model:model,Value:DashboardPresentation.Value(model.Totals?.TotalTokens))).ToArray();
        if(models.Any(model=>model.Value is null))panel.Children.Add(Text(L.Text("有模型的 Token 未知。","Some model token values are unknown.")));
        else panel.Children.Add(Charts.Distribution(models.Select(model=>new ChartSegment(DashboardPresentation.Name(model.Model.Model,L.Text("未归类模型","Unattributed model")),model.Value!.Value,model.Model.DimensionKey)).ToArray(),DashboardPresentation.Value(response.Totals?.TotalTokens),L,false));
        panel.Children.Add(PresentationControls.Disclosure(L.Text("模型用量与成本明细","Model usage and cost details"),PresentationControls.Table([L.Text("模型","Model"),"Token",L.Text("API 等价估算","API estimate"),L.Text("未定价轮数","Unpriced turns")],
            response.Models.Select(model=>new[]{DashboardPresentation.Name(model.Model,L.Text("未归类模型","Unattributed model")),L.Number(model.Totals?.TotalTokens),L.Money(model.Totals?.EstimatedUsdMicros),L.Number(model.Totals?.UnpricedTurnCount)}))));
        panel.Children.Add(PresentationControls.Disclosure(L.Text("每日明细","Daily details"),PresentationControls.Table([L.Text("日期","Date"),"Token",L.Text("缓存","Cached"),L.Text("输出","Output"),L.Text("API 等价估算","API estimate"),L.Text("未定价轮数","Unpriced turns")],
            response.Trend.Select(point=>new[]{point.Key,L.Number(point.Totals?.TotalTokens),L.Number(point.Totals?.CachedInputTokens),L.Number(point.Totals?.OutputTokens),L.Money(point.Totals?.EstimatedUsdMicros),L.Number(point.Totals?.UnpricedTurnCount)}))));
        RenderActivity();
    }
    private void RenderActivity()
    {
        activity.Children.Clear();if(currentUsage is not {} response || response.ActivityDistribution is not {} distribution) {activity.Children.Add(Text(L.Text("活动分布尚未取得。","Activity data is not available yet.")));return;}
        var unit=activitySessions?L.Text("会话数","Sessions"):"Token";
        activity.Children.Add(Charts.Bars(distribution.Timeline.Select(point=>
        {
            var instant=DashboardPresentation.Value(point.StartAtMs) is {} ms && ms<=253402300799999?DateTimeOffset.FromUnixTimeMilliseconds(ms).ToLocalTime():(DateTimeOffset?)null;
            return new ChartDatum(L.Timestamp(point.StartAtMs?.HasValue==true?point.StartAtMs.Value:null)+(instant is {} time?" "+time.ToString("zzz"):""),DashboardPresentation.Value(activitySessions?point.Metrics?.SessionCount:point.Metrics?.TotalTokens),[],L.Text("同一会话可能出现在多个时间桶。","A session can appear in multiple time buckets."),instant?.ToString(distribution.TimelineGranularity=="day"?"MM-dd":"HH:mm",L.Culture)??"—");
        }).ToArray(),L,unit));
        var reconciles=DashboardPresentation.ActivityReconciles(response);
        activity.Children.Add(Text(L.Text("星期 × 小时 · ","Weekday × hour · ")+(reconciles?L.Text("缺失格为已知零","Missing cells are known zero"):L.Text("缺失格保持未知","Missing cells remain unknown"))));
        var grid=new Grid {ColumnSpacing=3,RowSpacing=3,HorizontalAlignment=HorizontalAlignment.Left};
        grid.ColumnDefinitions.Add(new(){Width=new GridLength(48)});for(var hour=0;hour<24;hour++)grid.ColumnDefinitions.Add(new(){Width=new GridLength(17)});
        grid.RowDefinitions.Add(new(){Height=new GridLength(20)});for(var day=0;day<7;day++)grid.RowDefinitions.Add(new(){Height=new GridLength(17)});
        for(var hour=0;hour<24;hour+=3){var label=Text(hour.ToString(L.Culture));label.FontSize=10;Grid.SetColumn(label,hour+1);grid.Children.Add(label);}
        var values=distribution.WeekdayHours.Select(point=>DashboardPresentation.Value(activitySessions?point.Metrics?.SessionCount:point.Metrics?.TotalTokens)??0).ToArray();var peak=values.DefaultIfEmpty().Max();
        for(var day=1;day<=7;day++)
        {
            var label=Text(L.Culture.DateTimeFormat.GetAbbreviatedDayName((DayOfWeek)(day%7)));label.FontSize=11;Grid.SetRow(label,day);grid.Children.Add(label);
            for(var hour=0;hour<24;hour++)
            {
                var matches=distribution.WeekdayHours.Where(point=>point.Weekday==day && point.Hour==hour).ToArray();
                var value=matches.Length==1?DashboardPresentation.Value(activitySessions?matches[0].Metrics?.SessionCount:matches[0].Metrics?.TotalTokens):matches.Length==0 && reconciles?0:(long?)null;
                var cell=new Border {Width=17,Height=17,CornerRadius=new CornerRadius(2),BorderBrush=Charts.NeutralBrush,BorderThickness=value is null?new Thickness(1):new Thickness(0),Background=value>0?new Microsoft.UI.Xaml.Media.SolidColorBrush(Microsoft.UI.ColorHelper.FromArgb(255,42,119,213)):value==0?Charts.NeutralBrush:null,Opacity=value>0 && peak>0?.3+.7*Math.Pow((double)((decimal)value.Value/peak),.35):1};
                ToolTipService.SetToolTip(cell,label.Text+" "+hour.ToString("00")+":00 · "+(value?.ToString("N0",L.Culture)??"—")+" "+unit);Grid.SetRow(cell,day);Grid.SetColumn(cell,hour+1);grid.Children.Add(cell);
            }
        }
        activity.Children.Add(new ScrollViewer {Content=grid,HorizontalScrollBarVisibility=ScrollBarVisibility.Auto,VerticalScrollBarVisibility=ScrollBarVisibility.Disabled});
        activity.Children.Add(PresentationControls.Disclosure(L.Text("星期小时明细","Weekday-hour data"),PresentationControls.Table([L.Text("星期","Weekday"),L.Text("小时","Hour"),"Token",L.Text("会话数","Sessions")],distribution.WeekdayHours.Select(point=>new[]{point.Weekday.ToString(),point.Hour.ToString(),L.Number(point.Metrics?.TotalTokens),L.Number(point.Metrics?.SessionCount)}))));
    }
    private void RenderSessions(StackPanel panel,SessionListResponse response)
    {
        panel.Children.Add(Text(L.Text("按最近活动筛选；数值为会话完整生命周期，可能跨出当前范围。","Filtered by last activity; values cover each session lifetime and may extend beyond this range.")));
        foreach(var item in response.Items)
        {
            var block=new StackPanel {Spacing=4};var link=new Button {Content=new TextBlock {Text=item.DisplayTitle,TextWrapping=TextWrapping.Wrap,MaxLines=2},HorizontalAlignment=HorizontalAlignment.Stretch,HorizontalContentAlignment=HorizontalAlignment.Left};
            var captured=range?.Clone();link.Click+=(_,_)=>{if(captured is not null)Host.OpenSession(item.SessionId,captured);};block.Children.Add(link);
            block.Children.Add(Text(DashboardPresentation.Name(item.Project,L.Text("未归类项目","Unclassified project"))+" · "+L.Number(item.Totals?.TotalTokens)+" Token · "+L.Money(item.Totals?.EstimatedUsdMicros)));
            panel.Children.Add(block);
        }
        var all=new Button {Content=L.Text("查看会话","View sessions")};all.Click+=(_,_)=>Host.Navigate(Feature.Sessions);panel.Children.Add(all);
    }
    private void RenderProjects(StackPanel panel,ProjectListResponse response)
    {
        panel.Children.Add(Text(L.Text("当前精确范围内的项目贡献。","Project contributions within this exact range.")));
        foreach(var item in response.Items)
        {
            var block=new StackPanel {Spacing=4};var link=new Button {Content=new TextBlock {Text=DashboardPresentation.Name(item.Project,L.Text("其他","Other")),TextWrapping=TextWrapping.Wrap,MaxLines=2},HorizontalAlignment=HorizontalAlignment.Stretch,HorizontalContentAlignment=HorizontalAlignment.Left};
            var captured=range?.Clone();link.Click+=(_,_)=>{if(captured is not null)Host.OpenProject(item.DimensionKey,captured);};block.Children.Add(link);
            block.Children.Add(Text(L.Number(item.Totals?.TotalTokens)+" Token · "+L.Money(item.Totals?.EstimatedUsdMicros)+" · "+L.Number(item.SessionCount)+L.Text(" 个会话"," sessions")));panel.Children.Add(block);
        }
        var all=new Button {Content=L.Text("查看项目","View projects")};all.Click+=(_,_)=>Host.Navigate(Feature.Projects);panel.Children.Add(all);
    }
    private void RenderInvocations(StackPanel panel,InvocationUsageResponse response)
    {
        panel.Children.Add(PresentationControls.Metrics((L.Text("工具调用","Tool calls"),L.Number(response.Totals?.ToolCallCount)),(L.Text("技能活动","Skill activity"),L.Number(response.Totals?.SkillActivityCount)),(L.Text("调用失败","Failed calls"),L.Number(response.Totals?.ToolFailureCount))));
        panel.Children.Add(PresentationControls.Table([L.Text("工具","Tool"),L.Text("调用次数","Calls"),L.Text("失败","Failures"),L.Text("会话数","Sessions")],response.Tools.Select(tool=>new[]{tool.Name,L.Number(tool.CallCount),L.Number(tool.FailedCount),L.Number(tool.SessionCount)})));
        panel.Children.Add(PresentationControls.Disclosure(L.Text("技能明细","Skill details"),PresentationControls.Table([L.Text("技能","Skill"),L.Text("活动次数","Activity count"),L.Text("会话数","Sessions")],response.Skills.Select(skill=>new[]{skill.Name,L.Number(skill.ActivityCount),L.Number(skill.SessionCount)}))));
    }
    public override void Cancel() {loading?.Cancel();quota.Slot.Cancel();annual.Slot.Cancel();usage.Slot.Cancel();sessions.Slot.Cancel();projects.Slot.Cancel();invocations?.Slot.Cancel();}
    public override void Dispose(){disposed=true;Cancel();quota.Dispose();annual.Dispose();usage.Dispose();sessions.Dispose();projects.Dispose();invocations?.Dispose();}
}
