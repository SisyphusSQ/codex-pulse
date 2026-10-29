using System.Diagnostics;
using System.Runtime.InteropServices;
using System.Text.Json;
using CodexPulse.CoreClient;
using CodexPulse.Distribution;
using Microsoft.Win32;

internal static class Program
{
    private sealed record Profile(string Runtime,string Home,string Shortcut);
    [DllImport("user32.dll",CharSet=CharSet.Unicode)]private static extern int MessageBoxW(IntPtr owner,string message,string title,uint flags);
    private static bool Confirm(string text)=>MessageBoxW(IntPtr.Zero,text,"Codex Pulse",0x134)==6;
    [STAThread]
    private static async Task<int> Main(string[] args)
    {
        var values=new Dictionary<string,string>(StringComparer.Ordinal);var mode=args.FirstOrDefault()??"install";
        string? activeRuntime=null;
        Installation? activeInstallation=null;InstalledVersion? priorVersion=null;string? activeHome=null;var cleanHandoff=false;
        if(string.Equals(Path.GetFileNameWithoutExtension(Environment.ProcessPath),"CodexPulse",StringComparison.OrdinalIgnoreCase)){mode="launch";args=[mode,..args];}
        try
        {
            if(mode is not("install" or "apply" or "launch" or "rollback" or "uninstall"))throw new IOException("installer_action_invalid");
            for(var i=1;i<args.Length;i++)
            {
                if(args[i] is "--quiet" or "--relocated" or "--background" or "--show"){values.Add(args[i],"true");continue;}
                if(args[i] is not("--install-root" or "--runtime-directory" or "--codex-home" or "--release" or "--bundle" or "--parent-pid" or "--nonce" or "--startup-token") || i+1>=args.Length)throw new IOException("installer_argument_invalid");values.Add(args[i],args[++i]);
            }
            var policy=UpdatePolicy.Embedded();if(policy.PublicKeySpki.Length==0)throw new IOException("installer_trust_unconfigured");
            var root=Path.GetFullPath(values.GetValueOrDefault("--install-root")??Installation.DefaultRoot);
            var installation=new Installation(root,policy);activeInstallation=installation;var profile=ReadProfile(root);
            var runtime=Path.GetFullPath(values.GetValueOrDefault("--runtime-directory")??profile?.Runtime??Installation.DefaultRuntime);
            activeRuntime=runtime;
            var home=Path.GetFullPath(values.GetValueOrDefault("--codex-home")??profile?.Home??Installation.DefaultHome);
            activeHome=home;
            if(Inside(runtime,root) || Inside(home,root) || Inside(root,runtime) || Inside(root,home) || root==Path.GetPathRoot(root))throw new IOException("installation_data_boundary_invalid");
            if(mode=="launch")
            {Launch(installation.LaunchPath(),runtime,home,values.ContainsKey("--background"),values.GetValueOrDefault("--startup-token"));return 0;}
            if(!values.ContainsKey("--quiet") && !Confirm((mode=="uninstall"?"卸载程序并保留 runtime、账号资料与 Codex Home。\nUninstall programs and retain runtime, account details and Codex Home.":"安装/更新同包 App 与 Helper。程序数据分离，只有干净退出后才切换版本。\nInstall/update the paired App and Helper after clean shutdown.")+"\n\n"+root))return 2;
            if(!values.ContainsKey("--relocated") && Inside(Environment.ProcessPath!,root))
            {
                var current=installation.Current()??throw new IOException("installation_missing");var release=installation.VerifyInstalled(current);var host=release.Manifest.Files.Single(file=>file.Path=="CodexPulse.PackageHost.exe");
                using(var source=PinnedDirectory.Open(Path.GetDirectoryName(Environment.ProcessPath!)!))using(var file=source.Read(Path.GetFileName(Environment.ProcessPath!)))if(PinnedDirectory.FileHash(file)!=host.SHA256)throw new IOException("installer_host_hash_invalid");
                var work=Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"CodexPulse","updates",Guid.NewGuid().ToString("N"));
                using(var directory=PinnedDirectory.Open(work,true,true))using(var source=PinnedDirectory.Open(Path.GetDirectoryName(Environment.ProcessPath!)!))using(var input=source.Read(Path.GetFileName(Environment.ProcessPath!)))using(var output=directory.Create("worker.exe")){input.CopyTo(output);output.Flush(true);}
                var start=new ProcessStartInfo(Path.Combine(work,"worker.exe")){UseShellExecute=false,CreateNoWindow=true,WindowStyle=ProcessWindowStyle.Hidden};
                values["--relocated"]="true";values["--quiet"]="true";values["--parent-pid"]=Environment.ProcessId.ToString();values["--install-root"]=root;
                start.ArgumentList.Add(mode);foreach(var value in values){start.ArgumentList.Add(value.Key);if(value.Value!="true")start.ArgumentList.Add(value.Value);}
                _=Process.Start(start)??throw new IOException("installer_worker_start_failed");return 0;
            }
            if(values.TryGetValue("--parent-pid",out var parentText))
            {if(!int.TryParse(parentText,out var parent)||parent<=0)throw new IOException("installer_parent_invalid");await UpdateRuntime.WaitForExit(parent);}
            var previous=installation.Current();priorVersion=previous;var nonce=values.GetValueOrDefault("--nonce")??Guid.NewGuid().ToString("N");
            if(previous is not null)
            {
                if(mode=="apply"){UpdateRuntime.RequireClean(runtime,nonce,values.TryGetValue("--parent-pid",out var p)?int.Parse(p):null);cleanHandoff=true;}
                else await UpdateRuntime.RequestExit(Path.Combine(installation.VersionPath(previous),"CodexPulse.App.exe"),runtime,home,nonce);
            }
            void Gate(){if(previous is not null)UpdateRuntime.RequireClean(runtime,nonce,mode=="apply" && values.TryGetValue("--parent-pid",out var p)?int.Parse(p):null);}
            if(mode=="uninstall")
            {
                installation.Uninstall(Gate);Unregister(root,profile);UpdateRuntime.Result(runtime,"uninstalled",previous?.Version??"");return 0;
            }
            InstalledVersion next;
            if(mode=="rollback")next=installation.Rollback(Gate);
            else
            {
                var releasePath=Path.GetFullPath(values.GetValueOrDefault("--release")??Path.Combine(AppContext.BaseDirectory,"release.json"));
                using var directory=PinnedDirectory.Open(Path.GetDirectoryName(releasePath)!);
                var release=BundleFormat.Verify(directory.ReadBytes(Path.GetFileName(releasePath),BundleFormat.MaximumEnvelopeBytes),policy);
                var zip=Path.GetFullPath(values.GetValueOrDefault("--bundle")??Path.Combine(Path.GetDirectoryName(releasePath)!,release.Manifest.ZipName));
                next=installation.Install(zip,release,Gate);
            }
            var shortcut=profile?.Shortcut??"Codex Pulse"+(root.Equals(Installation.DefaultRoot,StringComparison.OrdinalIgnoreCase)?"":" ("+BundleFormat.Hash(System.Text.Encoding.UTF8.GetBytes(root))[..8]+")")+".lnk";
            InstallLaunchers(installation,next);Register(root,next,new(runtime,home,shortcut));UpdateRuntime.Result(runtime,"installed",next.Version);
            var startupNonce=Guid.NewGuid().ToString("N");var started=Launch(installation.LaunchPath(),runtime,home,false,startupNonce);
            var ready=await UpdateRuntime.WaitForStartup(runtime,startupNonce,next.Version,started);UpdateRuntime.Result(runtime,ready.Mode=="recovery"?"recovery_required":"ready",next.Version);return 0;
        }
        catch(Exception error)when(error is IOException or UnauthorizedAccessException or System.Security.SecurityException or JsonException or ArgumentException or System.ComponentModel.Win32Exception or InvalidDataException)
        {
            // Only fixed local failure codes enter UI/registry. Never echo raw paths,
            // downloaded JSON, exceptions or response bodies.
            var code=error is IOException io && io.Message.StartsWith("distribution_",StringComparison.Ordinal) || error.Message.StartsWith("package_",StringComparison.Ordinal) || error.Message.StartsWith("update_",StringComparison.Ordinal) || error.Message.StartsWith("installation_",StringComparison.Ordinal) || error.Message.StartsWith("rollback_",StringComparison.Ordinal)?error.Message:"installation_failed";
            if(activeRuntime is not null){try{UpdateRuntime.Result(activeRuntime,code,"");}catch(Exception local)when(local is IOException or UnauthorizedAccessException or System.Security.SecurityException){}}
            // A failed download/stage must not leave the user without the previous
            // working app. Restart only after its exact clean handoff, and only if
            // the authoritative pointer never changed. Schema changes are never
            // undone implicitly after a new version has started.
            if(mode=="apply" && cleanHandoff && activeInstallation is not null && priorVersion is not null && activeRuntime is not null && activeHome is not null)
            {
                try
                {
                    if(activeInstallation.Current()==priorVersion)
                    {
                        var recoveryNonce=Guid.NewGuid().ToString("N");var restarted=Launch(activeInstallation.LaunchPath(),activeRuntime,activeHome,false,recoveryNonce);
                        await UpdateRuntime.WaitForStartup(activeRuntime,recoveryNonce,priorVersion.Version,restarted);
                        UpdateRuntime.Result(activeRuntime,code,priorVersion.Version);
                    }
                }
                catch(Exception recovery)when(recovery is IOException or UnauthorizedAccessException or System.Security.SecurityException or JsonException or System.ComponentModel.Win32Exception){/* Preserve the original fixed failure outcome for the next normal launch. */}
            }
            if(!values.ContainsKey("--quiet"))MessageBoxW(IntPtr.Zero,"操作未完成 / Operation incomplete\n"+code,"Codex Pulse",0x10);return 1;
        }
    }
    private static bool Inside(string path,string root){path=Path.TrimEndingDirectorySeparator(Path.GetFullPath(path));root=Path.TrimEndingDirectorySeparator(Path.GetFullPath(root));return path.Equals(root,StringComparison.OrdinalIgnoreCase) || path.StartsWith(root+Path.DirectorySeparatorChar,StringComparison.OrdinalIgnoreCase);}
    private static Profile? ReadProfile(string root)
    {if(!File.Exists(Path.Combine(root,"install-profile.json")))return null;using var directory=PinnedDirectory.Open(root,requirePrivate:true);return JsonSerializer.Deserialize<Profile>(directory.ReadBytes("install-profile.json",16384),BundleFormat.Json);}
    private static void InstallLaunchers(Installation installation,InstalledVersion version)
    {
        using var source=PinnedDirectory.Open(installation.VersionPath(version));var bytes=source.ReadBytes("CodexPulse.PackageHost.exe",(int)BundleFormat.MaximumFileBytes);
        using var target=PinnedDirectory.Open(installation.Root,requirePrivate:true);target.AtomicWrite("Setup.exe",bytes);target.AtomicWrite("CodexPulse.exe",bytes);
    }
    private static string UninstallKey(string root)=>@"Software\Microsoft\Windows\CurrentVersion\Uninstall\CodexPulse.Windows."+BundleFormat.Hash(System.Text.Encoding.UTF8.GetBytes(root.ToUpperInvariant()))[..16];
    private static void Register(string root,InstalledVersion version,Profile profile)
    {
        using(var directory=PinnedDirectory.Open(root))directory.AtomicWrite("install-profile.json",JsonSerializer.SerializeToUtf8Bytes(profile,BundleFormat.Json));
        using(var key=Registry.CurrentUser.CreateSubKey(UninstallKey(root)))
        {
            key.SetValue("DisplayName","Codex Pulse");key.SetValue("DisplayVersion",version.Version);key.SetValue("Publisher","Codex Pulse");key.SetValue("InstallLocation",root);
            key.SetValue("UninstallString",WindowsCommandLine.Join(Path.Combine(root,"Setup.exe"),"uninstall","--install-root",root));key.SetValue("NoModify",1);key.SetValue("NoRepair",1);
        }
        if(Path.GetFileName(profile.Shortcut)!=profile.Shortcut || !profile.Shortcut.EndsWith(".lnk",StringComparison.OrdinalIgnoreCase))throw new IOException("shortcut_name_invalid");
        var path=Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.Programs),profile.Shortcut);
        var type=Type.GetTypeFromProgID("WScript.Shell")??throw new IOException("shortcut_service_unavailable");var shell=Activator.CreateInstance(type)!;
        try{using var parent=PinnedDirectory.Open(Path.GetDirectoryName(path)!);var exists=File.Exists(path);if(exists)using(parent.Read(Path.GetFileName(path))){}dynamic shortcut=((dynamic)shell).CreateShortcut(path);try{if(exists && !string.Equals((string)shortcut.TargetPath,Path.Combine(root,"CodexPulse.exe"),StringComparison.OrdinalIgnoreCase))throw new IOException("shortcut_owned_by_another_application");shortcut.TargetPath=Path.Combine(root,"CodexPulse.exe");shortcut.WorkingDirectory=root;shortcut.Description="Codex Pulse";shortcut.Save();}finally{Marshal.FinalReleaseComObject(shortcut);}}
        finally{Marshal.FinalReleaseComObject(shell);}
    }
    private static void Unregister(string root,Profile? profile)
    {
        // Remove only this install's startup command; preserve other profiles and
        // never modify Windows' StartupApproved policy.
        using(var run=Registry.CurrentUser.OpenSubKey(@"Software\Microsoft\Windows\CurrentVersion\Run",true))
        {
            if(run is not null)foreach(var name in run.GetValueNames())
            {
                if(!name.StartsWith("CodexPulse-",StringComparison.Ordinal) || run.GetValue(name) is not string command)continue;
                var arguments=WindowsCommandLine.Split(command);
                if(arguments.Length>0 && string.Equals(arguments[0],Path.Combine(root,"CodexPulse.exe"),StringComparison.OrdinalIgnoreCase))run.DeleteValue(name,false);
            }
        }
        using(var key=Registry.CurrentUser.OpenSubKey(UninstallKey(root)))if(key?.GetValue("InstallLocation") is string registered && !registered.Equals(root,StringComparison.OrdinalIgnoreCase))throw new IOException("uninstall_registration_changed");
        Registry.CurrentUser.DeleteSubKeyTree(UninstallKey(root),false);
        if(profile is not null && Path.GetFileName(profile.Shortcut)==profile.Shortcut && profile.Shortcut.EndsWith(".lnk",StringComparison.OrdinalIgnoreCase))
        {
            var path=Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.Programs),profile.Shortcut);
            if(File.Exists(path))
            {
                using var parent=PinnedDirectory.Open(Path.GetDirectoryName(path)!);using(parent.Read(Path.GetFileName(path))){}
                var type=Type.GetTypeFromProgID("WScript.Shell")??throw new IOException("shortcut_service_unavailable");var shell=Activator.CreateInstance(type)!;
                try{dynamic shortcut=((dynamic)shell).CreateShortcut(path);try{if(string.Equals((string)shortcut.TargetPath,Path.Combine(root,"CodexPulse.exe"),StringComparison.OrdinalIgnoreCase))File.Delete(path);}finally{Marshal.FinalReleaseComObject(shortcut);}}finally{Marshal.FinalReleaseComObject(shell);}
            }
        }
    }
    private static int Launch(string app,string runtime,string home,bool background,string? nonce)
    {
        var start=new ProcessStartInfo(app){UseShellExecute=false};foreach(var argument in new[]{"--runtime-directory",runtime,"--codex-home",home,background?"--background":"--show"})start.ArgumentList.Add(argument);
        if(nonce is not null){if(!Guid.TryParseExact(nonce,"N",out _))throw new IOException("startup_nonce_invalid");start.ArgumentList.Add("--startup-token");start.ArgumentList.Add(nonce);}
        using var process=Process.Start(start)??throw new IOException("installed_app_start_failed");return process.Id;
    }
}
