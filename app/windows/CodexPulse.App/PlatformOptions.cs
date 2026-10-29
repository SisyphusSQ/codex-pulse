using System.Text.Json;
using Microsoft.Win32;

namespace CodexPulse.App;

public sealed record PlatformOptions(string Runtime, string Home, string Helper, bool Background=false, bool Show=false,string? UpdateNonce=null,string? StartupNonce=null)
{
    public static PlatformOptions Parse()
    {
        var runtime = Environment.GetEnvironmentVariable("CODEX_PULSE_APP_RUNTIME") ??
            Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "CodexPulse", "development");
        var home = Environment.GetEnvironmentVariable("CODEX_HOME");
        if (string.IsNullOrWhiteSpace(home)) home = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".codex");
        var arguments = Environment.GetCommandLineArgs();
        var background=false;var show=false;string? updateNonce=null,startupNonce=null;
        for (var index = 1; index < arguments.Length; index++)
        {
            switch (arguments[index])
            {
                case "--runtime-directory" when index+1<arguments.Length: runtime = arguments[++index]; break;
                case "--codex-home" when index+1<arguments.Length: home = arguments[++index]; break;
                case "--background":background=true;break;
                case "--show":show=true;break;
                case "--quit-for-update" when index+1<arguments.Length: updateNonce=arguments[++index];if(!Guid.TryParseExact(updateNonce,"N",out _))throw new ArgumentException("update_nonce_invalid");break;
                case "--startup-token" when index+1<arguments.Length: startupNonce=arguments[++index];if(!Guid.TryParseExact(startupNonce,"N",out _))throw new ArgumentException("startup_nonce_invalid");break;
                default: throw new ArgumentException("startup_argument_unknown");
            }
        }
        if (!Path.IsPathFullyQualified(runtime) || !Path.IsPathFullyQualified(home)) throw new ArgumentException("startup_path_not_absolute");
        return new(Path.GetFullPath(runtime), Path.GetFullPath(home), Path.Combine(AppContext.BaseDirectory, "codex-pulse.exe"),background,show,updateNonce,startupNonce);
    }
}

public sealed record WindowPreferences(string Theme = "system", string Feature = "Summary", string? Provider = null, string? TrayProvider = null,
    int Width = 1280, int Height = 820)
{
    internal static string Key(string runtime) => CodexPulse.CoreClient.RuntimeProfile.Key(runtime);
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

// Paths are platform preferences, separate from Helper-owned business settings.
// Empty requests discovery; null leaves an inherited environment value intact.
public sealed record LaunchPreferences(string? CodexBinary=null,string? NodeBinary=null)
{
    public static LaunchPreferences Load(string runtime)
    {
        using var key=Registry.CurrentUser.OpenSubKey(WindowPreferences.Key(runtime));
        if(key?.GetValue("Launch") is not string value)return new();
        if(value.Length>32768)throw new IOException("launch_preferences_invalid");
        return JsonSerializer.Deserialize<LaunchPreferences>(value)??new();
    }
    public void Save(string runtime)
    {
        Validate(CodexBinary);Validate(NodeBinary);
        using var key=Registry.CurrentUser.CreateSubKey(WindowPreferences.Key(runtime));key.SetValue("Launch",JsonSerializer.Serialize(this),RegistryValueKind.String);
    }
    private static void Validate(string? path)
    {
        if(string.IsNullOrEmpty(path))return;
        if(!Path.IsPathFullyQualified(path) || !File.Exists(path) || !string.Equals(Path.GetExtension(path),".exe",StringComparison.OrdinalIgnoreCase))throw new ArgumentException("executable_path_invalid");
    }
    public void Apply()
    {
        Validate(CodexBinary);Validate(NodeBinary);
        if(CodexBinary is not null)Environment.SetEnvironmentVariable("CODEX_PULSE_CODEX_BINARY",string.IsNullOrWhiteSpace(CodexBinary)?null:CodexBinary);
        if(NodeBinary is not null)Environment.SetEnvironmentVariable("CODEX_PULSE_NODE_BINARY",string.IsNullOrWhiteSpace(NodeBinary)?null:NodeBinary);
    }
}
