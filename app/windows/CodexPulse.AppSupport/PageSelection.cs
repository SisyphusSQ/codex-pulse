using Codexpulse.Core.V1;
namespace CodexPulse.AppSupport;

// Opaque identities and exact range live only for this in-process drilldown.
public sealed record PageSelection(Feature Feature, string Identity, UTCTimeRange Range);
