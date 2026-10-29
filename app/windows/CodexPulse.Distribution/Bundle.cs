using System.Security.Cryptography;
using System.Text.Json;
using System.Text.Json.Serialization;
using System.Text.RegularExpressions;

namespace CodexPulse.Distribution;

public sealed record UpdatePolicy(int Format,string PackageId,string PublicKeySpki,string FeedBaseUri,bool Development,string DevelopmentTlsCertificate)
{
    public static UpdatePolicy Embedded()
    {
        using var stream=typeof(UpdatePolicy).Assembly.GetManifestResourceStream("CodexPulse.UpdatePolicy")??throw new IOException("update_policy_missing");
        return JsonSerializer.Deserialize<UpdatePolicy>(stream,BundleFormat.Json)??throw new IOException("update_policy_invalid");
    }
    public bool Configured=>Format==1 && PackageId==BundleFormat.PackageId && PublicKeySpki.Length>0 && Uri.TryCreate(FeedBaseUri,UriKind.Absolute,out var uri) && uri.Scheme=="https";
    public Uri Feed(string channel)
    {
        if(!Configured || channel is not("stable" or "prerelease"))throw new IOException("updates_unconfigured");
        var root=new Uri(FeedBaseUri);
        if(root.UserInfo.Length!=0 || root.Query.Length!=0 || root.Fragment.Length!=0 || !root.AbsolutePath.EndsWith('/') || root.IdnHost!=root.Host || root.Host.EndsWith('.'))throw new IOException("update_origin_invalid");
        if(DevelopmentTlsCertificate.Length>0 && (!Development || !root.IsLoopback))throw new IOException("update_development_trust_invalid");
        return new(root,channel+".json");
    }
}
public sealed record BundleFile(string Path,long Size,string SHA256);
public sealed record ReleaseManifest(int Format,string PackageId,long BuildNumber,string Version,string Channel,string Architecture,int MinimumWindowsBuild,string CoreContract,string DataCompatibility,string ZipName,long ZipSize,string ZipSHA256,IReadOnlyList<BundleFile> Files);
public sealed record SignedRelease(string Payload,string Signature);
public sealed record VerifiedRelease(ReleaseManifest Manifest,byte[] Envelope,string EnvelopeSHA256);

public static partial class BundleFormat
{
    public const string PackageId="CodexPulse.Windows",Architecture="win-x64",CoreContract="core-rpc-v9";
    public const int MinimumWindowsBuild=26100,MaximumFiles=8192,MaximumEnvelopeBytes=4*1024*1024;
    public const long MaximumZipBytes=512L*1024*1024,MaximumFileBytes=256L*1024*1024,MaximumExpandedBytes=1024L*1024*1024;
    public static readonly JsonSerializerOptions Json=new(){UnmappedMemberHandling=JsonUnmappedMemberHandling.Disallow};
    [GeneratedRegex("^[0-9a-fA-F]{64}$")]private static partial Regex HashPattern();
    [GeneratedRegex("^[0-9A-Za-z.+-]{1,80}$")]private static partial Regex VersionPattern();
    public static bool HashValid(string? value)=>value is not null && HashPattern().IsMatch(value);
    public static string Hash(byte[] bytes)=>Convert.ToHexStringLower(SHA256.HashData(bytes));
    public static string VersionDirectory(long number)=>"b"+number.ToString("D12",System.Globalization.CultureInfo.InvariantCulture);
    public static void ValidatePath(string path)
    {
        if(string.IsNullOrEmpty(path) || path.Length>1024 || path.Contains('\\') || path.Contains(':') || path.Contains('\0') || path.StartsWith('/') || path.Split('/').Length>16 || path.Split('/').Any(part=>part.Length==0 || part is "." or ".." || part.EndsWith('.') || part.EndsWith(' ') || part.IndexOfAny(['<','>','"','|','?','*'])>=0))throw new IOException("package_path_invalid");
        foreach(var part in path.Split('/'))
        {var stem=part.Split('.')[0];if(stem.Equals("CON",StringComparison.OrdinalIgnoreCase) || stem.Equals("PRN",StringComparison.OrdinalIgnoreCase) || stem.Equals("AUX",StringComparison.OrdinalIgnoreCase) || stem.Equals("NUL",StringComparison.OrdinalIgnoreCase) || Regex.IsMatch(stem,"^(COM|LPT)[1-9]$",RegexOptions.IgnoreCase|RegexOptions.CultureInvariant))throw new IOException("package_device_path_invalid");}
    }
    public static void Validate(ReleaseManifest value)
    {
        if(value.Format!=1 || value.PackageId!=PackageId || value.BuildNumber<=0 || value.Version is null || !VersionPattern().IsMatch(value.Version) || value.Channel is not("stable" or "prerelease") || value.Architecture!=Architecture || value.MinimumWindowsBuild<MinimumWindowsBuild || value.CoreContract is null || value.CoreContract.Length>32 || !Regex.IsMatch(value.CoreContract,"^core-rpc-v[1-9][0-9]*$",RegexOptions.CultureInvariant) || string.IsNullOrEmpty(value.DataCompatibility) || value.DataCompatibility.Length>256 || value.DataCompatibility.Any(char.IsControl) || value.ZipName!=$"codex-pulse-{value.BuildNumber}-win-x64.zip" || value.ZipSize<=0 || value.ZipSize>MaximumZipBytes || !HashValid(value.ZipSHA256) || value.Files is null || value.Files.Count is <1 or >MaximumFiles)throw new IOException("package_manifest_invalid");
        var names=new HashSet<string>(StringComparer.OrdinalIgnoreCase);long size=0;
        foreach(var file in value.Files)
        {
            ValidatePath(file.Path);
            if(!names.Add(file.Path) || file.Path.Equals("release.json",StringComparison.OrdinalIgnoreCase) || file.Size<0 || file.Size>MaximumFileBytes || !HashValid(file.SHA256))throw new IOException("package_file_invalid");
            size=checked(size+file.Size);if(size>MaximumExpandedBytes)throw new IOException("package_expanded_limit");
        }
        foreach(var file in new[]{"CodexPulse.App.exe","CodexPulse.App.pri","codex-pulse.exe","CodexPulse.PackageHost.exe","bundle-info.json"})if(!names.Contains(file))throw new IOException("package_required_file_missing");
    }
    public static VerifiedRelease Verify(ReadOnlySpan<byte> bytes,UpdatePolicy policy)
    {
        if(bytes.Length is <1 or >MaximumEnvelopeBytes || policy.Format!=1 || policy.PackageId!=PackageId || policy.PublicKeySpki.Length==0)throw new IOException("package_signature_invalid");
        try
        {
            var envelope=JsonSerializer.Deserialize<SignedRelease>(bytes,Json)??throw new IOException("package_signature_invalid");
            var payload=Convert.FromBase64String(envelope.Payload);var signature=Convert.FromBase64String(envelope.Signature);
            if(payload.Length>2*1024*1024 || signature.Length>144)throw new IOException("package_signature_invalid");
            using var key=ECDsa.Create();key.ImportSubjectPublicKeyInfo(Convert.FromBase64String(policy.PublicKeySpki),out var read);
            if(read!=Convert.FromBase64String(policy.PublicKeySpki).Length || key.KeySize!=256 || !key.VerifyData(payload,signature,HashAlgorithmName.SHA256,DSASignatureFormat.Rfc3279DerSequence))throw new IOException("package_signature_invalid");
            var manifest=JsonSerializer.Deserialize<ReleaseManifest>(payload,Json)??throw new IOException("package_manifest_invalid");Validate(manifest);
            var copy=bytes.ToArray();return new(manifest,copy,Hash(copy));
        }
        catch(Exception e)when(e is CryptographicException or FormatException or JsonException or ArgumentException or OverflowException){throw new IOException("package_signature_invalid");}
    }
    public static byte[] Sign(ReleaseManifest manifest,ECDsa key)
    {
        Validate(manifest);if(key.KeySize!=256)throw new ArgumentException("signing_key_invalid");
        var payload=JsonSerializer.SerializeToUtf8Bytes(manifest,Json);var signature=key.SignData(payload,HashAlgorithmName.SHA256,DSASignatureFormat.Rfc3279DerSequence);
        return JsonSerializer.SerializeToUtf8Bytes(new SignedRelease(Convert.ToBase64String(payload),Convert.ToBase64String(signature)),Json);
    }
}
