using Codexpulse.Core.V1;

namespace CodexPulse.AppSupport;

public enum Feature { Summary, Overview, Sessions, Projects, Quota, AccountQuotas, ApiSubscriptions, LocalStatus, SourcesJobs, Settings }
public sealed record QueryContext(string Provider, DateOnly Start, DateOnly EndExclusive, string TimeZone, long Generation);

public sealed class Workspace
{
    public static readonly string[] ProviderOrder = ["codex", "cursor", "grok", "dsh"];
    public Feature Feature { get; private set; } = Feature.Summary;
    public string? Provider { get; private set; }
    public string? TrayProvider { get; private set; }
    public IReadOnlyList<string> EnabledProviders { get; private set; } = [];
    public long Generation { get; private set; }
    public DateOnly Start { get; private set; } = DateOnly.FromDateTime(DateTime.Today).AddDays(-6);
    public DateOnly EndExclusive { get; private set; } = DateOnly.FromDateTime(DateTime.Today).AddDays(1);
    public string TimeZone { get; private set; } = LocalTimeZone();
    public QueryContext Context => new(Provider ?? "", Start, EndExclusive, TimeZone, Generation);
    public event Action? Changed;

    public void ApplyProviders(IEnumerable<SettingsProviderSnapshot> providers)
    {
        var enabled = providers.Where(value => value.EffectiveState == ProviderEffectiveState.Enabled).Select(value => value.Provider).ToHashSet();
        EnabledProviders = ProviderOrder.Where(enabled.Contains).ToArray();
        if (Provider is null || !enabled.Contains(Provider)) Provider = EnabledProviders.FirstOrDefault();
        if (TrayProvider is null || !enabled.Contains(TrayProvider)) TrayProvider = EnabledProviders.FirstOrDefault();
        if (Provider is null && RequiresProvider(Feature)) Feature = Feature.Summary;
        if (Feature == Feature.AccountQuotas && Provider != "codex") Feature = Feature.Overview;
        Changed?.Invoke();
    }

    public void SelectProvider(string provider, bool tray = false)
    {
        if (!EnabledProviders.Contains(provider)) return;
        if (tray) { TrayProvider = provider; return; }
        if (Provider == provider) return;
        Provider = provider;
        if (Feature == Feature.AccountQuotas && provider != "codex") Feature = Feature.Overview;
        Changed?.Invoke();
    }

    public void Navigate(Feature feature)
    {
        if (RequiresProvider(feature) && Provider is null) feature = Feature.Summary;
        if (feature == Feature.AccountQuotas && Provider != "codex") feature = Feature.Overview;
        Feature = feature;
        Changed?.Invoke();
    }

    public void SetRange(DateOnly start, DateOnly endExclusive, string timeZone)
    {
        if (endExclusive <= start || endExclusive.DayNumber - start.DayNumber > 3660 || string.IsNullOrWhiteSpace(timeZone))
            throw new ArgumentException("invalid_range");
        Start = start; EndExclusive = endExclusive; TimeZone = timeZone;
        Changed?.Invoke();
    }

    public void ResetContext() { Generation++; Changed?.Invoke(); }
    public static bool RequiresProvider(Feature feature) => feature is Feature.Overview or Feature.Sessions or Feature.Projects or Feature.Quota or Feature.AccountQuotas;
    public static string LocalTimeZone()
    {
        var id = TimeZoneInfo.Local.Id;
        if (TimeZoneInfo.TryConvertWindowsIdToIanaId(id, out var iana)) return iana;
        if (id.Contains('/') || id == "UTC") return id;
        throw new InvalidOperationException("iana_time_zone_unavailable");
    }
}
