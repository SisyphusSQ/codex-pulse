using System.Text.Json;
using System.Security.Cryptography;
using System.Text;
using Microsoft.Win32;

namespace CodexPulse.App;

public sealed record PlatformOptions(string Runtime, string Home, string Helper)
{
    public static PlatformOptions Parse()
    {
        var runtime = Environment.GetEnvironmentVariable("CODEX_PULSE_APP_RUNTIME") ??
            Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "CodexPulse", "development");
        var home = Environment.GetEnvironmentVariable("CODEX_HOME");
        if (string.IsNullOrWhiteSpace(home)) home = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".codex");
        var arguments = Environment.GetCommandLineArgs();
        for (var index = 1; index < arguments.Length; index++)
        {
            if (index + 1 >= arguments.Length) throw new ArgumentException("startup_argument_missing");
            switch (arguments[index])
            {
                case "--runtime-directory": runtime = arguments[++index]; break;
                case "--codex-home": home = arguments[++index]; break;
                default: throw new ArgumentException("startup_argument_unknown");
            }
        }
        if (!Path.IsPathFullyQualified(runtime) || !Path.IsPathFullyQualified(home)) throw new ArgumentException("startup_path_not_absolute");
        return new(Path.GetFullPath(runtime), Path.GetFullPath(home), Path.Combine(AppContext.BaseDirectory, "codex-pulse.exe"));
    }
}

public sealed record WindowPreferences(string Theme = "system", string Feature = "Summary", string? Provider = null, string? TrayProvider = null,
    int Width = 1280, int Height = 820)
{
    private static string Key(string runtime) => @"Software\CodexPulse\Windows\Profiles\" +
        Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(runtime.ToUpperInvariant())));
    public static WindowPreferences Load(string runtime)
    {
        using var key = Registry.CurrentUser.OpenSubKey(Key(runtime));
        if (key?.GetValue("Window") is not string value) return new();
        if (value.Length > 8192) throw new IOException("window_preferences_invalid");
        return JsonSerializer.Deserialize<WindowPreferences>(value) ?? new();
    }
    public void Save(string runtime)
    {
        using var key = Registry.CurrentUser.CreateSubKey(Key(runtime));
        key.SetValue("Window", JsonSerializer.Serialize(this), RegistryValueKind.String);
    }
}
