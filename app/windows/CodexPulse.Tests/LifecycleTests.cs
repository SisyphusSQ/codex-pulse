using System.Diagnostics;
using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using CodexPulse.CoreClient;

internal static class LifecycleTests
{
    private static void Require(bool value,string name){if(!value)throw new InvalidOperationException(name);Console.WriteLine("PASS "+name);}
    private sealed class StartupStore:IStartupStore
    {
        public string? Value;public byte[]? Status;public int Writes;
        public string? Command(string name)=>Value;
        public byte[]? Approval(string name)=>Status;
        public void SetCommand(string name,string? value){Writes++;Value=value;}
    }
    public static async Task Unit()
    {
        var budget=new RestartBudget();var now=DateTimeOffset.UtcNow;
        Require(budget.Claim(now)==TimeSpan.FromSeconds(1) && budget.Claim(now)==TimeSpan.FromSeconds(2) && budget.Claim(now)==TimeSpan.FromSeconds(4) && budget.Claim(now) is null,"Helper recovery stops after three attempts in ten minutes, even after successful handshakes");
        Require(budget.Claim(now.AddMinutes(10))==TimeSpan.FromSeconds(1),"Helper restart budget expires after the bounded observation window");
        var release=new TaskCompletionSource(TaskCreationOptions.RunContinuationsAsynchronously);var events=new List<string>();
        var mailbox=new LifecycleMailbox(async value=>{events.Add(value);if(events.Count==1)await release.Task;});
        mailbox.Post("application_did_become_active");for(var i=0;i<100;i++)mailbox.Post("application_did_become_active");mailbox.Post("system_will_sleep");mailbox.Post("system_did_wake");
        release.SetResult();await mailbox.Completion;
        Require(events.SequenceEqual(new[]{"application_did_become_active","system_did_wake"}),"slow lifecycle delivery coalesces activation storms and converges the latest power state");
        mailbox.Stop();mailbox.Post("application_did_become_active");Require(events.Count==2,"shutdown drops pending lifecycle notifications instead of replaying them");
        var store=new StartupStore();var startup=new StartupRegistration(store,"test",@"C:\Program Files\Pulse\Pulse.exe",@"C:\local\中文 path",@"C:\users\test\.codex");
        Require(startup.Read()==StartupState.Off && startup.Set(true)==StartupState.Configured && startup.ExpectedCommand.Contains("\"--background\"") && startup.ExpectedCommand.Contains("\"C:\\local\\中文 path\""),"login registration quotes paths and reads the exact system command through an injectable adapter");
        store.Status=new byte[12];store.Status[0]=3;Require(startup.Read()==StartupState.DisabledBySystem,"a Windows-disabled startup entry is not reported as enabled");
        store.Status[0]=99;Require(startup.Read()==StartupState.ApprovalUnknown,"unknown startup approval metadata remains unknown");
        store.Value="changed";var writes=store.Writes;var rejected=false;try{startup.Set(true);}catch(IOException){rejected=true;}
        Require(rejected && store.Writes==writes,"changed startup command cannot be silently overwritten");
        var account=new AccountSnapshotResponse{Binding=new(){State="confirmed",AccountScope="scope-a",BindingGeneration=1},Account=new(){Email="private@example.invalid"}};
        var quota=new QuotaCurrentResponse{ProviderContext=new(){EffectiveProvider="codex"},Current=new(){Binding=account.Binding.Clone()}};
        Require(TrayPresentation.CanShowQuota("codex",quota,account) && !TrayPresentation.CanShowQuota("cursor",quota,account),"tray quota verifies its Provider independently from the main selection");
        account.Binding.BindingGeneration=2;Require(!TrayPresentation.CanShowQuota("codex",quota,account),"tray suppresses quota when account generations differ");
        var l=new Localization();l.Set("en-US");Require(!TrayPresentation.Identity(account,true,l).Contains("private") && !TrayPresentation.ClipboardText.Contains("@"),"private screenshot identity and text representation contain no account details");
    }
    public static async Task Parent(string executable,string runtime)
    {
        await using var session=await HelperSession.StartAsync(executable,runtime,Path.Combine(runtime,"synthetic-home"),"dev");
        var file=Path.Combine(runtime,"test-parent-pid.txt");await File.WriteAllTextAsync(file+".tmp",session.ProcessId.ToString());File.Move(file+".tmp",file);
        await Task.Delay(Timeout.Infinite);
    }
    public static async Task Processes(string executable,string root)
    {
        var normal=Path.Combine(root,"normal");
        await using(var session=await HelperSession.StartAsync(executable,normal,Path.Combine(normal,"synthetic-home"),"dev"))
        {
            foreach(var name in new[]{"system_will_sleep","system_did_wake","application_did_become_active"})
            {var receipt=await session.Connection.Raw.NotifyLifecycleAsync(new(){Event=name},deadline:DateTime.UtcNow.AddSeconds(3));Require(receipt.Accepted && receipt.Event==name,"actual Helper acknowledges "+name);}
            Require(await session.StopAsync() && await session.Exit==0,"Job-owned Helper still completes normal drain and exits zero");
        }
        var crash=Path.Combine(root,"crash");var launch=new ProcessStartInfo("dotnet"){UseShellExecute=false,CreateNoWindow=true};
        launch.ArgumentList.Add(typeof(LifecycleTests).Assembly.Location);launch.ArgumentList.Add("--lifecycle-parent");launch.ArgumentList.Add(executable);launch.ArgumentList.Add(crash);
        using var parent=Process.Start(launch)??throw new IOException("test_parent_start_failed");
        try
        {
            var file=Path.Combine(crash,"test-parent-pid.txt");using var deadline=new CancellationTokenSource(TimeSpan.FromSeconds(20));
            while(!File.Exists(file)){if(parent.HasExited)throw new IOException("test_parent_exited");await Task.Delay(50,deadline.Token);}
            var pid=int.Parse(await File.ReadAllTextAsync(file,deadline.Token));using var helper=Process.GetProcessById(pid);
            parent.Kill();await parent.WaitForExitAsync(deadline.Token);await helper.WaitForExitAsync(deadline.Token);
            Require(helper.HasExited,"abrupt parent termination closes the private Job and leaves no Helper orphan");
            await using var restarted=await HelperSession.StartAsync(executable,crash,Path.Combine(crash,"synthetic-home"),"dev");
            Require(restarted.State==CoreConnectionState.Connected && await restarted.StopAsync(),"a new owner recovers the crashed runtime without a second writer");
        }
        finally{if(!parent.HasExited){parent.Kill();await parent.WaitForExitAsync();}}
    }
}
