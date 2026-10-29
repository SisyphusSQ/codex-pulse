using Codexpulse.Core.V1;

namespace CodexPulse.AppSupport;

public static class TrayPresentation
{
    public const string ClipboardText="Codex Pulse · Usage snapshot · Account details hidden";
    public static bool CanShowQuota(string provider,QuotaCurrentResponse? quota,AccountSnapshotResponse? account)=>
        quota?.ProviderContext?.EffectiveProvider==provider &&
        (provider!="codex" || QuotaPresentation.SameAccount(quota.Current?.Binding,account?.Binding));
    public static string Identity(AccountSnapshotResponse? account,bool privacy,Localization l)
        =>Identity("codex",account,privacy,l);
    public static string Identity(string provider,AccountSnapshotResponse? account,bool privacy,Localization l)
    {
        if(privacy)return l.Text("账号资料已隐藏","Account details hidden");
        if(provider is "cursor" or "grok")
        {
            if(account?.Account is not {} identity || identity.Type!=provider)return l.Text("账号资料暂不可用","Account details unavailable");
            var email=identity.HasEmail && identity.Email.Length>0?identity.Email:l.Text("邮箱未提供","Email not provided");
            var plan=identity.HasPlanType && identity.PlanType.Length>0?identity.PlanType:l.Text("套餐未提供","Plan not provided");
            if(provider=="cursor")plan=plan.ToLowerInvariant() switch{"pro_plus" or "pro+"=>"Pro+","pro"=>"Pro","ultra"=>"Ultra","free"=>"Free",_=>plan};
            return email+" · "+plan;
        }
        if(provider!="codex")return l.Text("此客户端不提供账号资料","This client does not provide account details");
        if(account?.Subscription is not {} profile)return account?.Account?.Email??l.Text("当前已确认账号","Current confirmed account");
        return QuotaPresentation.AccountTitle(profile,l)+" · "+QuotaPresentation.Plan(profile.HasResolvedPlan?profile.ResolvedPlan:null,l)+"\n"+QuotaPresentation.SubscriptionDate(profile,l);
    }
}
