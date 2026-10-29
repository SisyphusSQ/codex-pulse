using System.Collections;
using System.ComponentModel;
using System.Runtime.InteropServices;
using System.Text;
using Microsoft.Win32.SafeHandles;

namespace CodexPulse.CoreClient;

// The only inherited handles are the dedicated authentication reader and NUL.
// No shell, credentials in arguments, or broad inheritable-handle launch is used.
internal sealed class NativeHelper : IDisposable
{
    private readonly SafeProcessHandle process;
    private readonly Task<int> exit;
    private readonly ProcessJob job;
    private FileStream? authentication;
    public uint Id { get; }
    public Task<int> Exit => exit;

    private NativeHelper(ProcessInformation information, SafeFileHandle writer, ProcessJob job)
    {
        this.job = job;
        process = new SafeProcessHandle(information.Process, true);
        CloseHandle(information.Thread);
        Id = information.ProcessId;
        authentication = new FileStream(writer, FileAccess.Write, 128, false);
        exit = Task.Run(() =>
        {
            if (WaitForSingleObject(process, uint.MaxValue) != 0 || !GetExitCodeProcess(process, out var code))
                throw new IOException("helper_wait_failed");
            return unchecked((int)code);
        });
    }

    public static NativeHelper Start(string executable, string pipeName, string runtime, string home)
    {
        if (!Path.IsPathFullyQualified(executable) || !File.Exists(executable) || !Path.IsPathFullyQualified(home))
            throw new IOException("helper_path_invalid");
        var security = new SecurityAttributes { Length = Marshal.SizeOf<SecurityAttributes>(), Inherit = true };
        if (!CreatePipe(out var reader, out var writer, ref security, 0)) throw NativeError();
        using (reader)
        {
            try
            {
                if (!SetHandleInformation(writer, 1, 0)) throw NativeError();
                using var nul = CreateFileW("NUL", 0xC0000000, 3, ref security, 3, 0, IntPtr.Zero);
                if (nul.IsInvalid) throw NativeError();
                nuint attributeBytes = 0;
                InitializeProcThreadAttributeList(IntPtr.Zero, 2, 0, ref attributeBytes);
                var attributes = Marshal.AllocHGlobal(checked((int)attributeBytes));
                var handles = Marshal.AllocHGlobal(2 * IntPtr.Size);
                var jobHandles = Marshal.AllocHGlobal(IntPtr.Size);
                IntPtr environment = IntPtr.Zero;
                var initialized = false;
                try
                {
                    if (!InitializeProcThreadAttributeList(attributes, 2, 0, ref attributeBytes)) throw NativeError();
                    initialized = true;
                    Marshal.WriteIntPtr(handles, reader.DangerousGetHandle());
                    Marshal.WriteIntPtr(handles, IntPtr.Size, nul.DangerousGetHandle());
                    if (!UpdateProcThreadAttribute(attributes, 0, (nuint)0x20002, handles, (nuint)(2 * IntPtr.Size), IntPtr.Zero, IntPtr.Zero))
                        throw NativeError();
                    var startup = new StartupInformationEx
                    {
                        Startup = new StartupInformation
                        {
                            Size = Marshal.SizeOf<StartupInformationEx>(), Flags = 0x100,
                            StandardInput = nul.DangerousGetHandle(), StandardOutput = nul.DangerousGetHandle(), StandardError = nul.DangerousGetHandle()
                        },
                        Attributes = attributes
                    };
                    var arguments = new[] { executable, "--pipe", @"\\.\pipe\" + pipeName, "--auth-fd",
                        reader.DangerousGetHandle().ToInt64().ToString(System.Globalization.CultureInfo.InvariantCulture),
                        "--runtime-directory", runtime };
                    var command = new StringBuilder(string.Join(" ", arguments.Select(Quote)));
                    var variables = new SortedDictionary<string, string>(StringComparer.OrdinalIgnoreCase);
                    foreach (DictionaryEntry item in Environment.GetEnvironmentVariables()) variables[(string)item.Key] = (string)item.Value!;
                    variables["CODEX_HOME"] = home;
                    // Query profiling is an explicit developer tool, never inherited by the App helper.
                    variables.Remove("CODEX_PULSE_QUERY_PROFILE");
                    var block = string.Concat(variables.Select(pair => pair.Key + "=" + pair.Value + "\0")) + "\0";
                    environment = Marshal.StringToHGlobalUni(block);
                    var job = ProcessJob.Create();
                    try
                    {
                        Marshal.WriteIntPtr(jobHandles,job.DangerousGetHandle());
                        // Assign at creation, closing the parent-death race
                        // between CreateProcess and AssignProcessToJobObject.
                        if(!UpdateProcThreadAttribute(attributes,0,0x2000D,jobHandles,(nuint)IntPtr.Size,IntPtr.Zero,IntPtr.Zero))throw NativeError();
                        if (!CreateProcessW(executable, command, IntPtr.Zero, IntPtr.Zero, true,
                                0x00080000 | 0x08000000 | 0x00000400 | 0x00000004, environment, runtime, ref startup, out var information))
                            throw NativeError();
                        try
                        {
                            if (ResumeThread(information.Thread) == uint.MaxValue) throw NativeError();
                        }
                        catch
                        {
                            TerminateRawProcess(information.Process, 1);
                            CloseHandle(information.Thread); CloseHandle(information.Process);
                            throw;
                        }
                        return new NativeHelper(information, writer, job);
                    }
                    catch { job.Dispose(); throw; }
                }
                finally
                {
                    if (environment != IntPtr.Zero) Marshal.FreeHGlobal(environment);
                    if (initialized) DeleteProcThreadAttributeList(attributes);
                    Marshal.FreeHGlobal(attributes);
                    Marshal.FreeHGlobal(handles);
                    Marshal.FreeHGlobal(jobHandles);
                }
            }
            catch { writer.Dispose(); throw; }
        }
    }

    public async Task AuthenticateAsync(byte[] bytes, CancellationToken cancellation)
    {
        await authentication!.WriteAsync(bytes, cancellation).ConfigureAwait(false);
        await authentication.FlushAsync(cancellation).ConfigureAwait(false);
    }

    public void CloseParentPipe() => Interlocked.Exchange(ref authentication, null)?.Dispose();
    public Task<bool> DrainJobAsync()=>job.DrainAsync();
    public void Terminate() { if (!exit.IsCompleted && !TerminateProcess(process, 1)) throw NativeError(); }
    public void Dispose() { CloseParentPipe(); job.Dispose(); process.Dispose(); }

    internal static string Quote(string value)
    {
        if (value.IndexOf('\0') >= 0) throw new ArgumentException("invalid_process_argument");
        var result = new StringBuilder("\"");
        var slashes = 0;
        foreach (var character in value)
        {
            if (character == '\\') { slashes++; continue; }
            result.Append('\\', character == '"' ? slashes * 2 + 1 : slashes);
            result.Append(character);
            slashes = 0;
        }
        return result.Append('\\', slashes * 2).Append('"').ToString();
    }

    private static IOException NativeError() => new("helper_process_failed", new Win32Exception(Marshal.GetLastWin32Error()));

    [StructLayout(LayoutKind.Sequential)] private struct SecurityAttributes { public int Length; public IntPtr Descriptor; [MarshalAs(UnmanagedType.Bool)] public bool Inherit; }
    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)] private struct StartupInformation
    {
        public int Size; public IntPtr Reserved, Desktop, Title;
        public uint X, Y, XSize, YSize, XCountChars, YCountChars, FillAttribute, Flags;
        public ushort ShowWindow, ReservedBytes; public IntPtr ReservedData, StandardInput, StandardOutput, StandardError;
    }
    [StructLayout(LayoutKind.Sequential)] private struct StartupInformationEx { public StartupInformation Startup; public IntPtr Attributes; }
    [StructLayout(LayoutKind.Sequential)] private struct ProcessInformation { public IntPtr Process, Thread; public uint ProcessId, ThreadId; }
    [DllImport("kernel32.dll", SetLastError = true)] [return: MarshalAs(UnmanagedType.Bool)] private static extern bool CreatePipe(out SafeFileHandle reader, out SafeFileHandle writer, ref SecurityAttributes security, uint size);
    [DllImport("kernel32.dll", SetLastError = true)] [return: MarshalAs(UnmanagedType.Bool)] private static extern bool SetHandleInformation(SafeFileHandle handle, uint mask, uint flags);
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)] private static extern SafeFileHandle CreateFileW(string name, uint access, uint share, ref SecurityAttributes security, uint creation, uint flags, IntPtr template);
    [DllImport("kernel32.dll", SetLastError = true)] [return: MarshalAs(UnmanagedType.Bool)] private static extern bool InitializeProcThreadAttributeList(IntPtr list, int count, uint flags, ref nuint bytes);
    [DllImport("kernel32.dll", SetLastError = true)] [return: MarshalAs(UnmanagedType.Bool)] private static extern bool UpdateProcThreadAttribute(IntPtr list, uint flags, nuint attribute, IntPtr value, nuint bytes, IntPtr previous, IntPtr returned);
    [DllImport("kernel32.dll")] private static extern void DeleteProcThreadAttributeList(IntPtr list);
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)] [return: MarshalAs(UnmanagedType.Bool)] private static extern bool CreateProcessW(string app, StringBuilder command, IntPtr processSecurity, IntPtr threadSecurity, [MarshalAs(UnmanagedType.Bool)] bool inherit, uint flags, IntPtr environment, string directory, ref StartupInformationEx startup, out ProcessInformation process);
    [DllImport("kernel32.dll", SetLastError = true)] private static extern uint WaitForSingleObject(SafeProcessHandle handle, uint milliseconds);
    [DllImport("kernel32.dll", SetLastError = true)] [return: MarshalAs(UnmanagedType.Bool)] private static extern bool GetExitCodeProcess(SafeProcessHandle process, out uint code);
    [DllImport("kernel32.dll", SetLastError = true)] [return: MarshalAs(UnmanagedType.Bool)] private static extern bool TerminateProcess(SafeProcessHandle process, uint code);
    [DllImport("kernel32.dll", EntryPoint="TerminateProcess", SetLastError=true)] [return:MarshalAs(UnmanagedType.Bool)] private static extern bool TerminateRawProcess(IntPtr process, uint code);
    [DllImport("kernel32.dll", SetLastError=true)] private static extern uint ResumeThread(IntPtr thread);
    [DllImport("kernel32.dll")] [return: MarshalAs(UnmanagedType.Bool)] private static extern bool CloseHandle(IntPtr handle);
}
