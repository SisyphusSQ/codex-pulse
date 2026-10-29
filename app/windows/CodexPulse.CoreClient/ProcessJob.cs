using System.ComponentModel;
using System.Runtime.InteropServices;
using Microsoft.Win32.SafeHandles;

namespace CodexPulse.CoreClient;

// The handle is never inheritable. Parent death closes it, including when a
// suspended child has not yet received its authentication frame.
internal sealed class ProcessJob : SafeHandleZeroOrMinusOneIsInvalid
{
    private ProcessJob() : base(true) { }
    public static ProcessJob Create()
    {
        var job = CreateJobObjectW(IntPtr.Zero, null);
        if (job.IsInvalid) { job.Dispose(); throw Failure(); }
        var limits = new ExtendedLimits { Basic = new BasicLimits { Flags = 0x2000 } };
        if (!SetInformationJobObject(job, 9, ref limits, (uint)Marshal.SizeOf<ExtendedLimits>()))
        { job.Dispose(); throw Failure(); }
        return job;
    }
    private static IOException Failure() => new("helper_job_failed", new Win32Exception(Marshal.GetLastWin32Error()));
    public async Task<bool> DrainAsync()
    {
        if(!QueryInformationJobObject(this,1,out var accounting,(uint)Marshal.SizeOf<Accounting>(),IntPtr.Zero))throw Failure();
        if(accounting.ActiveProcesses==0)return true;
        // Process exit and nested Job teardown are asynchronous. A signaled
        // Helper handle is not yet proof that every job notification settled.
        var settle=Environment.TickCount64+1000;
        do
        {
            await Task.Delay(20).ConfigureAwait(false);
            if(!QueryInformationJobObject(this,1,out accounting,(uint)Marshal.SizeOf<Accounting>(),IntPtr.Zero))throw Failure();
            if(accounting.ActiveProcesses==0)return true;
        }while(Environment.TickCount64<settle);
        if(!TerminateJobObject(this,1))throw Failure();
        var end=Environment.TickCount64+5000;
        do
        {
            await Task.Delay(20).ConfigureAwait(false);
            if(!QueryInformationJobObject(this,1,out accounting,(uint)Marshal.SizeOf<Accounting>(),IntPtr.Zero))throw Failure();
            if(accounting.ActiveProcesses==0)return false;
        }while(Environment.TickCount64<end);
        throw new IOException("helper_job_drain_uncertain");
    }
    protected override bool ReleaseHandle() => CloseHandle(handle);
    [StructLayout(LayoutKind.Sequential)] private struct BasicLimits
    {
        public long ProcessTime, JobTime;
        public uint Flags;
        public nuint MinWorkingSet, MaxWorkingSet;
        public uint ActiveProcesses;
        public nuint Affinity;
        public uint Priority, Scheduling;
    }
    [StructLayout(LayoutKind.Sequential)] private struct IoCounters { public ulong ReadOperations, WriteOperations, OtherOperations, ReadBytes, WriteBytes, OtherBytes; }
    [StructLayout(LayoutKind.Sequential)] private struct ExtendedLimits
    { public BasicLimits Basic; public IoCounters Io; public nuint ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory; }
    [StructLayout(LayoutKind.Sequential)]private struct Accounting{public long UserTime,KernelTime,PeriodUserTime,PeriodKernelTime;public uint PageFaults,TotalProcesses,ActiveProcesses,TerminatedProcesses;}
    [DllImport("kernel32.dll", CharSet=CharSet.Unicode, SetLastError=true)] private static extern ProcessJob CreateJobObjectW(IntPtr attributes, string? name);
    [DllImport("kernel32.dll", SetLastError=true)] [return:MarshalAs(UnmanagedType.Bool)] private static extern bool SetInformationJobObject(ProcessJob job, int kind, ref ExtendedLimits limits, uint bytes);
    [DllImport("kernel32.dll",SetLastError=true)] [return:MarshalAs(UnmanagedType.Bool)]private static extern bool QueryInformationJobObject(ProcessJob job,int kind,out Accounting info,uint bytes,IntPtr returned);
    [DllImport("kernel32.dll",SetLastError=true)] [return:MarshalAs(UnmanagedType.Bool)]private static extern bool TerminateJobObject(ProcessJob job,uint code);
    [DllImport("kernel32.dll")] [return:MarshalAs(UnmanagedType.Bool)] private static extern bool CloseHandle(IntPtr value);
}
