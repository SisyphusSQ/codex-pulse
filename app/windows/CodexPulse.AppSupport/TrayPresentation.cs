using Codexpulse.Core.V1;

namespace CodexPulse.AppSupport;

public static class TrayPresentation
{
    public const string ClipboardText="Codex Pulse · Usage snapshot · Account details hidden";
    public static bool CanShowQuota(string provider,QuotaCurrentResponse? quota,AccountSnapshotResponse? account)=>
        quota?.ProviderContext?.EffectiveProvider==provider &&
        (provider!="codex" || QuotaPresentation.SameAccount(quota.Current?.Binding,account?.Binding));
    public static string Identity(AccountSnapshotResponse? account,bool privacy,Localization l)
    {
        if(privacy)return l.Text("账号资料已隐藏","Account details hidden");
        if(account?.Subscription is not {} profile)return account?.Account?.Email??l.Text("当前已确认账号","Current confirmed account");
        return QuotaPresentation.AccountTitle(profile,l)+" · "+QuotaPresentation.Plan(profile.HasResolvedPlan?profile.ResolvedPlan:null,l)+"\n"+QuotaPresentation.SubscriptionDate(profile,l);
    }
}
