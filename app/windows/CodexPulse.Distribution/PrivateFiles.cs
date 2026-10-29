using System.ComponentModel;
using System.Runtime.InteropServices;
using System.Security.Cryptography;
using Microsoft.Win32.SafeHandles;
using CodexPulse.CoreClient;

namespace CodexPulse.Distribution;

// Ancestor handles deny delete/rename, and every leaf is opened no-follow.
// Private ACL creation is shared with the runtime's existing boundary.
public sealed class PinnedDirectory:IDisposable
{
    private readonly List<SafeFileHandle> handles=[];
    public string Path{get;}
    private PinnedDirectory(string path){Path=path;}
    public static PinnedDirectory Open(string path,bool create=false,bool requirePrivate=false)
    {
        var full=System.IO.Path.TrimEndingDirectorySeparator(System.IO.Path.GetFullPath(path));
        if(!System.IO.Path.IsPathFullyQualified(path) || full.StartsWith("\\",StringComparison.Ordinal) || full.IndexOf(':',2)>=0)throw new IOException("distribution_path_invalid");
        var value=new PinnedDirectory(full);
        try
        {
            var drive=System.IO.Path.GetPathRoot(full)!;var current=drive;
            value.handles.Add(Native.Open(current,0x80|0x20000,3,3,true));
            foreach(var component in full[drive.Length..].Split(System.IO.Path.DirectorySeparatorChar,StringSplitOptions.RemoveEmptyEntries))
            {
                current=System.IO.Path.Combine(current,component);
                if(!Directory.Exists(current) && create)PrivateRuntime.Ensure(current);
                value.handles.Add(Native.Open(current,0x80|0x20000,3,3,true));
            }
            if(requirePrivate)PrivateRuntime.Ensure(full);
            return value;
        }
        catch{value.Dispose();throw;}
    }
    public void Dispose(){for(var i=handles.Count-1;i>=0;i--)handles[i].Dispose();handles.Clear();}
    public string Resolve(string relative)
    {BundleFormat.ValidatePath(relative);var full=System.IO.Path.GetFullPath(System.IO.Path.Combine(Path,relative.Replace('/',System.IO.Path.DirectorySeparatorChar)));var prefix=System.IO.Path.EndsInDirectorySeparator(Path)?Path:Path+System.IO.Path.DirectorySeparatorChar;if(!full.StartsWith(prefix,StringComparison.OrdinalIgnoreCase))throw new IOException("distribution_path_escape");return full;}
    public FileStream Read(string relative)
    {var full=Resolve(relative);var parent=Open(System.IO.Path.GetDirectoryName(full)!);try{return new PinnedFile(Native.Open(full,0x80000000|0x20000,1,3,false),FileAccess.Read,parent);}catch{parent.Dispose();throw;}}
    public FileStream Create(string relative)
    {var full=Resolve(relative);var parent=Open(System.IO.Path.GetDirectoryName(full)!,true);try{return new PinnedFile(Native.Open(full,0x40000000|0x20000,0,1,false),FileAccess.Write,parent);}catch{parent.Dispose();throw;}}
    public FileStream Lock(string name,bool create=false)
    {var full=Resolve(name);var parent=Open(System.IO.Path.GetDirectoryName(full)!);try{return new PinnedFile(Native.Open(full,0xc0000000|0x20000,0,create?4u:3u,false),FileAccess.ReadWrite,parent);}catch{parent.Dispose();throw;}}
    public void AtomicWrite(string name,byte[] data)
    {
        BundleFormat.ValidatePath(name);if(name.Contains('/'))throw new IOException("atomic_name_invalid");
        var temporary=".write-"+Guid.NewGuid().ToString("N");
        try{using(var file=Create(temporary)){file.Write(data);file.Flush(true);}File.Move(Resolve(temporary),Resolve(name),true);}
        finally{if(File.Exists(Resolve(temporary)))File.Delete(Resolve(temporary));}
    }
    public byte[] ReadBytes(string name,int maximum)
    {using var file=Read(name);if(file.Length>maximum)throw new IOException("distribution_file_limit");var result=new byte[checked((int)file.Length)];file.ReadExactly(result);return result;}
    public static string FileHash(FileStream file)=>Convert.ToHexStringLower(SHA256.HashData(file));
    private sealed class PinnedFile(SafeFileHandle file,FileAccess access,PinnedDirectory parent):FileStream(file,access)
    {
        protected override void Dispose(bool disposing){try{base.Dispose(disposing);}finally{if(disposing)parent.Dispose();}}
        public override async ValueTask DisposeAsync(){try{await base.DisposeAsync();}finally{parent.Dispose();}GC.SuppressFinalize(this);}
    }
    public static void DeleteTree(string path,string ownedRoot)
    {
        var target=System.IO.Path.GetFullPath(path);var root=System.IO.Path.GetFullPath(ownedRoot);
        if(target!=root && !target.StartsWith(root+System.IO.Path.DirectorySeparatorChar,StringComparison.OrdinalIgnoreCase))throw new IOException("delete_boundary_invalid");
        using(var directory=Open(target))
        {
            foreach(var item in new DirectoryInfo(target).EnumerateFileSystemInfos())
            {
                if((item.Attributes&FileAttributes.ReparsePoint)!=0)throw new IOException("delete_reparse_rejected");
                if(item is DirectoryInfo)DeleteTree(item.FullName,root);else Native.Delete(item.FullName,false);
            }
        }
        using var parent=Open(System.IO.Path.GetDirectoryName(target)!);Native.Delete(target,true);
    }
    private static class Native
    {
        [StructLayout(LayoutKind.Sequential)]private struct Info{public uint Attributes;public System.Runtime.InteropServices.ComTypes.FILETIME Created,Accessed,Written;public uint Volume,SizeHigh,SizeLow,Links,IndexHigh,IndexLow;}
        [StructLayout(LayoutKind.Sequential)]private struct Disposition{[MarshalAs(UnmanagedType.Bool)]public bool Delete;}
        [DllImport("kernel32.dll",CharSet=CharSet.Unicode,SetLastError=true)]private static extern SafeFileHandle CreateFileW(string path,uint access,uint share,IntPtr security,uint creation,uint flags,IntPtr template);
        [DllImport("kernel32.dll",SetLastError=true)]private static extern bool GetFileInformationByHandle(SafeFileHandle file,out Info info);
        [DllImport("kernel32.dll",SetLastError=true)]private static extern bool SetFileInformationByHandle(SafeFileHandle file,int kind,ref Disposition info,uint size);
        internal static SafeFileHandle Open(string path,uint access,uint share,uint creation,bool directory)
        {
            var native="\\\\?\\"+System.IO.Path.GetFullPath(path);var file=CreateFileW(native,access,share,IntPtr.Zero,creation,0x200000|(directory?0x2000000u:0),IntPtr.Zero);
            if(file.IsInvalid){var error=Marshal.GetLastWin32Error();file.Dispose();throw new IOException(error==32?"distribution_file_in_use":"distribution_file_unavailable",new Win32Exception(error));}
            if(!GetFileInformationByHandle(file,out var info) || (info.Attributes&0x400)!=0 || ((info.Attributes&0x10)!=0)!=directory || !directory && info.Links!=1){file.Dispose();throw new IOException("distribution_unsafe_file");}
            return file;
        }
        internal static void Delete(string path,bool directory)
        {using var file=Open(path,0x10000|0x80|0x20000,3,3,directory);var info=new Disposition{Delete=true};if(!SetFileInformationByHandle(file,4,ref info,(uint)Marshal.SizeOf<Disposition>()))throw new IOException("distribution_delete_failed",new Win32Exception(Marshal.GetLastWin32Error()));}
    }
}
