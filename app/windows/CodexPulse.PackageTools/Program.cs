using System.Diagnostics;
using System.IO.Compression;
using System.Net;
using System.Net.Security;
using System.Net.Sockets;
using System.Reflection.Metadata;
using System.Reflection.PortableExecutable;
using System.Security.Authentication;
using System.Security.Cryptography;
using System.Security.Cryptography.X509Certificates;
using System.Text;
using System.Text.Json;
using CodexPulse.Distribution;

internal static class Program
{
    private const string SigningKey="CodexPulse.Windows.DevelopmentUpdateSigning",TLSKey="CodexPulse.Windows.DevelopmentUpdateTLS";
    private sealed record HelperMetadata(string Version,string OperatingSystem,string Architecture,string CoreContract,int DatabaseSchema,int PreferencesSchema,string CredentialSchema);
    private static ECDsaCng Key(string name)
    {
        using var key=CngKey.Exists(name)?CngKey.Open(name):CngKey.Create(CngAlgorithm.ECDsaP256,name,new(){ExportPolicy=CngExportPolicies.None,KeyUsage=CngKeyUsages.Signing});
        return new(key);
    }
    private static UpdatePolicy ReadPolicy(string path)=>JsonSerializer.Deserialize<UpdatePolicy>(File.ReadAllBytes(path),BundleFormat.Json)??throw new IOException("development_policy_invalid");
    private static async Task<int> Main(string[] args)
    {
        if(args.Length==2 && args[0]=="ensure-directory"){using var directory=PinnedDirectory.Open(Path.GetFullPath(args[1]),true,true);Console.WriteLine("Private distribution directory ready");return 0;}
        if(args.Length==3 && args[0]=="dev-policy")
        {
            using var key=Key(SigningKey);var publicKey=Convert.ToBase64String(key.ExportSubjectPublicKeyInfo());
            if(File.Exists(args[1])){var existing=ReadPolicy(args[1]);if(existing.PublicKeySpki!=publicKey || existing.FeedBaseUri!=args[2] || !existing.Development)throw new IOException("development_policy_changed");Console.WriteLine("Existing protected CNG signing key and development policy retained");return 0;}
            using var tlsKey=Key(TLSKey);var request=new CertificateRequest("CN=localhost",tlsKey,HashAlgorithmName.SHA256);
            request.CertificateExtensions.Add(new X509BasicConstraintsExtension(false,false,0,true));request.CertificateExtensions.Add(new X509KeyUsageExtension(X509KeyUsageFlags.DigitalSignature,true));
            request.CertificateExtensions.Add(new X509EnhancedKeyUsageExtension(new OidCollection{new("1.3.6.1.5.5.7.3.1")},true));
            var names=new SubjectAlternativeNameBuilder();names.AddDnsName("localhost");names.AddIpAddress(IPAddress.Loopback);request.CertificateExtensions.Add(names.Build());
            using var certificate=request.CreateSelfSigned(DateTimeOffset.UtcNow.AddMinutes(-5),DateTimeOffset.UtcNow.AddDays(7));
            var policy=new UpdatePolicy(1,BundleFormat.PackageId,publicKey,args[2],true,Convert.ToBase64String(certificate.RawData));_ = policy.Feed("prerelease");
            using var directory=PinnedDirectory.Open(Path.GetDirectoryName(Path.GetFullPath(args[1]))!,true,true);directory.AtomicWrite(Path.GetFileName(args[1]),JsonSerializer.SerializeToUtf8Bytes(policy,BundleFormat.Json));
            Console.WriteLine("Development signing/TLS keys created in non-exportable current-user CNG storage; no system certificate trust changed");return 0;
        }
        if(args.Length==8 && args[0]=="pack")
        {
            var source=Path.GetFullPath(args[1]);var output=Path.GetFullPath(args[2]);var version=args[3];var build=long.Parse(args[4],System.Globalization.CultureInfo.InvariantCulture);var channel=args[5];var policy=ReadPolicy(args[6]);var keyName=args[7];
            using var key=Key(keyName);if(Convert.ToBase64String(key.ExportSubjectPublicKeyInfo())!=policy.PublicKeySpki)throw new IOException("package_signing_root_mismatch");
            var helper=await HelperInfo(Path.Combine(source,"codex-pulse.exe"));var appVersion=AppVersion(Path.Combine(source,"CodexPulse.App.dll"));
            if(helper.Version!=version || appVersion!=version || helper.OperatingSystem!="windows" || helper.Architecture!="amd64" || helper.CoreContract!=BundleFormat.CoreContract || !BundleFormat.HashValid(helper.CredentialSchema))throw new IOException("package_software_pair_mismatch");
            var compatibility=$"sqlite{helper.DatabaseSchema}-prefs{helper.PreferencesSchema}-credentials-{helper.CredentialSchema}";
            var info=new BundleInfo(version,build,BundleFormat.Architecture,helper.CoreContract,compatibility,policy.Development?"development-package-signature; Authenticode not asserted":"Authenticode verification required separately");
            using(var directory=PinnedDirectory.Open(source))directory.AtomicWrite("bundle-info.json",JsonSerializer.SerializeToUtf8Bytes(info,BundleFormat.Json));
            var files=new List<BundleFile>();using(var directory=PinnedDirectory.Open(source))Walk(source,"",directory,files);
            using var destination=PinnedDirectory.Open(output,true,true);var name=$"codex-pulse-{build}-win-x64.zip";
            using(var target=destination.Create(name))using(var archive=new ZipArchive(target,ZipArchiveMode.Create,true))
                foreach(var file in files){var entry=archive.CreateEntry(file.Path,CompressionLevel.Optimal);using var src=PinnedDirectory.Open(source);using var input=src.Read(file.Path);using var stream=entry.Open();input.CopyTo(stream);}
            using var zip=destination.Read(name);var manifest=new ReleaseManifest(1,BundleFormat.PackageId,build,version,channel,BundleFormat.Architecture,BundleFormat.MinimumWindowsBuild,helper.CoreContract,compatibility,name,zip.Length,PinnedDirectory.FileHash(zip),files);
            var envelope=BundleFormat.Sign(manifest,key);_ = BundleFormat.Verify(envelope,policy);destination.AtomicWrite("release.json",envelope);destination.AtomicWrite(channel+".json",envelope);
            Console.WriteLine(JsonSerializer.Serialize(new{version,build,channel,Files=files.Count,ZipBytes=manifest.ZipSize,manifest.ZipSHA256,DataCompatibility=compatibility,Trust=info.WindowsTrust}));return 0;
        }
        if(args.Length==3 && args[0]=="serve")
        {await Serve(Path.GetFullPath(args[1]),ReadPolicy(args[2]));return 0;}
        throw new ArgumentException("usage: dev-policy <policy> <https-loopback-base> | pack <source> <output> <version> <build> <channel> <policy> <CNG-key-name> | serve <feed-directory> <policy>");
    }
    private static void Walk(string root,string relative,PinnedDirectory directory,List<BundleFile> files)
    {
        foreach(var item in new DirectoryInfo(Path.Combine(root,relative)).EnumerateFileSystemInfos().OrderBy(v=>v.Name,StringComparer.Ordinal))
        {
            if((item.Attributes&FileAttributes.ReparsePoint)!=0)throw new IOException("package_source_reparse_rejected");
            var name=relative.Length==0?item.Name:relative+"/"+item.Name;BundleFormat.ValidatePath(name);
            if(item is DirectoryInfo)Walk(root,name,directory,files);
            else{using var file=directory.Read(name);if(file.Length>BundleFormat.MaximumFileBytes || files.Count>=BundleFormat.MaximumFiles)throw new IOException("package_source_limit");files.Add(new(name,file.Length,PinnedDirectory.FileHash(file)));}
        }
    }
    private static async Task<HelperMetadata> HelperInfo(string path)
    {
        using var timeout=new CancellationTokenSource(TimeSpan.FromSeconds(10));var start=new ProcessStartInfo(path){UseShellExecute=false,CreateNoWindow=true,WindowStyle=ProcessWindowStyle.Hidden,RedirectStandardOutput=true,RedirectStandardError=true};start.ArgumentList.Add("--build-info");
        using var process=Process.Start(start)??throw new IOException("helper_metadata_start_failed");var text=await process.StandardOutput.ReadToEndAsync(timeout.Token);await process.WaitForExitAsync(timeout.Token);
        if(process.ExitCode!=0 || text.Length>8192)throw new IOException("helper_metadata_failed");return JsonSerializer.Deserialize<HelperMetadata>(text,BundleFormat.Json)??throw new IOException("helper_metadata_invalid");
    }
    private static string AppVersion(string path)
    {
        using var file=File.OpenRead(path);using var pe=new PEReader(file);var reader=pe.GetMetadataReader();
        foreach(var handle in reader.GetAssemblyDefinition().GetCustomAttributes())
        {
            var attribute=reader.GetCustomAttribute(handle);if(attribute.Constructor.Kind!=HandleKind.MemberReference)continue;
            var member=reader.GetMemberReference((MemberReferenceHandle)attribute.Constructor);if(member.Parent.Kind!=HandleKind.TypeReference)continue;
            var type=reader.GetTypeReference((TypeReferenceHandle)member.Parent);if(reader.GetString(type.Name)!="AssemblyMetadataAttribute")continue;
            var blob=reader.GetBlobReader(attribute.Value);if(blob.ReadUInt16()!=1)continue;var key=blob.ReadSerializedString();var value=blob.ReadSerializedString();if(key=="PulseVersion")return value??"";
        }
        throw new IOException("app_version_metadata_missing");
    }
    private static async Task Serve(string directory,UpdatePolicy policy)
    {
        var uri=policy.Feed("prerelease");if(!policy.Development || !uri.IsLoopback)throw new IOException("development_server_loopback_required");
        using var publicCertificate=X509CertificateLoader.LoadCertificate(Convert.FromBase64String(policy.DevelopmentTlsCertificate));using var key=Key(TLSKey);using var certificate=publicCertificate.CopyWithPrivateKey(key);
        using var root=PinnedDirectory.Open(directory,requirePrivate:true);var listener=new TcpListener(IPAddress.Loopback,uri.Port);listener.Start(8);Console.WriteLine("Controlled HTTPS update fixture listening on loopback port "+uri.Port);
        try{while(true){using var socket=await listener.AcceptTcpClientAsync();socket.ReceiveTimeout=socket.SendTimeout=10000;using var stream=new SslStream(socket.GetStream(),false);
            try
            {
                using var timeout=new CancellationTokenSource(TimeSpan.FromSeconds(30));await stream.AuthenticateAsServerAsync(new SslServerAuthenticationOptions{ServerCertificate=certificate,EnabledSslProtocols=SslProtocols.Tls12|SslProtocols.Tls13},timeout.Token);
                var header=new List<byte>();var buffer=new byte[1];while(header.Count<16384){var count=await stream.ReadAsync(buffer,timeout.Token);if(count==0)break;header.Add(buffer[0]);if(header.Count>=4 && header.TakeLast(4).SequenceEqual(new byte[]{13,10,13,10}))break;}
                var first=Encoding.ASCII.GetString(header.ToArray()).Split("\r\n")[0].Split(' ');var name=first.Length==3 && first[0]=="GET"?Uri.UnescapeDataString(first[1].TrimStart('/')):"";
                if(name is not("stable.json" or "prerelease.json") && !System.Text.RegularExpressions.Regex.IsMatch(name,"^codex-pulse-[0-9]+-win-x64\\.zip$"))throw new IOException("fixture_route_invalid");
                using var file=root.Read(name);var response=Encoding.ASCII.GetBytes($"HTTP/1.1 200 OK\r\nContent-Length: {file.Length}\r\nContent-Type: application/octet-stream\r\nConnection: close\r\n\r\n");await stream.WriteAsync(response,timeout.Token);await file.CopyToAsync(stream,timeout.Token);
            }
            catch(Exception error)when(error is IOException or AuthenticationException or OperationCanceledException){/* Bounded, test-only connections; do not log request bodies or TLS data. */}
        }}finally{listener.Stop();}
    }
}
