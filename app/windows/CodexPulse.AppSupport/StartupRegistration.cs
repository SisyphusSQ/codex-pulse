using CodexPulse.CoreClient;
using Microsoft.Win32;

namespace CodexPulse.AppSupport;

public interface IStartupStore
{
    string? Command(string name);
    byte[]? Approval(string name);
    void SetCommand(string name,string? command);
}
public sealed class RegistryStartupStore:IStartupStore
{
    private const string Run=@"Software\Microsoft\Windows\CurrentVersion\Run";
    public string? Command(string name){using var key=Registry.CurrentUser.OpenSubKey(Run);return key?.GetValue(name, null, RegistryValueOptions.DoNotExpandEnvironmentNames) as string;}
    public byte[]? Approval(string name){using var key=Registry.CurrentUser.OpenSubKey(@"Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run");return key?.GetValue(name) as byte[];}
    public void SetCommand(string name,string? command)
    {
        using var key=Registry.CurrentUser.CreateSubKey(Run);
        if(command is null)key.DeleteValue(name,false);else key.SetValue(name,command,RegistryValueKind.String);
    }
}
public enum StartupState{Off,Configured,DisabledBySystem,ApprovalUnknown,DifferentCommand}
public sealed class StartupRegistration(IStartupStore store,string name,string executable,string runtime,string home)
{
    public string ExpectedCommand{get;}=WindowsCommandLine.Join(executable,"--runtime-directory",runtime,"--codex-home",home,"--background");
    public StartupState Read()
    {
        var command=store.Command(name);
        if(command is null)return StartupState.Off;
        if(command!=ExpectedCommand)return StartupState.DifferentCommand;
        // StartupApproved is optional OS metadata. Unrecognized data never
        // implies enabled; only Run is written by this application.
        var approval=store.Approval(name);
        if(approval is null)return StartupState.Configured;
        if(approval.Length!=12)return StartupState.ApprovalUnknown;
        return BitConverter.ToUInt32(approval) switch{2 or 6=>StartupState.Configured,3 or 7=>StartupState.DisabledBySystem,_=>StartupState.ApprovalUnknown};
    }
    public StartupState Set(bool enabled)
    {
        if(Read()==StartupState.DifferentCommand)throw new IOException("startup_registration_changed");
        store.SetCommand(name,enabled?ExpectedCommand:null);return Read();
    }
}
