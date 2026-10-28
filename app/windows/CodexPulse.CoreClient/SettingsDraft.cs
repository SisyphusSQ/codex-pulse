using Codexpulse.Core.V1;

namespace CodexPulse.CoreClient;

public static class SettingsDraft
{
    // Copy only editable fields from the latest authoritative snapshot, retaining its revision.
    public static UpdateSettingsRequest From(SettingsSnapshot snapshot)
    {
        var result = new UpdateSettingsRequest
        {
            ExpectedRevision = snapshot.Revision,
            Online = new SettingsOnlineUpdate
            {
                QuotaEnabled = snapshot.Online.QuotaEnabled, ResetCreditsEnabled = snapshot.Online.ResetCreditsEnabled,
                GrokQuotaEnabled = snapshot.Online.GrokQuotaEnabled, GrokAutoRefreshEnabled = snapshot.Online.GrokAutoRefreshEnabled,
                CursorOnlineEnabled = snapshot.Online.CursorOnlineEnabled
            },
            Refresh = new SettingsRefreshUpdate
            {
                QuotaIntervalSeconds = snapshot.Refresh.QuotaIntervalSeconds, ResetCreditsIntervalSeconds = snapshot.Refresh.ResetCreditsIntervalSeconds,
                ReconcileIntervalSeconds = snapshot.Refresh.ReconcileIntervalSeconds, JsonlDebounceMilliseconds = snapshot.Refresh.JsonlDebounceMilliseconds
            },
            Updates = new SettingsUpdatesUpdate
            {
                AutoCheckEnabled = snapshot.Updates.AutoCheckEnabled, CheckIntervalSeconds = snapshot.Updates.CheckIntervalSeconds,
                Channel = snapshot.Updates.Channel
            },
            Ui = new SettingsUIUpdate { Locale = snapshot.Ui.Locale, LaunchBehavior = snapshot.Ui.LaunchBehavior, OverviewRange = snapshot.Ui.OverviewRange },
            CodexAccounts = new SettingsCodexAccountsUpdate { RetainQuotaHistory = snapshot.CodexAccounts.RetainQuotaHistory }
        };
        result.Providers.AddRange(snapshot.Providers.Select(provider => new SettingsProviderUpdate { Provider = provider.Provider, Intent = provider.Intent }));
        return result;
    }
}
