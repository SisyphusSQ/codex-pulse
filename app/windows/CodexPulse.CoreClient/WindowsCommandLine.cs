namespace CodexPulse.CoreClient;

public static class WindowsCommandLine
{
    public static string Join(params string[] arguments)=>string.Join(" ",arguments.Select(NativeHelper.Quote));
    public static string[] Split(string commandLine)
    {
        if(commandLine.Length>32767 || commandLine.Contains('\0'))throw new ArgumentException("command_line_invalid");
        var memory=CommandLineToArgvW(commandLine,out var count);if(memory==IntPtr.Zero)throw new IOException("command_line_invalid");
        try{var result=new string[count];for(var i=0;i<count;i++)result[i]=System.Runtime.InteropServices.Marshal.PtrToStringUni(System.Runtime.InteropServices.Marshal.ReadIntPtr(memory,i*IntPtr.Size))??"";return result;}
        finally{LocalFree(memory);}
    }
    public static string? UpdateNonce(string commandLine)
    {var values=Split(commandLine);for(var i=0;i+1<values.Length;i++)if(values[i]=="--quit-for-update" && Guid.TryParseExact(values[i+1],"N",out _))return values[i+1];return null;}
    [System.Runtime.InteropServices.DllImport("shell32.dll",CharSet=System.Runtime.InteropServices.CharSet.Unicode,SetLastError=true)]private static extern IntPtr CommandLineToArgvW(string value,out int count);
    [System.Runtime.InteropServices.DllImport("kernel32.dll")]private static extern IntPtr LocalFree(IntPtr memory);
}
