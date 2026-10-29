using System.IO.Compression;
using System.Reflection.PortableExecutable;
using System.Security.Cryptography;
using System.Security.Principal;
using System.Text.Json;

namespace CodexPulse.Distribution;

public sealed record InstalledVersion(long BuildNumber,string Version,string EnvelopeSHA256);
public sealed record InstallOwner(string PackageId,string PublicKeyHash,string OwnerSID,string Directory);
public sealed record InstallJournal(InstalledVersion? Previous,InstalledVersion Next,string Phase);
public sealed record BundleInfo(string Version,long BuildNumber,string Architecture,string CoreContract,string DataCompatibility,string WindowsTrust);

public sealed class Installation(string root,UpdatePolicy policy)
{
    public string Root{get;}=Path.TrimEndingDirectorySeparator(Path.GetFullPath(root));
    public static string DefaultRoot=>Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"Programs","CodexPulse");
    public static string DefaultRuntime=>Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"CodexPulse","runtime");
    public static string DefaultHome=>Environment.GetEnvironmentVariable("CODEX_HOME") is {Length:>0} path?Path.GetFullPath(path):Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile),".codex");
    public InstalledVersion? Current()
    {
        using var lease=new InstallLease(Root);
        if(!Directory.Exists(Root))return null;
        using var directory=OpenOwned();using var gate=directory.Lock(".distribution.lock",true);Recover(directory);return ReadState(directory,"current.json");
    }
    public VerifiedRelease VerifyInstalled(InstalledVersion version,bool allFiles=true)
    {
        using var directory=PinnedDirectory.Open(VersionPath(version),requirePrivate:true);
        var verified=BundleFormat.Verify(directory.ReadBytes("release.json",BundleFormat.MaximumEnvelopeBytes),policy);
        if(verified.Manifest.BuildNumber!=version.BuildNumber || verified.Manifest.Version!=version.Version || verified.EnvelopeSHA256!=version.EnvelopeSHA256)throw new IOException("installed_version_mismatch");
        if(allFiles)VerifyFiles(directory,verified.Manifest);
        return verified;
    }
    public string VersionPath(InstalledVersion version)=>Path.Combine(Root,"releases",BundleFormat.VersionDirectory(version.BuildNumber));
    public void ValidateDownload(string zipPath,VerifiedRelease release,string validationDirectory)
    {
        release=BundleFormat.Verify(release.Envelope,policy);using var source=PinnedDirectory.Open(Path.GetDirectoryName(zipPath)!);using var file=source.Read(Path.GetFileName(zipPath));
        if(file.Length!=release.Manifest.ZipSize || !PinnedDirectory.FileHash(file).Equals(release.Manifest.ZipSHA256,StringComparison.OrdinalIgnoreCase))throw new IOException("package_hash_invalid");file.Position=0;
        using var target=PinnedDirectory.Open(validationDirectory,true,true);Extract(file,target,release.Manifest);VerifyFiles(target,release.Manifest);
    }
    public InstalledVersion Install(string zipPath,VerifiedRelease release,Action shutdownGate)
    {
        using var lease=new InstallLease(Root);
        var manifest=release.Manifest;BundleFormat.Validate(manifest);
        if(Environment.OSVersion.Version.Build<manifest.MinimumWindowsBuild || !Environment.Is64BitProcess)throw new IOException("package_platform_unsupported");
        // Re-verify raw bytes against the installed trust root at every boundary.
        release=BundleFormat.Verify(release.Envelope,policy);manifest=release.Manifest;
        using var directory=OpenOwned(true);using var gate=directory.Lock(".distribution.lock",true);Recover(directory);
        var previous=ReadState(directory,"current.json");
        if(previous is not null && manifest.BuildNumber<=previous.BuildNumber)
        {
            if(manifest.BuildNumber==previous.BuildNumber && previous.EnvelopeSHA256==release.EnvelopeSHA256){VerifyInstalled(previous);return previous;}
            throw new IOException("package_downgrade_rejected");
        }
        var stageName=".stage-"+Guid.NewGuid().ToString("N");var stage=Path.Combine(Root,stageName);
        try
        {
        using(var archiveParent=PinnedDirectory.Open(Path.GetDirectoryName(zipPath)!))
        using(var archive=archiveParent.Read(Path.GetFileName(zipPath)))
        {
            if(archive.Length!=manifest.ZipSize || !PinnedDirectory.FileHash(archive).Equals(manifest.ZipSHA256,StringComparison.OrdinalIgnoreCase))throw new IOException("package_hash_invalid");archive.Position=0;
            using var output=PinnedDirectory.Open(stage,true,true);
            Extract(archive,output,manifest);output.AtomicWrite("release.json",release.Envelope);VerifyFiles(output,manifest);
        }
            // No pointer, existing program file, runtime or data mutation precedes this gate.
            shutdownGate();if(previous is not null)AssertFilesUnused(previous);
            var next=new InstalledVersion(manifest.BuildNumber,manifest.Version,release.EnvelopeSHA256);
            var releases=Path.Combine(Root,"releases");using(var parent=PinnedDirectory.Open(releases,true,true))
            {
                var destination=VersionPath(next);
                if(Directory.Exists(destination))
                {VerifyInstalled(next);PinnedDirectory.DeleteTree(stage,Root);}
                else Directory.Move(stage,destination);
            }
            Write(directory,"journal.json",new InstallJournal(previous,next,"prepared"));
            if(previous is not null)Write(directory,"previous.json",previous);
            Write(directory,"current.json",next);Write(directory,"journal.json",new InstallJournal(previous,next,"committed"));
            return next;
        }
        finally{if(Directory.Exists(stage))PinnedDirectory.DeleteTree(stage,Root);}
    }
    public InstalledVersion Rollback(Action shutdownGate)
    {
        using var lease=new InstallLease(Root);
        using var directory=OpenOwned();using var gate=directory.Lock(".distribution.lock",true);Recover(directory);
        var current=ReadState(directory,"current.json")??throw new IOException("installation_missing");var previous=ReadState(directory,"previous.json")??throw new IOException("rollback_missing");
        var currentRelease=VerifyInstalled(current);var oldRelease=VerifyInstalled(previous);
        if(currentRelease.Manifest.DataCompatibility!=oldRelease.Manifest.DataCompatibility)throw new IOException("rollback_requires_data_recovery");
        shutdownGate();AssertFilesUnused(current);AssertFilesUnused(previous);
        Write(directory,"journal.json",new InstallJournal(current,previous,"prepared"));Write(directory,"previous.json",current);Write(directory,"current.json",previous);Write(directory,"journal.json",new InstallJournal(current,previous,"committed"));return previous;
    }
    public string LaunchPath()
    {
        using var lease=new InstallLease(Root);
        using var directory=OpenOwned();using var gate=directory.Lock(".distribution.lock",true);Recover(directory);
        var current=ReadState(directory,"current.json")??throw new IOException("installation_missing");VerifyInstalled(current);
        return Path.Combine(VersionPath(current),"CodexPulse.App.exe");
    }
    public void Uninstall(Action shutdownGate)
    {
        using var lease=new InstallLease(Root);
        using(var directory=OpenOwned())
        using(var gate=directory.Lock(".distribution.lock",true))
        {
            Recover(directory);shutdownGate();
            if(Directory.Exists(Path.Combine(Root,"releases")))
            {using var releases=PinnedDirectory.Open(Path.Combine(Root,"releases"));foreach(var path in Directory.GetDirectories(releases.Path))foreach(var file in EnumerateFiles(path))using(var release=PinnedDirectory.Open(path))using(release.Lock(file)){} }
        }
        PinnedDirectory.DeleteTree(Root,Root);
    }
    private sealed class InstallLease:IDisposable
    {
        private readonly Mutex mutex;
        public InstallLease(string root)
        {
            var name="Global\\CodexPulse.Install."+BundleFormat.Hash(System.Text.Encoding.UTF8.GetBytes((WindowsIdentity.GetCurrent().User?.Value??"")+"|"+root.ToUpperInvariant()));mutex=new(false,name);
            try{if(!mutex.WaitOne(TimeSpan.FromSeconds(30))){mutex.Dispose();throw new IOException("installation_busy");}}
            catch(AbandonedMutexException){/* The caller must recover and verify the durable journal. */}
        }
        public void Dispose(){mutex.ReleaseMutex();mutex.Dispose();}
    }
    public void AssertFilesUnused(InstalledVersion version)
    {
        using var directory=PinnedDirectory.Open(VersionPath(version));
        var release=VerifyInstalled(version,false);
        foreach(var file in release.Manifest.Files)using(directory.Lock(file.Path)){}
    }
    private PinnedDirectory OpenOwned(bool initialize=false)
    {
        var directory=PinnedDirectory.Open(Root,initialize,true);
        try
        {
            var user=WindowsIdentity.GetCurrent().User?.Value??throw new IOException("installation_owner_unavailable");var expected=new InstallOwner(BundleFormat.PackageId,BundleFormat.Hash(Convert.FromBase64String(policy.PublicKeySpki)),user,Root);
            if(!File.Exists(Path.Combine(Root,"installation.json")))
            {
                if(!initialize || Directory.EnumerateFileSystemEntries(Root).Any())throw new IOException("installation_owner_missing");Write(directory,"installation.json",expected);
            }
            var owner=JsonSerializer.Deserialize<InstallOwner>(directory.ReadBytes("installation.json",8192),BundleFormat.Json);
            if(owner!=expected)throw new IOException("installation_owner_invalid");return directory;
        }
        catch{directory.Dispose();throw;}
    }
    private static void Write<T>(PinnedDirectory directory,string name,T value)=>directory.AtomicWrite(name,JsonSerializer.SerializeToUtf8Bytes(value,BundleFormat.Json));
    private static InstalledVersion? ReadState(PinnedDirectory directory,string name)
    {
        if(!File.Exists(Path.Combine(directory.Path,name)))return null;
        var value=JsonSerializer.Deserialize<InstalledVersion>(directory.ReadBytes(name,8192),BundleFormat.Json)??throw new IOException("installation_state_invalid");
        if(value.BuildNumber<=0 || !BundleFormat.HashValid(value.EnvelopeSHA256) || string.IsNullOrEmpty(value.Version))throw new IOException("installation_state_invalid");return value;
    }
    private void Recover(PinnedDirectory directory)
    {
        if(!File.Exists(Path.Combine(Root,"journal.json")))return;
        var journal=JsonSerializer.Deserialize<InstallJournal>(directory.ReadBytes("journal.json",16384),BundleFormat.Json)??throw new IOException("installation_journal_invalid");
        if(journal.Phase is "committed" or "abandoned")return;
        if(journal.Phase!="prepared")throw new IOException("installation_journal_invalid");
        var current=ReadState(directory,"current.json");
        if(current==journal.Next){VerifyInstalled(journal.Next);Write(directory,"journal.json",journal with{Phase="committed"});return;}
        if(current!=journal.Previous)throw new IOException("installation_recovery_required");
        if(current is not null)VerifyInstalled(current);
        // Prepared but no pointer switch: keep the previous installation authoritative.
        Write(directory,"journal.json",journal with{Phase="abandoned"});
    }
    private static void Extract(FileStream source,PinnedDirectory target,ReleaseManifest manifest)
    {
        using var archive=new ZipArchive(source,ZipArchiveMode.Read,true);if(archive.Entries.Count!=manifest.Files.Count)throw new IOException("package_entries_mismatch");
        var files=manifest.Files.ToDictionary(file=>file.Path,StringComparer.OrdinalIgnoreCase);var names=new HashSet<string>(StringComparer.OrdinalIgnoreCase);long expanded=0;
        foreach(var entry in archive.Entries)
        {
            BundleFormat.ValidatePath(entry.FullName);
            if(!names.Add(entry.FullName) || !files.TryGetValue(entry.FullName,out var expected) || expected.Path!=entry.FullName || entry.Length!=expected.Size || (entry.ExternalAttributes>>16&0xf000)==0xa000)throw new IOException("package_entry_invalid");
            using var input=entry.Open();using var output=target.Create(entry.FullName);using var hash=IncrementalHash.CreateHash(HashAlgorithmName.SHA256);var buffer=new byte[64*1024];long size=0;
            while(true){var read=input.Read(buffer);if(read==0)break;size+=read;expanded+=read;if(size>expected.Size || expanded>BundleFormat.MaximumExpandedBytes)throw new IOException("package_expanded_limit");output.Write(buffer,0,read);hash.AppendData(buffer,0,read);}
            if(size!=expected.Size || !Convert.ToHexStringLower(hash.GetHashAndReset()).Equals(expected.SHA256,StringComparison.OrdinalIgnoreCase))throw new IOException("package_entry_hash");output.Flush(true);
        }
    }
    private static void VerifyFiles(PinnedDirectory directory,ReleaseManifest manifest)
    {
        var allowed=manifest.Files.Select(file=>file.Path).Append("release.json").ToHashSet(StringComparer.OrdinalIgnoreCase);
        foreach(var actual in EnumerateFiles(directory.Path))if(!allowed.Contains(actual))throw new IOException("installed_unsigned_file");
        foreach(var expected in manifest.Files)
        {using var file=directory.Read(expected.Path);if(file.Length!=expected.Size || !PinnedDirectory.FileHash(file).Equals(expected.SHA256,StringComparison.OrdinalIgnoreCase))throw new IOException("installed_file_hash_invalid");}
        var info=JsonSerializer.Deserialize<BundleInfo>(directory.ReadBytes("bundle-info.json",8192),BundleFormat.Json);
        if(info is null || info.Version!=manifest.Version || info.BuildNumber!=manifest.BuildNumber || info.Architecture!=manifest.Architecture || info.CoreContract!=manifest.CoreContract || info.DataCompatibility!=manifest.DataCompatibility)throw new IOException("bundle_pairing_mismatch");
        foreach(var name in new[]{"CodexPulse.App.exe","codex-pulse.exe","CodexPulse.PackageHost.exe"})
        {using var file=directory.Read(name);using var pe=new PEReader(file);if(pe.PEHeaders.CoffHeader.Machine!=Machine.Amd64)throw new IOException("package_architecture_mismatch");}
    }
    private static IEnumerable<string> EnumerateFiles(string root,string relative="")
    {
        using var directory=PinnedDirectory.Open(relative.Length==0?root:Path.Combine(root,relative));
        foreach(var item in new DirectoryInfo(directory.Path).EnumerateFileSystemInfos())
        {
            if((item.Attributes&FileAttributes.ReparsePoint)!=0)throw new IOException("distribution_unsafe_file");
            var name=relative.Length==0?item.Name:relative+"/"+item.Name;BundleFormat.ValidatePath(name);
            if(item is DirectoryInfo){foreach(var child in EnumerateFiles(root,name))yield return child;}
            else yield return name;
        }
    }
}
