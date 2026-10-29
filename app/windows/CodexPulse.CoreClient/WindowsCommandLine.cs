namespace CodexPulse.CoreClient;

public static class WindowsCommandLine
{
    public static string Join(params string[] arguments)=>string.Join(" ",arguments.Select(NativeHelper.Quote));
}
