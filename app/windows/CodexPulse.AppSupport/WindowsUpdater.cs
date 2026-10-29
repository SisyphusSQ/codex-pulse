using System.Diagnostics;
using CodexPulse.Distribution;

namespace CodexPulse.AppSupport;

public enum WindowsUpdateKind{Development,Unconfigured,Idle,Checking,Current,Available,Downloading,Preparing,Handoff,Cancelled,Failed}
public sealed class WindowsUpdater:IDisposable
{
    private readonly UpdatePolicy policy=UpdatePolicy.Embedded();
    private readonly string appDirectory,runtime,home;
    private readonly Func<(bool Auto,string Channel,long Seconds)> preferences;
    private readonly Func<string,Task<bool>> quit;
    private readonly Timer timer;
    private CancellationTokenSource? operation;
    private int busy;
    private bool disposed;
    private (bool Auto,string Channel,long Seconds)? schedule;
    public WindowsUpdateKind Kind{get;private set;}
    public string? Error{get;private set;}
    public double Progress{get;private set;}
    public InstalledBundle? Installed{get;private set;}
    public VerifiedRelease? Candidate{get;private set;}
    public UpdateOutcome? LastOutcome{get;private set;}
    public bool DevelopmentTrust=>policy.Development;
    public bool Busy=>Volatile.Read(ref busy)!=0;
    public bool Configured=>policy.Configured && Installed is not null;
    public event Action? Changed;
    public WindowsUpdater(string appDirectory,string runtime,string home,Func<(bool Auto,string Channel,long Seconds)> preferences,Func<string,Task<bool>> quit)
    {
        this.appDirectory=appDirectory;this.runtime=runtime;this.home=home;this.preferences=preferences;this.quit=quit;
        timer=new(_=>{if(!disposed && preferences().Auto)_=CheckAsync();},null,Timeout.Infinite,Timeout.Infinite);
        try{Installed=InstalledBundle.Discover(appDirectory,policy);LastOutcome=UpdateRuntime.ReadResult(runtime);Kind=Installed is null?WindowsUpdateKind.Development:policy.Configured?WindowsUpdateKind.Idle:WindowsUpdateKind.Unconfigured;}
        catch(Exception e)when(e is IOException or UnauthorizedAccessException or System.Text.Json.JsonException){Kind=WindowsUpdateKind.Failed;Error="installation_invalid";}
    }
    public void Configure()
    {if(disposed)return;var value=preferences();if(schedule==value)return;schedule=value;timer.Change(Configured && value.Auto?TimeSpan.FromSeconds(2):Timeout.InfiniteTimeSpan,Configured && value.Auto && value.Seconds is >=60 and <=int.MaxValue?TimeSpan.FromSeconds(value.Seconds):Timeout.InfiniteTimeSpan);}
    public async Task CheckAsync()
    {
        if(disposed || !Configured || Interlocked.CompareExchange(ref busy,1,0)!=0)return;
        operation=new();Kind=WindowsUpdateKind.Checking;Error=null;Notify();
        try
        {
            using var client=new UpdateClient(policy);var value=await client.CheckAsync(preferences().Channel,operation.Token);
            if(disposed)return;Candidate=value.Manifest.BuildNumber>Installed!.Version.BuildNumber?value:null;Kind=Candidate is null?WindowsUpdateKind.Current:WindowsUpdateKind.Available;
        }
        catch(OperationCanceledException){if(!disposed){Kind=operation.IsCancellationRequested?WindowsUpdateKind.Cancelled:WindowsUpdateKind.Failed;Error=operation.IsCancellationRequested?null:"update_timeout";}}
        catch(Exception e)when(e is IOException or HttpRequestException or System.Security.Cryptography.CryptographicException){if(!disposed){Kind=WindowsUpdateKind.Failed;Error="update_check_failed";}}
        finally{operation.Dispose();operation=null;Interlocked.Exchange(ref busy,0);Notify();}
    }
    public async Task InstallAsync()
    {
        if(disposed || Installed is null || Candidate is null || Interlocked.CompareExchange(ref busy,1,0)!=0)return;
        operation=new();Kind=WindowsUpdateKind.Checking;Error=null;Notify();
        try
        {
            using var client=new UpdateClient(policy);var fresh=await client.CheckAsync(preferences().Channel,operation.Token);
            if(fresh.EnvelopeSHA256!=Candidate.EnvelopeSHA256){Candidate=fresh.Manifest.BuildNumber>Installed.Version.BuildNumber?fresh:null;Kind=Candidate is null?WindowsUpdateKind.Current:WindowsUpdateKind.Available;Error="update_changed_confirm_again";return;}
            var work=Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"CodexPulse","updates",Guid.NewGuid().ToString("N"));
            Kind=WindowsUpdateKind.Downloading;Progress=0;Notify();var last=-1;
            var zip=await client.DownloadAsync(fresh,work,new Progress<double>(value=>{var percent=(int)(value*100);if(percent==last)return;last=percent;Progress=value;Notify();}),operation.Token);
            Kind=WindowsUpdateKind.Preparing;Notify();var installation=new Installation(Installed.Root,policy);
            await Task.Run(()=>installation.ValidateDownload(zip,fresh,Path.Combine(work,"validated")),operation.Token);
            operation.Token.ThrowIfCancellationRequested();
            var expected=Installed.Release.Manifest.Files.Single(file=>file.Path=="CodexPulse.PackageHost.exe");
            using(var source=PinnedDirectory.Open(appDirectory))using(var input=source.Read(expected.Path))using(var target=PinnedDirectory.Open(work,requirePrivate:true))
            {
                if(input.Length!=expected.Size || PinnedDirectory.FileHash(input)!=expected.SHA256)throw new IOException("package_host_hash_invalid");input.Position=0;
                using var output=target.Create("worker.exe");input.CopyTo(output);output.Flush(true);
            }
            var nonce=Guid.NewGuid().ToString("N");var start=new ProcessStartInfo(Path.Combine(work,"worker.exe")){UseShellExecute=false,CreateNoWindow=true,WindowStyle=ProcessWindowStyle.Hidden};
            foreach(var argument in new[]{"apply","--quiet","--install-root",Installed.Root,"--runtime-directory",runtime,"--codex-home",home,"--release",Path.Combine(work,"release.json"),"--bundle",zip,"--parent-pid",Environment.ProcessId.ToString(),"--nonce",nonce})start.ArgumentList.Add(argument);
            _=Process.Start(start)??throw new IOException("update_worker_start_failed");Kind=WindowsUpdateKind.Handoff;Notify();
            if(!await quit(nonce)){Kind=WindowsUpdateKind.Failed;Error="update_shutdown_unconfirmed";}
        }
        catch(OperationCanceledException){if(!disposed){Kind=operation?.IsCancellationRequested==true?WindowsUpdateKind.Cancelled:WindowsUpdateKind.Failed;Error=operation?.IsCancellationRequested==true?null:"update_timeout";}}
        catch(Exception e)when(e is IOException or HttpRequestException or UnauthorizedAccessException or System.ComponentModel.Win32Exception or System.Text.Json.JsonException){if(!disposed){Kind=WindowsUpdateKind.Failed;Error="update_install_failed";}}
        finally{operation?.Dispose();operation=null;Interlocked.Exchange(ref busy,0);Notify();}
    }
    private void Notify(){if(!disposed)Changed?.Invoke();}
    public void Cancel()=>operation?.Cancel();
    public void Dispose(){disposed=true;timer.Dispose();operation?.Cancel();Changed=null;}
}
