using System.Globalization;
using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using CodexPulse.CoreClient;
using Grpc.Core;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Controls;

namespace CodexPulse.App.Views;

internal static class ProductDialog
{
    public static async Task<bool> Confirm(MainWindow host,string title,string message,string action)
    {
        var dialog=new ContentDialog{XamlRoot=host.DialogRoot,Title=title,Content=PresentationControls.Text(message),PrimaryButtonText=action,
            CloseButtonText=host.Localization.Text("取消","Cancel"),DefaultButton=ContentDialogButton.Close,RequestedTheme=((FrameworkElement)host.Content).ActualTheme};
        return await dialog.ShowAsync()==ContentDialogResult.Primary;
    }
}

internal sealed class AccountSettingsPage:FeaturePage
{
    private readonly CodexAccountsSection accounts;
    public AccountSettingsPage(MainWindow host):base(host){accounts=new(host);Body.Children.Add(accounts.View);}
    public override async Task LoadAsync(bool force=false){await accounts.LoadAsync();State(accounts.Kind);}
    public override void Dispose()=>accounts.Dispose();
}

// This section is also embedded in the complete Settings surface. Its Home-scoped
// coordinator keeps profile drafts independent of the global settings snapshot.
internal sealed class CodexAccountsSection:IDisposable
{
    public StackPanel View{get;}=new(){Spacing=16};
    public LoadKind Kind=>controller.List.State.Kind;
    private readonly MainWindow host;
    private Localization L=>host.Localization;
    private readonly SubscriptionController controller;
    private readonly StackPanel rows=new(){Spacing=12},editor=new(){Spacing=12};
    private readonly TextBlock status=PresentationControls.Text("");
    private SubscriptionDraft? renderedDraft;
    private bool disposed,actionRunning;
    public CodexAccountsSection(MainWindow host)
    {
        this.host=host;controller=host.Subscriptions;
        View.Children.Add(PresentationControls.Text(L.Text("Codex 账号与订阅资料","Codex accounts and subscription details"),22));
        View.Children.Add(PresentationControls.Text(L.Text("自动资料只读；手工字段可补充或覆盖。Home 的会话、Token 和项目统计独立于这些资料。此处不切换 Agent 登录。","Detected details are read-only; manual fields supplement or override them. Home usage, sessions and projects remain independent. This does not switch the Agent login.")));
        var add=Button(L.Text("新增手工账号","Add manual account"),()=>{controller.BeginNew();return Task.CompletedTask;});View.Children.Add(add);
        View.Children.Add(status);View.Children.Add(editor);View.Children.Add(rows);
        var suppressRetention=false;
        var retention=new ToggleSwitch{Header=L.Text("切换后保留旧账号额度快照","Retain previous-account quota snapshots after switching"),IsOn=host.Settings?.Snapshot.CodexAccounts.RetainQuotaHistory==true};
        retention.Toggled+=async(_,_)=>
        {
            if(disposed || suppressRetention || host.Settings is null)return;
            var desired=retention.IsOn;
            if(!await ProductDialog.Confirm(host,L.Text("更改保留策略","Change retention policy"),L.Text("此设置仅影响之后确认的账号切换；已有历史不会自动清理。","This affects future confirmed account switches. Existing history is not cleared automatically."),L.Text("保存策略","Save policy")))
            {suppressRetention=true;retention.IsOn=!desired;suppressRetention=false;return;}
            if(disposed)return;
            retention.IsEnabled=false;
            try
            {
                var request=SettingsDraft.From(host.Settings.Snapshot);request.CodexAccounts.RetainQuotaHistory=desired;
                var result=await host.Client.MutateAsync((core,deadline,ct)=>core.UpdateSettingsAsync(request,deadline:deadline,cancellationToken:ct));
                await host.ReloadSettingsAsync();
                host.ShowNotice(host.Settings?.Snapshot.CodexAccounts.RetainQuotaHistory==desired?"已读回保留策略。":"策略未确认保存，请重新核对。",
                    host.Settings?.Snapshot.CodexAccounts.RetainQuotaHistory==desired?"Retention policy read back.":"The policy change is not confirmed. Check it again.",InfoBarSeverity.Informational);
            }
            catch(RpcException){host.ShowNotice("策略保存未完成，请重新读取设置。","The policy could not be saved. Reload Settings.");}
            finally{if(!disposed)retention.IsEnabled=true;}
        };
        View.Children.Add(PresentationControls.Card(retention));
        View.Children.Add(Button(L.Text("清理非当前账号额度历史…","Clear previous-account quota history…"),ClearHistory));
        controller.Changed+=Changed;controller.List.Changed+=Changed;Publish();
    }
    private Button Button(string label,Func<Task> action)
    {
        var button=new Button{Content=label};button.Click+=async(_,_)=>
        {
            if(disposed || controller.Busy || actionRunning)return;
            actionRunning=true;button.IsEnabled=false;
            try{await action();}
            catch(ArgumentException error){if(!disposed)status.Text=L.Text("请检查填写内容：","Check the entered values: ")+error.Message;}
            catch(RpcException error){if(!disposed)status.Text=L.Text("操作未完成：","Action incomplete: ")+(CoreConnection.ErrorDetail(error)?.Code??error.StatusCode.ToString());}
            catch(IOException){if(!disposed)status.Text=L.Text("连接中断，请重新读取并核对结果。","Connection interrupted. Reload and check the result.");}
            finally{actionRunning=false;if(!disposed)button.IsEnabled=true;}
        };return button;
    }
    public Task LoadAsync()=>controller.ReloadAsync();
    private void Changed()=>host.Dispatch(Publish);
    private void Publish()
    {
        if(disposed)return;
        status.Text=L.State(controller.List.State.Kind)+" · "+(controller.Action switch
        {
            SubscriptionAction.Running=>L.Text("正在保存并读回…","Saving and reading back…"),
            SubscriptionAction.Applied=>L.Text("已保存并核对列表","Saved and checked against the list"),
            SubscriptionAction.Noop=>L.Text("无需变更，已核对列表","No change needed; list checked"),
            SubscriptionAction.Conflict=>L.Text("资料已变化；草稿已保留，核对后载入新版本再保存","Details changed; draft retained. Review and rebase before saving"),
            SubscriptionAction.ReadbackConfirmed=>L.Text("命令回执未知；最新列表已确认目标状态","Receipt unknown; latest list confirms the requested state"),
            SubscriptionAction.ReadbackChanged=>L.Text("回执与最新列表不一致，请核对；草稿已保留","Receipt and latest list differ; check them. Draft retained"),
            SubscriptionAction.Unknown=>L.Text("结果尚未确认；请重新读取，勿重复新增","Result uncertain. Reload before adding again"),
            SubscriptionAction.Unavailable=>L.Text("操作未完成；已尝试重新读取","Action incomplete; readback attempted"),_=>""
        });
        if(controller.Action==SubscriptionAction.Unavailable && controller.Field is {} field)status.Text+=" · "+(field switch
        {"manual.email"=>L.Text("请检查手工邮箱。","Check the manual email."),"manual.alias"=>L.Text("请检查别名的长度与字符。","Check alias length and characters."),"manual.membershipDate"=>L.Text("请检查会员日期与类型。","Check membership date and type."),_=>L.Text("请核对填写内容后重试。","Check the entered values before retrying.")});
        rows.Children.Clear();
        if(controller.List.State.Value is {} list)
        {
            if(list.Accounts.Count==0)rows.Children.Add(PresentationControls.Text(L.Text("尚无订阅资料。手工账号不会请求在线额度。","No subscription details yet. Manual accounts do not request online quota.")));
            foreach(var account in list.Accounts)
            {
                var row=new StackPanel{Spacing=8};row.Children.Add(PresentationControls.Text(QuotaPresentation.AccountTitle(account,L)+(account.Current?L.Text(" · 当前"," · Current"):account.Detected?L.Text(" · 已识别"," · Detected"):L.Text(" · 仅手工资料"," · Manual only")),18));
                row.Children.Add(PresentationControls.Text(QuotaPresentation.Plan(account.HasResolvedPlan?account.ResolvedPlan:null,L)+" · "+QuotaPresentation.SubscriptionDate(account,L)));
                if(account.HasDisplayEmail && QuotaPresentation.AccountTitle(account,L)!=account.DisplayEmail)row.Children.Add(PresentationControls.Text(account.DisplayEmail));
                row.Children.Add(PresentationControls.Text(L.Text("自动套餐：","Detected plan: ")+QuotaPresentation.Plan(account.HasAutomaticPlan?account.AutomaticPlan:null,L)+L.Text("；会员日期仅支持手工维护。","; membership dates are maintained manually.")));
                var actions=new StackPanel{Orientation=Orientation.Horizontal,Spacing=8};
                actions.Children.Add(Button(L.Text("编辑资料","Edit details"),()=>{controller.BeginEdit(account);return Task.CompletedTask;}));
                if(!account.Current)actions.Children.Add(Button(L.Text("删除资料…","Delete details…"),async()=>{if(await Confirm(L.Text("删除订阅资料","Delete subscription details"),L.Text("删除该条本机资料与对应归属历史；不会删除 Home 会话、Token、项目或 Agent 凭据。","Delete this local profile and its associated history. Home sessions, tokens, projects and Agent credentials are retained."),L.Text("删除资料","Delete details")))await controller.DeleteAsync(account);}));
                row.Children.Add(actions);
                if(account.Linked)row.Children.Add(Button(L.Text("取消手工关联…","Unlink manual details…"),async()=>{if(await Confirm(L.Text("取消关联","Unlink details"),L.Text("手工资料将恢复为独立账号。须先为手工资料填写邮箱。","Manual details will become a standalone account. Set their manual email first."),L.Text("取消关联","Unlink")))await controller.UnlinkAsync(account);}));
                foreach(var candidate in list.LinkCandidates.Where(candidate=>candidate.Reason=="same_email" && account.HasDetectedAccountId && candidate.DetectedAccountId==account.DetectedAccountId))
                {
                    var manual=list.Accounts.FirstOrDefault(item=>item.HasManualEntryId && item.ManualEntryId==candidate.ManualEntryId);if(manual is null)continue;
                    row.Children.Add(Button(L.Text("关联同邮箱手工资料：","Link same-email manual details: ")+QuotaPresentation.AccountTitle(manual,L),async()=>{if(await Confirm(L.Text("显式关联资料","Link details explicitly"),L.Text("仅关联所选资料；邮箱相同本身不合并账号或额度历史。","Link the selected profile. Matching email alone does not merge accounts or quota history."),L.Text("关联","Link")))await controller.LinkAsync(account,manual);}));
                }
                if(account.Current && account.LegacyQuotaHistory?.State==CodexLegacyQuotaHistoryState.Available)
                    row.Children.Add(Button(L.Text("关联未归属旧额度历史…","Associate unassigned legacy quota history…"),async()=>{if(await Confirm(L.Text("关联旧历史","Associate legacy history"),L.Text("仅将合同允许的旧额度历史关联到当前已确认账号，不扩大至其他资料。","Associate eligible legacy quota history with the current confirmed account."),L.Text("关联旧历史","Associate history")))await controller.LinkLegacyAsync(account);}));
                if(account.LegacyQuotaHistory?.State==CodexLegacyQuotaHistoryState.Linked)
                    row.Children.Add(Button(L.Text("取消旧历史关联…","Unlink legacy history…"),async()=>{if(await Confirm(L.Text("取消旧历史关联","Unlink legacy history"),L.Text("旧额度事实保留并恢复为未归属状态。","Legacy quota facts remain and return to the unassigned state."),L.Text("取消关联","Unlink")))await controller.UnlinkLegacyAsync(account);}));
                SetEnabled(row,!controller.Busy);rows.Children.Add(PresentationControls.Card(row));
            }
        }
        RenderEditor();SetEnabled(editor,!controller.Busy);
    }
    private static void SetEnabled(UIElement element,bool enabled)
    {if(element is Control control)control.IsEnabled=enabled;if(element is Panel panel)foreach(var child in panel.Children)SetEnabled(child,enabled);}
    private Task<bool> Confirm(string title,string message,string action)=>ProductDialog.Confirm(host,title,message,action);
    private void RenderEditor()
    {
        var draft=controller.Draft;
        if(ReferenceEquals(renderedDraft,draft))return;
        renderedDraft=draft;editor.Children.Clear();if(draft is null)return;
        editor.Children.Add(PresentationControls.Text(draft.Creating?L.Text("新增手工资料","New manual details"):L.Text("编辑手工字段","Edit manual fields"),20));
        if(draft.Original is {} original)editor.Children.Add(PresentationControls.Text(L.Text("自动邮箱：","Detected email: ")+(original.HasDetectedEmail?original.DetectedEmail:"—")+" · "+QuotaPresentation.Plan(original.HasAutomaticPlan?original.AutomaticPlan:null,L)));
        var email=new TextBox{Header=L.Text("手工邮箱（独立账号必填；留空使用自动资料）","Manual email (required for standalone accounts; blank uses detected details)"),Text=draft.Email,MaxLength=320};email.TextChanged+=(_,_)=>draft.Email=email.Text;editor.Children.Add(email);
        var alias=new TextBox{Header=L.Text("别名","Alias"),Text=draft.Alias,MaxLength=128};alias.TextChanged+=(_,_)=>draft.Alias=alias.Text;editor.Children.Add(alias);
        var plan=new ComboBox{Header=L.Text("手工套餐","Manual plan"),HorizontalAlignment=HorizontalAlignment.Stretch};
        plan.Items.Add(new ComboBoxItem{Content=L.Text("使用自动套餐","Use detected plan"),Tag=CodexSubscriptionPlan.Unspecified});
        foreach(var value in Enum.GetValues<CodexSubscriptionPlan>().Where(value=>value!=CodexSubscriptionPlan.Unspecified))plan.Items.Add(new ComboBoxItem{Content=QuotaPresentation.Plan(value,L),Tag=value});
        plan.SelectedItem=plan.Items.Cast<ComboBoxItem>().FirstOrDefault(item=>(CodexSubscriptionPlan)item.Tag==(draft.Plan??CodexSubscriptionPlan.Unspecified));
        plan.SelectionChanged+=(_,_)=>{if(plan.SelectedItem is ComboBoxItem{Tag:CodexSubscriptionPlan value})draft.Plan=value==CodexSubscriptionPlan.Unspecified?null:value;};editor.Children.Add(plan);
        var dateKind=new ComboBox{Header=L.Text("手工日期类型","Manual date type"),HorizontalAlignment=HorizontalAlignment.Stretch};
        foreach(var item in new[]{(CodexSubscriptionDateKind.Unspecified,L.Text("不设置","Not set")),(CodexSubscriptionDateKind.NextRenewal,L.Text("每月续费日","Monthly renewal day")),(CodexSubscriptionDateKind.MembershipExpiry,L.Text("会员到期日","Membership expiry"))})dateKind.Items.Add(new ComboBoxItem{Tag=item.Item1,Content=item.Item2});
        dateKind.SelectedItem=dateKind.Items.Cast<ComboBoxItem>().First(item=>(CodexSubscriptionDateKind)item.Tag==(draft.DateKind??CodexSubscriptionDateKind.Unspecified));
        var day=new NumberBox{Header=L.Text("每月几号","Day of month"),Minimum=1,Maximum=31,SmallChange=1,SpinButtonPlacementMode=NumberBoxSpinButtonPlacementMode.Compact,Value=DateOnly.TryParseExact(draft.MembershipDate,"yyyy-MM-dd",CultureInfo.InvariantCulture,DateTimeStyles.None,out var stored)?stored.Day:1};
        var expiry=new CalendarDatePicker{Header=L.Text("会员到期日","Membership expiry"),DateFormat="{year.full}-{month.integer(2)}-{day.integer(2)}",HorizontalAlignment=HorizontalAlignment.Stretch};
        if(DateOnly.TryParseExact(draft.MembershipDate,"yyyy-MM-dd",CultureInfo.InvariantCulture,DateTimeStyles.None,out stored))expiry.Date=new DateTimeOffset(stored.ToDateTime(TimeOnly.MinValue));
        void UpdateDate(){var kind=(CodexSubscriptionDateKind)((ComboBoxItem)dateKind.SelectedItem).Tag;draft.DateKind=kind==CodexSubscriptionDateKind.Unspecified?null:kind;day.Visibility=kind==CodexSubscriptionDateKind.NextRenewal?Visibility.Visible:Visibility.Collapsed;expiry.Visibility=kind==CodexSubscriptionDateKind.MembershipExpiry?Visibility.Visible:Visibility.Collapsed;
            draft.MembershipDate=kind==CodexSubscriptionDateKind.NextRenewal && double.IsFinite(day.Value) && day.Value==Math.Truncate(day.Value) && day.Value is>=1 and<=31?"2000-01-"+((int)day.Value).ToString("00",CultureInfo.InvariantCulture):kind==CodexSubscriptionDateKind.MembershipExpiry && expiry.Date is {} date?date.ToString("yyyy-MM-dd",CultureInfo.InvariantCulture):"";}
        dateKind.SelectionChanged+=(_,_)=>UpdateDate();day.ValueChanged+=(_,_)=>UpdateDate();expiry.DateChanged+=(_,_)=>UpdateDate();UpdateDate();
        editor.Children.Add(dateKind);editor.Children.Add(day);editor.Children.Add(expiry);
        var controls=new StackPanel{Spacing=8};controls.Children.Add(Button(L.Text("保存并核对","Save and check"),controller.SaveAsync));
        controls.Children.Add(Button(L.Text("保留草稿并载入最新版本","Rebase draft on latest details"),()=>{controller.RebaseDraft();return Task.CompletedTask;}));
        controls.Children.Add(Button(L.Text("取消编辑","Cancel editing"),()=>{controller.CancelDraft();return Task.CompletedTask;}));editor.Children.Add(controls);
    }
    private async Task ClearHistory()
    {
        if(!await Confirm(L.Text("清理旧账号额度历史","Clear previous-account quota history"),L.Text("仅清理非当前账号的额度窗口、观察和重置快照。当前账号、订阅资料、Home 会话、Token 和项目保持保留。","Clear non-current quota windows, observations and reset snapshots. Retain the current account, subscription details and Home usage."),L.Text("清理历史","Clear history")) || disposed)return;
        var result=await host.Client.MutateAsync((core,deadline,ct)=>core.ClearCodexAccountQuotaHistoryAsync(new(),deadline:deadline,cancellationToken:ct));
        var request=new CodexAccountQuotasRequest{EvaluatedAtMs=DateTimeOffset.Now.ToUnixTimeMilliseconds(),TimeZone=host.Workspace.TimeZone};
        var readback=await host.Client.QueryAsync("ListCodexAccountQuotas",request,host.Workspace.Context,["account","quota"],(core,deadline,ct)=>core.ListCodexAccountQuotasAsync(request,deadline:deadline,cancellationToken:ct),force:true);
        var clear=readback.Accounts.All(account=>account.Account.Current);
        var counts=result.Receipt is {} receipt?L.Text(" 回执："," Receipt: ")+receipt.AccountCount.ToString(L.Culture)+L.Text(" 个账号，"," accounts, ")+receipt.WindowCount.ToString(L.Culture)+L.Text(" 个窗口，"," windows, ")+receipt.ObservationCount.ToString(L.Culture)+L.Text(" 条观察，"," observations, ")+receipt.ResetSnapshotCount.ToString(L.Culture)+L.Text(" 条重置快照。"," reset snapshots."):L.Text(" 命令回执未知，未自动重发。"," Receipt unknown; command was not replayed.");
        host.ShowNotice(clear?"最新列表已确认无非当前账号额度历史。"+counts:"清理结果尚未确认，请重新读取账号额度。",clear?"Latest list confirms no previous-account quota history remains."+counts:"Clear result is unconfirmed. Reload account quotas.",clear?InfoBarSeverity.Informational:InfoBarSeverity.Warning);
        await controller.ReloadAsync();
    }
    public void Dispose(){disposed=true;controller.Changed-=Changed;controller.List.Changed-=Changed;}
}
