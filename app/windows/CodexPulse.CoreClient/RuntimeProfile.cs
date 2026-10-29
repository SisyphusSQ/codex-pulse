using System.Security.Cryptography;
using System.Text;

namespace CodexPulse.CoreClient;

public static class RuntimeProfile
{
    public static string Key(string runtime)=>@"Software\CodexPulse\Windows\Profiles\"+Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(Path.GetFullPath(runtime).ToUpperInvariant())));
}
