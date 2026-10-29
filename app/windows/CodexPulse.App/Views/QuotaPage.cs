using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using CodexPulse.CoreClient;
using Grpc.Core;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Controls;

namespace CodexPulse.App.Views;

internal sealed class QuotaPage:FeaturePage
{
    private readonly string provider;
    private readonly QuerySlot<QuotaCurrentResponse> quota=new();
    private readonly QuerySlot<QuotaPaceResponse> pace=new();
    private readonly QuerySlot<AccountSnapshotResponse> account=new();
    private readonly DataSection<UsageCostResponse> usage;
    private readonly DataSection<PricingCatalogCurrentResponse> prices;
    private readonly StackPanel current=new(){Spacing=14},paceView=new(){Spacing=14};
    private readonly TextBlock feedback;
    private readonly Button refresh,resetRefresh;
    private bool disposed,refreshing;
    public QuotaPage(MainWindow host):base(host)
    {
        provider=host.Workspace.Provider??"codex";
        feedback=Text("");
        refresh=new(){Content=L.Text("刷新当前额度","Refresh current quota")};refresh.Click+=async(_,_)=>await Refresh("quota");
        resetRefresh=new(){Content=L.Text("刷新重置次数","Refresh reset credits"),Visibility=provider=="codex"?Visibility.Visible:Visibility.Collapsed};resetRefresh.Click+=async(_,_)=>await Refresh("reset_credits");
        if(provider!="dsh"){Body.Children.Add(refresh);Body.Children.Add(resetRefresh);Body.Children.Add(feedback);Body.Children.Add(PresentationControls.Card(current));Body.Children.Add(PresentationControls.Card(paceView));}
        else Body.Children.Add(Text(L.Text("DSH 不提供官方额度，以下为 Home 的用量与 API 等价估算。","DSH does not provide an official quota. Home usage and API-equivalent estimates follow.")));
        usage=new(host,L.Text("Home 用量与成本","Home usage and cost"),RenderUsage,"quota-usage");Body.Children.Add(usage.View);
        prices=new(host,L.Text("模型参考价格","Model reference prices"),RenderPrices,"quota-prices");Body.Children.Add(PresentationControls.Card(prices.View));
    }
    public override bool DependsOn(string domain)=>domain is "account" or "settings" or "index" or "quota" || domain=="quota_"+provider;
    public override async Task LoadAsync(bool force=false)
    {
        State(LoadKind.Loading);
        var context=Host.Workspace.Context;var now=DateTimeOffset.Now.ToUnixTimeMilliseconds();
        var qr=new QuotaCurrentRequest{Provider=new(){Provider=provider},EvaluatedAtMs=now};var pr=new QuotaPaceRequest{Provider=new(){Provider=provider},EvaluatedAtMs=now};
        Task LoadAccount()=>account.LoadAsync((provider,context.Generation),ct=>
        {
            var request=new AccountSnapshotRequest{Provider=new(){Provider=provider},EvaluatedAtMs=DateTimeOffset.Now.ToUnixTimeMilliseconds(),TimeZone=context.TimeZone};
            return Host.Client.QueryAsync("AccountSnapshot",request,context,["account"],(core,deadline,cancel)=>core.AccountSnapshotAsync(request,deadline:deadline,cancellationToken:cancel),ct,true);
        });
        var quotaTask=provider=="dsh"?Task.CompletedTask:quota.LoadAsync((provider,context.Generation),ct=>Host.Client.QueryAsync("QuotaCurrent",qr,context,["account","quota","quota_"+provider],(core,deadline,cancel)=>core.QuotaCurrentAsync(qr,deadline:deadline,cancellationToken:cancel),ct,force),r=>DashboardPresentation.Kind(r.Meta));
        var paceTask=provider=="dsh"?Task.CompletedTask:pace.LoadAsync((provider,context.Generation),ct=>Host.Client.QueryAsync("QuotaPace",pr,context,["account","quota","quota_"+provider],(core,deadline,cancel)=>core.QuotaPaceAsync(pr,deadline:deadline,cancellationToken:cancel),ct,force),r=>DashboardPresentation.Kind(r.Meta));
        var loadAccount=provider!="dsh" && (force || provider!="codex" || account.State.Value is null);
        var accountTask=loadAccount?LoadAccount():Task.CompletedTask;
        var ur=new UsageCostRequest{Provider=new(){Provider=provider},Range=DashboardPresentation.LocalRange(context),Granularity="day"};
        var usageTask=usage.Slot.LoadAsync(context,ct=>Host.Client.QueryAsync("UsageCost",ur,context,["index","account"],(core,deadline,cancel)=>core.UsageCostAsync(ur,deadline:deadline,cancellationToken:cancel),ct,force),r=>DashboardPresentation.Kind(r.Meta));
        var priceRequest=new PricingCatalogCurrentRequest{Provider=new(){Provider=provider}};
        var priceTask=prices.Slot.LoadAsync((provider,context.Generation),ct=>Host.Client.QueryAsync("PricingCatalogCurrent",priceRequest,context,["settings"],(core,deadline,cancel)=>core.PricingCatalogCurrentAsync(priceRequest,deadline:deadline,cancellationToken:cancel),ct,force),r=>DashboardPresentation.Kind(r.Meta));
        await Task.WhenAll(quotaTask,paceTask,accountTask,usageTask,priceTask);
        if(disposed)return;
        if(provider=="codex" && !QuotaPresentation.SameAccount(account.State.Value?.Binding,quota.State.Value?.Current.Binding) && !loadAccount)await LoadAccount();
        if(disposed)return;RenderQuota();State(provider=="dsh"?usage.Slot.State.Kind:quota.State.Kind);
    }
    private async Task Refresh(string source)
    {
        if(refreshing || disposed)return;refreshing=true;refresh.IsEnabled=resetRefresh.IsEnabled=false;feedback.Text=L.Text("正在刷新当前账号…","Refreshing the current account…");
        try
        {
            var request=new QuotaRefreshRequest{Provider=new(){Provider=provider},Source=source};
            var result=await Host.Client.MutateAsync((core,deadline,ct)=>core.RequestQuotaRefreshAsync(request,deadline:deadline,cancellationToken:ct));
            if(disposed)return;
            var receipt=result.Receipt;
            feedback.Text=result.Outcome==MutationOutcome.Unknown?L.Text("刷新结果未知，正在重新读取；不会自动重发。","Refresh result unknown; reading back without replaying."):
                receipt?.ProviderContext?.EffectiveProvider!=provider?L.Text("刷新回执客户端不一致，请重读。","Refresh receipt client differs. Reload."):
                receipt.Fetched?L.Text("已取得本次数据，正在读取最新快照。","New data fetched; reading the latest snapshot."):
                L.Text("本次未取得新数据：","No new data fetched: ")+receipt.Reason+(receipt.HasNextDueAtMs?L.Text(" · 下次可用 "," · Next due ")+L.Timestamp(receipt.NextDueAtMs):"");
            await LoadAsync(true);
            if(!disposed && receipt?.Fetched==true && result.Outcome!=MutationOutcome.Unknown && receipt.ProviderContext?.EffectiveProvider==provider)
                feedback.Text=L.Text("本次刷新已取得数据，并已重新读取最新快照。","New data fetched and the latest snapshot read back.");
        }
        catch(RpcException error){if(!disposed)feedback.Text=L.Text("刷新未完成：","Refresh incomplete: ")+(CoreConnection.ErrorDetail(error)?.Code??error.StatusCode.ToString());}
        catch(IOException){if(!disposed)feedback.Text=L.Text("连接中断，刷新结果待确认。","Connection interrupted; refresh result unconfirmed.");}
        finally{refreshing=false;if(!disposed)refresh.IsEnabled=resetRefresh.IsEnabled=true;}
    }
    private void RenderQuota()
    {
        current.Children.Clear();paceView.Children.Clear();
        current.Children.Add(Text(L.Text("当前额度","Current quota"),"SubtitleTextBlockStyle"));
        current.Children.Add(Text(L.State(quota.State.Kind)));
        if(provider is "cursor" or "grok")current.Children.Add(Text(TrayPresentation.Identity(provider,account.State.Kind==LoadKind.Ready?account.State.Value:null,false,L)));
        if(quota.State.Value is not {} response || response.ProviderContext?.EffectiveProvider!=provider){current.Children.Add(Text(L.Text("当前额度暂不可用。","Current quota is unavailable.")));return;}
        if(provider=="codex")
        {
            if(!QuotaPresentation.SameAccount(account.State.Value?.Binding,response.Current.Binding))
            {current.Children.Add(Text(L.Text("账号身份确认中；暂不展示在线额度和旧账号事实。Home 用量仍独立显示。","Confirming account identity; online quota is temporarily withheld. Home usage remains independent.")));return;}
            var identity=account.State.Value!;
            current.Children.Add(Text(identity.Subscription is {} profile?QuotaPresentation.AccountTitle(profile,L)+" · "+QuotaPresentation.Plan(profile.HasResolvedPlan?profile.ResolvedPlan:null,L):identity.Account?.Email??L.Text("当前已确认账号","Current confirmed account")));
            if(identity.Subscription is {} subscription)current.Children.Add(Text(QuotaPresentation.SubscriptionDate(subscription,L)));
        }
        if(QuotaPresentation.Windows(response.Current).Count==0)current.Children.Add(Text(L.Text("尚未获得可展示的额度窗口。","No quota windows are available yet.")));
        foreach(var window in QuotaPresentation.Windows(response.Current))
        {
            var name=QuotaPresentation.WindowName(window.LimitId,window.HasLimitName?window.LimitName:null,window.HasWindowMinutes?window.WindowMinutes:null,L);
            var expired=window.HasResetsAtMs && window.ResetsAtMs<=response.Current.EvaluatedAtMs;
            var remaining=expired?null:QuotaPresentation.Percent(window.HasRemainingPercent,window.RemainingPercent);
            var row=new StackPanel{Spacing=6};row.Children.Add(Text(name+" · "+L.Text("剩余 ","Remaining ")+QuotaPresentation.PercentText(remaining,L)));
            AddProgress(row,name,remaining);
            row.Children.Add(Text(expired?L.Text("周期已结束，等待新周期确认。","Period ended; awaiting confirmation of a new cycle."):QuotaPresentation.Freshness(window.Freshness,L)));
            row.Children.Add(Text(L.Text("重置 ","Resets ")+L.Timestamp(window.HasResetsAtMs?window.ResetsAtMs:null)+" · "+L.Text("来源 ","Source ")+window.SelectedSource+" · "+L.Text("上次成功 ","Last success ")+L.Timestamp(window.HasLastSuccessAtMs?window.LastSuccessAtMs:null)));
            if(!string.IsNullOrEmpty(window.Conflict) && window.Conflict!="none")row.Children.Add(Text(L.Text("来源存在冲突：","Sources conflict: ")+window.Conflict));
            if(window.HasUnknownReason)row.Children.Add(Text(L.Text("状态原因：","State reason: ")+window.UnknownReason));
            if(window.Explanations.Count>0)row.Children.Add(PresentationControls.Disclosure(L.Text("来源证据","Source evidence"),PresentationControls.Table([L.Text("来源","Source"),L.Text("剩余","Remaining"),L.Text("观察时间","Observed"),L.Text("可信状态","Trust")],window.Explanations.Select(item=>new[]{item.Source,QuotaPresentation.PercentText(QuotaPresentation.Percent(item.HasRemainingPercent,item.RemainingPercent),L),L.Timestamp(item.ObservedAtMs),item.Validity+" · "+item.Disposition}))));
            current.Children.Add(PresentationControls.Card(row));
        }
        if(provider=="codex" && response.Current.ResetCredits is {} credits)
        {
            current.Children.Add(Text(L.Text("可用重置次数","Available reset credits"),"SubtitleTextBlockStyle"));
            current.Children.Add(PresentationControls.Metrics((L.Text("当前库存","Current inventory"),credits.HasAvailableCount?credits.AvailableCount.ToString(L.Culture):"—"),(L.Text("最近到期","Next expiry"),L.Timestamp(credits.HasNextExpiresAtMs?credits.NextExpiresAtMs:null))));
            current.Children.Add(Text(QuotaPresentation.Freshness(credits.Freshness,L)+" · "+L.Text("上次成功 ","Last success ")+L.Timestamp(credits.HasLastSuccessAtMs?credits.LastSuccessAtMs:null)));
            current.Children.Add(Text(L.Text("只展示当前库存；不推断累计领取或已使用次数。","Current inventory only; cumulative allocation and consumption are not inferred.")));
            if(credits.HasCumulativeRemainingMs)current.Children.Add(Text(L.Text("库存累计剩余时长：","Cumulative remaining inventory time: ")+(credits.CumulativeRemainingMs>=0 && credits.CumulativeRemainingMs<=TimeSpan.MaxValue.TotalMilliseconds?TimeSpan.FromMilliseconds(credits.CumulativeRemainingMs).ToString("g",L.Culture):"—")));
            if(credits.Items.Count>0)current.Children.Add(PresentationControls.Disclosure(L.Text("重置库存明细","Reset inventory details"),PresentationControls.Table([L.Text("类型","Type"),L.Text("状态","Status"),L.Text("发放时间","Granted"),L.Text("到期时间","Expires")],credits.Items.Select(item=>new[]{item.Type,item.Status,L.Timestamp(item.GrantedAtMs),L.Timestamp(item.HasExpiresAtMs?item.ExpiresAtMs:null)}))));
        }
        paceView.Children.Add(Text(L.Text("额度节奏与历史","Quota pace and history"),"SubtitleTextBlockStyle"));
        paceView.Children.Add(Text(L.State(pace.State.Kind)));
        if(pace.State.Value is not {} p || p.ProviderContext?.EffectiveProvider!=provider || provider=="codex" && !QuotaPresentation.SameAccount(response.Current.Binding,p.Pace.Binding))
        {paceView.Children.Add(Text(L.Text("同一账号的节奏数据尚未确认。","Pace data for the same account is not confirmed yet.")));return;}
        if(p.Pace.Windows.Count==0)paceView.Children.Add(Text(L.Text("尚无足够的节奏观察。","There are not enough pace observations yet.")));
        foreach(var window in p.Pace.Windows)
        {
            var panel=new StackPanel{Spacing=10};panel.Children.Add(Text(QuotaPresentation.WindowName(window.LimitId,null,window.HasWindowMinutes?window.WindowMinutes:null,L)));
            panel.Children.Add(PresentationControls.Metrics((L.Text("周期已过","Period elapsed"),QuotaPresentation.PercentText(QuotaPresentation.Percent(window.HasElapsedPercent,window.ElapsedPercent),L)),(L.Text("剩余额度","Remaining quota"),QuotaPresentation.PercentText(QuotaPresentation.Percent(window.HasRemainingPercent,window.RemainingPercent),L)),(L.Text("历史周期数","History cycles"),window.HistoryCycleCount.ToString(L.Culture))));
            if(window.HasPaceDeltaPp)panel.Children.Add(Text(L.Text("相对均匀节奏的消耗差：","Consumption difference from even pace: ")+window.PaceDeltaPp.ToString("+0.##;-0.##;0",L.Culture)+" pp"));
            panel.Children.Add(PaceChart.Create(window,L));
            var forecast=window.Forecast;
            panel.Children.Add(Text(forecast is null?L.Text("预测未知","Forecast unknown"):QuotaPresentation.Forecast(forecast.State,L)+(forecast.HasExhaustAtMs?L.Text(" · 预计耗尽 "," · Expected exhaustion ")+L.Timestamp(forecast.ExhaustAtMs):"")+L.Text(" · 证据点 "," · Evidence points ")+forecast.EvidenceCount.ToString(L.Culture)+(forecast.HasUnknownReason?" · "+forecast.UnknownReason:"")));
            panel.Children.Add(PresentationControls.Disclosure(L.Text("节奏数据明细","Pace data details"),PresentationControls.Table([L.Text("时间","Time"),L.Text("周期已过","Elapsed"),L.Text("剩余","Remaining"),L.Text("归属","Attribution")],window.CurrentPoints.Select(point=>new[]{L.Timestamp(point.ObservedAtMs),QuotaPresentation.PercentText(QuotaPresentation.Percent(true,point.ElapsedPercent),L),QuotaPresentation.PercentText(QuotaPresentation.Percent(true,point.RemainingPercent),L),point.LinkedHistory?L.Text("显式关联旧历史","Explicitly linked history"):L.Text("当前账号","Current account")}))));
            paceView.Children.Add(PresentationControls.Card(panel));
        }
    }
    internal static void AddProgress(StackPanel panel,string name,double? remaining)
    {
        if(remaining is not {} value)return;
        var progress=new ProgressBar{Minimum=0,Maximum=100,Value=value,Height=6};AutomationProperties.SetName(progress,name+" · "+value.ToString("G6",System.Globalization.CultureInfo.CurrentUICulture)+"%");panel.Children.Add(progress);
    }
    private void RenderUsage(StackPanel panel,UsageCostResponse response)
    {
        panel.Children.Add(PresentationControls.Metrics(("Token",L.Number(response.Totals?.TotalTokens)),(L.Text("API 等价估算","API-equivalent estimate"),L.Money(response.Totals?.EstimatedUsdMicros)),(L.Text("未定价轮数","Unpriced turns"),L.Number(response.Totals?.UnpricedTurnCount))));
        panel.Children.Add(Text(L.Text("此处为所选范围的 Home 用量，与账号订阅资料和在线额度分别统计。估算不等于订阅账单。","Home usage for the selected range is independent of subscription profiles and online quota. Estimates are not subscription bills.")));
        if(response.ReportedUsdMicros?.HasValue==true)panel.Children.Add(Text(L.Text("官方报告金额：","Reported amount: ")+L.Money(response.ReportedUsdMicros)+" · "+response.ReportedCostSource));
        panel.Children.Add(Charts.Bars(DashboardPresentation.UsageTrend(response),L,"Token",true));
        panel.Children.Add(PresentationControls.Table([L.Text("模型","Model"),"Token",L.Text("API 等价估算","API estimate")],response.Models.Select(item=>new[]{DashboardPresentation.Name(item.Model,L.Text("未归类","Unattributed")),L.Number(item.Totals?.TotalTokens),L.Money(item.Totals?.EstimatedUsdMicros)})));
    }
    private void RenderPrices(StackPanel panel,PricingCatalogCurrentResponse response)
    {
        if(response.ProviderContext?.EffectiveProvider!=provider){panel.Children.Add(Text(L.Text("参考价客户端不一致，暂不展示。","Reference-price client differs; prices withheld.")));return;}
        panel.Children.Add(Text((provider=="codex"?"OpenAI API Standard":MainWindow.ProviderName(provider))+L.Text(" · 基础文本参考价 · "," · Basic text reference prices · ")+response.Currency+" / "+L.Number(response.UnitTokens)+" Token"));
        panel.Children.Add(Text(L.Text("仅用于 API 等价估算；不代表订阅账单。缓存写入、长上下文、Batch、Flex、Fast mode 和区域处理可能适用不同费率。","For API-equivalent estimates; not subscription bills. Cache-write, long-context, Batch, Flex, Fast mode and regional processing may use different rates.")));
        panel.Children.Add(PresentationControls.Table(provider=="cursor"?[L.Text("模型","Model"),L.Text("输入","Input"),L.Text("缓存读取","Cache read"),L.Text("缓存写入","Cache write"),L.Text("输出","Output")]:[L.Text("模型","Model"),L.Text("输入","Input"),L.Text("缓存输入","Cached input"),L.Text("输出","Output")],response.Items.Where(item=>provider!="codex" || QuotaPresentation.ReferenceModelVisible(item.ModelId)).OrderBy(item=>item.ModelId).Select(item=>provider=="cursor"?new[]{item.ModelId,L.Money(item.InputMicros),L.Money(item.CachedInputMicros),L.Money(item.CacheWriteMicros),L.Money(item.OutputMicros)}:new[]{item.ModelId,L.Money(item.InputMicros),L.Money(item.CachedInputMicros),L.Money(item.OutputMicros)})));
        panel.Children.Add(Text(L.Text("来源：","Source: ")+response.Source+" · "+response.PricingVersion+L.Text(" · 核验时间 "," · Verified ")+L.Timestamp(response.VerifiedAtMs?.HasValue==true?response.VerifiedAtMs.Value:null)));
        if(response.HasSourceUrl && Uri.TryCreate(response.SourceUrl,UriKind.Absolute,out var uri) && uri.Scheme=="https")panel.Children.Add(new HyperlinkButton{Content=L.Text("查看定价来源","View pricing source"),NavigateUri=uri});
    }
    public override void Cancel(){quota.Cancel();pace.Cancel();account.Cancel();usage.Slot.Cancel();prices.Slot.Cancel();}
    public override void Dispose(){disposed=true;quota.Dispose();pace.Dispose();account.Dispose();usage.Dispose();prices.Dispose();}
}
