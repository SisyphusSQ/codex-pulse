using System.Diagnostics;
using System.Text.Json;
using Microsoft.Win32;
using CodexPulse.CoreClient;

namespace CodexPulse.Distribution;

public sealed record UpdateShutdownReceipt(string Nonce,int AppPid,string State,long WrittenAtMs);
public sealed record UpdateStartupReceipt(string Nonce,int AppPid,string Version,string Mode,long WrittenAtMs);
public sealed record UpdateOutcome(string State,string Version,long AtMs);
public static class UpdateRuntime
{
    public static void Shutdown(string runtime,string nonce,int appPid,bool clean)
    {if(!Guid.TryParseExact(nonce,"N",out _))throw new ArgumentException("update_nonce_invalid");using var key=Registry.CurrentUser.CreateSubKey(RuntimeProfile.Key(runtime));key.SetValue("UpdateShutdown",JsonSerializer.Serialize(new UpdateShutdownReceipt(nonce,appPid,clean?"clean":"uncertain",DateTimeOffset.UtcNow.ToUnixTimeMilliseconds())));}
    public static void IdleShutdown(string runtime,string nonce)
    {using var key=Registry.CurrentUser.OpenSubKey(RuntimeProfile.Key(runtime));var state=key?.GetValue("Shutdown") as string;Shutdown(runtime,nonce,Environment.ProcessId,state is null or "clean");}
    public static void Startup(string runtime,string nonce,string version,string mode)
    {if(!Guid.TryParseExact(nonce,"N",out _))throw new ArgumentException("update_nonce_invalid");using var key=Registry.CurrentUser.CreateSubKey(RuntimeProfile.Key(runtime));key.SetValue("UpdateStartup",JsonSerializer.Serialize(new UpdateStartupReceipt(nonce,Environment.ProcessId,version,mode,DateTimeOffset.UtcNow.ToUnixTimeMilliseconds())));}
    public static UpdateShutdownReceipt? ReadShutdown(string runtime)
    {using var key=Registry.CurrentUser.OpenSubKey(RuntimeProfile.Key(runtime));return key?.GetValue("UpdateShutdown") is string value && value.Length<=8192?JsonSerializer.Deserialize<UpdateShutdownReceipt>(value):null;}
    public static UpdateStartupReceipt? ReadStartup(string runtime)
    {using var key=Registry.CurrentUser.OpenSubKey(RuntimeProfile.Key(runtime));return key?.GetValue("UpdateStartup") is string value && value.Length<=8192?JsonSerializer.Deserialize<UpdateStartupReceipt>(value):null;}
    public static UpdateOutcome? ReadResult(string runtime)
    {using var key=Registry.CurrentUser.OpenSubKey(RuntimeProfile.Key(runtime));return key?.GetValue("UpdateResult") is string value && value.Length<=8192?JsonSerializer.Deserialize<UpdateOutcome>(value):null;}
    public static async Task<UpdateStartupReceipt> WaitForStartup(string runtime,string nonce,string version,int pid,CancellationToken cancellation=default)
    {
        var deadline=DateTime.UtcNow.AddSeconds(45);
        while(DateTime.UtcNow<deadline)
        {
            cancellation.ThrowIfCancellationRequested();var value=ReadStartup(runtime);
            if(value is not null && value.Nonce==nonce && value.Version==version && value.AppPid==pid)
            {if(value.Mode is "connected" or "recovery")return value;throw new IOException("update_startup_unavailable");}
            if(!Alive(pid))throw new IOException("update_app_exited_before_startup");await Task.Delay(150,cancellation);
        }
        throw new IOException("update_startup_timeout");
    }
    public static void RequireClean(string runtime,string nonce,int? expectedPid=null)
    {
        var value=ReadShutdown(runtime);var now=DateTimeOffset.UtcNow.ToUnixTimeMilliseconds();
        if(value is null || value.Nonce!=nonce || (value.State!="clean" && value.State!="not_started") || expectedPid is {} pid && value.AppPid!=pid || value.WrittenAtMs>now || now-value.WrittenAtMs>120000)throw new IOException("update_shutdown_unconfirmed");
        if(value.State=="not_started" && (value.AppPid!=0 || Directory.Exists(runtime)))throw new IOException("update_shutdown_unconfirmed");
        if(Alive(value.AppPid))throw new IOException("update_parent_still_running");
    }
    public static async Task RequestExit(string app,string runtime,string home,string nonce,CancellationToken cancellation=default)
    {
        // Initial publish failures can occur before the Helper/runtime ever exists.
        // This is a distinct state, never a fabricated clean Helper acknowledgment.
        if(!Directory.Exists(runtime) && !RunningApp(app))
        {using var key=Registry.CurrentUser.CreateSubKey(RuntimeProfile.Key(runtime));key.SetValue("UpdateShutdown",JsonSerializer.Serialize(new UpdateShutdownReceipt(nonce,0,"not_started",DateTimeOffset.UtcNow.ToUnixTimeMilliseconds())));return;}
        var start=new ProcessStartInfo(app){UseShellExecute=false,CreateNoWindow=true,WindowStyle=ProcessWindowStyle.Hidden};foreach(var argument in new[]{"--runtime-directory",runtime,"--codex-home",home,"--quit-for-update",nonce})start.ArgumentList.Add(argument);
        using var request=Process.Start(start)??throw new IOException("update_exit_request_failed");
        var deadline=DateTime.UtcNow.AddSeconds(45);
        while(DateTime.UtcNow<deadline)
        {cancellation.ThrowIfCancellationRequested();var receipt=ReadShutdown(runtime);if(receipt?.Nonce==nonce && !Alive(receipt.AppPid)){RequireClean(runtime,nonce);return;}await Task.Delay(150,cancellation);}
        throw new IOException("update_shutdown_timeout");
    }
    public static async Task WaitForExit(int pid,CancellationToken cancellation=default)
    {try{using var process=Process.GetProcessById(pid);await process.WaitForExitAsync(cancellation).WaitAsync(TimeSpan.FromSeconds(45),cancellation);}catch(ArgumentException){}catch(TimeoutException){throw new IOException("update_parent_timeout");}}
    public static bool Alive(int pid){if(pid<=0)return false;try{using var process=Process.GetProcessById(pid);return !process.HasExited;}catch(ArgumentException){return false;}}
    private static bool RunningApp(string app)
    {foreach(var process in Process.GetProcessesByName(Path.GetFileNameWithoutExtension(app))){using(process){if(process.MainModule?.FileName?.Equals(app,StringComparison.OrdinalIgnoreCase)==true)return true;}}return false;}
    public static void Result(string runtime,string state,string version)
    {using var key=Registry.CurrentUser.CreateSubKey(RuntimeProfile.Key(runtime));key.SetValue("UpdateResult",JsonSerializer.Serialize(new UpdateOutcome(state,version,DateTimeOffset.UtcNow.ToUnixTimeMilliseconds())));}
}
