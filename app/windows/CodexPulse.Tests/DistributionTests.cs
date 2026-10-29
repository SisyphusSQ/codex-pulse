using System.IO.Compression;
using System.Security.Cryptography;
using System.Text.Json;
using CodexPulse.CoreClient;
using CodexPulse.Distribution;
using Microsoft.Win32;

internal static class DistributionTests
{
    private const string Compatibility="sqlite38-prefs5-credentials1";
    private static void Require(bool value,string name){if(!value)throw new InvalidOperationException(name);Console.WriteLine("PASS "+name);}
    private static void Reject(Action action,string name){var failed=false;try{action();}catch(IOException){failed=true;}Require(failed,name);}
    public static void Run(string root)
    {
        PrivateRuntime.Ensure(root);using var key=ECDsa.Create(ECCurve.NamedCurves.nistP256);var policy=new UpdatePolicy(1,BundleFormat.PackageId,Convert.ToBase64String(key.ExportSubjectPublicKeyInfo()),"https://localhost:18731/",true,"");
        (string Zip,VerifiedRelease Release) Fixture(long number,string compatibility=Compatibility)
        {
            var source=Path.Combine(root,"source-"+number);PrivateRuntime.Ensure(source);
            var native=Path.Combine(System.Runtime.InteropServices.RuntimeEnvironment.GetRuntimeDirectory(),"..","..","..","dotnet.exe");
            native=Path.GetFullPath(native);if(!File.Exists(native))native=Environment.ProcessPath!;
            foreach(var name in new[]{"CodexPulse.App.exe","codex-pulse.exe","CodexPulse.PackageHost.exe"})File.Copy(native,Path.Combine(source,name));
            File.WriteAllBytes(Path.Combine(source,"CodexPulse.App.pri"),[1,2,3]);
            var version="0.0.0-windows-dev."+number;
            File.WriteAllBytes(Path.Combine(source,"bundle-info.json"),JsonSerializer.SerializeToUtf8Bytes(new BundleInfo(version,number,BundleFormat.Architecture,BundleFormat.CoreContract,compatibility,"NotSigned")));
            var files=Directory.GetFiles(source).Order().Select(path=>new BundleFile(Path.GetFileName(path),new FileInfo(path).Length,BundleFormat.Hash(File.ReadAllBytes(path)))).ToArray();
            var zip=Path.Combine(root,$"codex-pulse-{number}-win-x64.zip");ZipFile.CreateFromDirectory(source,zip);
            var manifest=new ReleaseManifest(1,BundleFormat.PackageId,number,version,"prerelease",BundleFormat.Architecture,BundleFormat.MinimumWindowsBuild,BundleFormat.CoreContract,compatibility,Path.GetFileName(zip),new FileInfo(zip).Length,BundleFormat.Hash(File.ReadAllBytes(zip)),files);
            return(zip,BundleFormat.Verify(BundleFormat.Sign(manifest,key),policy));
        }
        using(var directory=PinnedDirectory.Open(Path.Combine(root,"private"),true,true))
        {
            using(var file=directory.Create("中文 空格#%/sample.txt"))file.Write([1,2,3]);
            using(var file=directory.Read("中文 空格#%/sample.txt"))Require(file.ReadByte()==1,"distribution opens Chinese and special-character paths through pinned Windows handles");
            using(var trailing=PinnedDirectory.Open(directory.Path+Path.DirectorySeparatorChar))using(var file=trailing.Read("中文 空格#%/sample.txt"))Require(file.ReadByte()==1,"AppContext base directories with trailing separators resolve legitimate paired package files");
            Reject(()=>directory.Read("missing"),"distribution read and occupancy checks never create missing files");Require(!File.Exists(directory.Resolve("missing")),"failed distribution reads leave the filesystem unchanged");
            foreach(var name in new[]{"../escape","sample:stream","CON.txt","a\\b","a/../b"})Reject(()=>directory.Resolve(name),"distribution rejects unsafe path "+name);
        }
        var first=Fixture(1001);var second=Fixture(1002);var future=Fixture(1003,"sqlite99-prefs5-credentials1");
        using var wrong=ECDsa.Create(ECCurve.NamedCurves.nistP256);var wrongPolicy=policy with{PublicKeySpki=Convert.ToBase64String(wrong.ExportSubjectPublicKeyInfo())};
        Reject(()=>BundleFormat.Verify(first.Release.Envelope,wrongPolicy),"a different signing root cannot authorize an update package");
        var envelope=JsonSerializer.Deserialize<SignedRelease>(first.Release.Envelope)!;var signature=Convert.FromBase64String(envelope.Signature);signature[^1]^=1;
        Reject(()=>BundleFormat.Verify(JsonSerializer.SerializeToUtf8Bytes(envelope with{Signature=Convert.ToBase64String(signature)}),policy),"modified signatures are rejected before extraction or version switching");
        var installation=new Installation(Path.Combine(root,"install"),policy);var gates=0;var one=installation.Install(first.Zip,first.Release,()=>gates++);
        Require(gates==1 && installation.Current()==one && File.Exists(installation.LaunchPath()),"first installation verifies the exact App Helper and package host before publishing a usable current version");
        File.WriteAllText(Path.Combine(installation.VersionPath(one),"unsigned.dll"),"synthetic unsigned file");
        Reject(()=>installation.VerifyInstalled(one),"installed package refuses extra unsigned files that could affect DLL loading");File.Delete(Path.Combine(installation.VersionPath(one),"unsigned.dll"));
        using(var current=PinnedDirectory.Open(installation.Root))current.AtomicWrite("journal.json",JsonSerializer.SerializeToUtf8Bytes(new InstallJournal(one,new(1002,"0.0.0-windows-dev.1002",second.Release.EnvelopeSHA256),"prepared"),BundleFormat.Json));
        Require(installation.Current()==one,"interrupted prepared journal retains the prior authoritative pointer without touching runtime data");
        Reject(()=>installation.Install(second.Zip,second.Release,()=>throw new IOException("uncertain_shutdown")),"an uncertain shutdown prevents the N-1 pointer switch");Require(installation.Current()==one && !Directory.GetDirectories(installation.Root,".stage-*").Any(),"failed shutdown keeps the previous installation and removes its staged replacement");
        using(var occupied=new FileStream(Path.Combine(installation.VersionPath(one),"codex-pulse.exe"),FileMode.Open,FileAccess.Read,FileShare.None))Reject(()=>installation.Install(second.Zip,second.Release,()=>{}),"an occupied Helper file prevents updating the active version");
        Require(installation.Current()==one,"file occupancy does not alter the current version");
        var two=installation.Install(second.Zip,second.Release,()=>{});Require(installation.Current()==two && Directory.Exists(installation.VersionPath(one)),"valid N-1 update atomically selects the new version while preserving the prior binaries");
        using(var current=PinnedDirectory.Open(installation.Root))current.AtomicWrite("journal.json",JsonSerializer.SerializeToUtf8Bytes(new InstallJournal(one,two,"prepared"),BundleFormat.Json));
        Require(installation.Current()==two,"interrupted post-switch journal verifies and commits the selected new package");
        var rolled=installation.Rollback(()=>{});Require(rolled==one && installation.Current()==one,"explicit binary rollback works when declared SQLite preferences and credential compatibility match");
        _=installation.Install(future.Zip,future.Release,()=>{});Reject(()=>installation.Rollback(()=>{}),"binary rollback refuses a changed data compatibility contract instead of pretending to downgrade schema");
        Reject(()=>installation.Install(first.Zip,first.Release,()=>{}),"normal installation rejects an older monotonic build");
        var external=Path.Combine(root,"retained-runtime");PrivateRuntime.Ensure(external);File.WriteAllText(Path.Combine(external,"preserve.txt"),"synthetic data");installation.Uninstall(()=>{});
        Require(!Directory.Exists(installation.Root) && File.Exists(Path.Combine(external,"preserve.txt")),"uninstall removes only the owned program directory and preserves the separate runtime data");
        Reject(()=>new UpdateClient(policy with{FeedBaseUri="http://localhost:18731/"}),"production update transport cannot be downgraded to HTTP");
        Reject(()=>new UpdateClient(policy with{FeedBaseUri="https://user:secret@localhost:18731/"}),"credential-bearing update origins are rejected");
        Reject(()=>new UpdateClient(policy with{Development=false,DevelopmentTlsCertificate="invalid"}),"production policy cannot enable the scoped development TLS root");
        var withoutPri=first.Release.Manifest with{Files=first.Release.Manifest.Files.Where(file=>file.Path!="CodexPulse.App.pri").ToArray()};Reject(()=>BundleFormat.Sign(withoutPri,key),"WinUI PRI resources are mandatory before a signed package can be emitted");
        var nonce=Guid.NewGuid().ToString("N");var runtime=Path.Combine(root,"shutdown");PrivateRuntime.Ensure(runtime);
        Require(WindowsCommandLine.UpdateNonce(WindowsCommandLine.Join("--runtime-directory",runtime,"--quit-for-update",nonce))==nonce,"forwarded update activation preserves a nonce after quoted runtime paths");
        UpdateRuntime.Shutdown(runtime,nonce,Environment.ProcessId,false);Reject(()=>UpdateRuntime.RequireClean(runtime,nonce),"uncertain real shutdown receipt cannot authorize a switch");
        UpdateRuntime.Shutdown(runtime,nonce,Environment.ProcessId,true);Reject(()=>UpdateRuntime.RequireClean(runtime,nonce),"clean receipt cannot authorize replacement while its app PID remains alive");
        using(var receipt=Registry.CurrentUser.CreateSubKey(RuntimeProfile.Key(runtime)))receipt.SetValue("UpdateShutdown",JsonSerializer.Serialize(new UpdateShutdownReceipt(nonce,0,"clean",DateTimeOffset.UtcNow.AddMinutes(-3).ToUnixTimeMilliseconds())));
        Reject(()=>UpdateRuntime.RequireClean(runtime,nonce),"stale clean receipt cannot be replayed for a later update");
        UpdateRuntime.Startup(runtime,nonce,"fixture","connected");Require(UpdateRuntime.WaitForStartup(runtime,nonce,"fixture",Environment.ProcessId).GetAwaiter().GetResult().Mode=="connected","installer startup success requires an exact nonce PID version and connected receipt");
        UpdateRuntime.Result(runtime,"update_check_failed","fixture");Require(UpdateRuntime.ReadResult(runtime)?.State=="update_check_failed","fixed failure outcome survives for the next native settings page");
        if(Directory.Exists(Path.Combine(root,"junction"))){Reject(()=>PinnedDirectory.Open(Path.Combine(root,"junction")),"distribution denies junction ancestors");using var parent=PinnedDirectory.Open(root);Reject(()=>parent.Read("junction/preserve.txt"),"distribution read cannot traverse a junction to an external file");Reject(()=>parent.Lock("junction/preserve.txt"),"occupancy gates cannot traverse a junction to an external file");}
    }
    public static async Task Network(string policyPath,string root)
    {
        var policy=JsonSerializer.Deserialize<UpdatePolicy>(File.ReadAllBytes(policyPath),BundleFormat.Json)!;
        using var client=new UpdateClient(policy);var release=await client.CheckAsync("prerelease");
        Require(release.Manifest.Channel=="prerelease","real HTTPS loopback feed validates scoped certificate trust and ECDSA signature");
        var zip=await client.DownloadAsync(release,root);var installation=new Installation(Path.Combine(root,"unused-install"),policy);installation.ValidateDownload(zip,release,Path.Combine(root,"validated"));
        Require(File.Exists(zip),"real update transport downloads and verifies the exact complete native bundle before handoff");
        var noTrust=policy with{DevelopmentTlsCertificate=""};var rejected=false;try{using var untrusted=new UpdateClient(noTrust);await untrusted.CheckAsync("prerelease");}catch(HttpRequestException){rejected=true;}
        Require(rejected,"the operating system trust store does not accept the development certificate without the scoped pin");
    }
    private sealed class ImmediateProgress(Action<double> report):IProgress<double>{public void Report(double value)=>report(value);}
    public static async Task Interrupted(string policyPath,string root)
    {
        var policy=JsonSerializer.Deserialize<UpdatePolicy>(File.ReadAllBytes(policyPath),BundleFormat.Json)!;
        using var client=new UpdateClient(policy);var release=await client.CheckAsync("prerelease");using var cancel=new CancellationTokenSource();var cancelled=false;
        try{await client.DownloadAsync(release,root,new ImmediateProgress(_=>cancel.Cancel()),cancel.Token);}catch(OperationCanceledException){cancelled=true;}
        Require(cancelled && !File.Exists(Path.Combine(root,"release.json")),"interrupted real HTTPS download never emits a handoff release or selects a new version");
        using var directory=PinnedDirectory.Open(root);using var partial=directory.Read(release.Manifest.ZipName);
        Require(partial.Length<release.Manifest.ZipSize,"partial bytes remain confined to a private temporary file and cannot pass signed size validation");
    }
}
