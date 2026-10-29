using System.ComponentModel;
using System.Runtime.InteropServices;
using Windows.Graphics;

namespace CodexPulse.App;

// Shell callbacks arrive on the owning WinUI thread. No credentials or business
// values cross window messages; menu commands are a fixed local registry.
internal sealed class NativeTray:IDisposable
{
    private const uint Callback=0x8001;
    private readonly IntPtr window,icon;
    private readonly SubclassProc callback;
    private NotifyData data;
    private readonly uint taskbarCreated=RegisterWindowMessageW("TaskbarCreated");
    private bool disposed;
    public event Action? Toggle,ContextMenu,TaskbarRestored;
    public event Action<string>? Power;
    public event Action? SessionEnding,DisplayChanged;
    public bool Available{get;private set;}
    public NativeTray(IntPtr window)
    {
        if(taskbarCreated==0)throw Failure();
        this.window=window;callback=Receive;
        icon=LoadImageW(IntPtr.Zero,Path.Combine(AppContext.BaseDirectory,"Assets","CodexPulse.ico"),1,32,32,0x10);
        if(icon==IntPtr.Zero)throw Failure();
        data=new(){Size=(uint)Marshal.SizeOf<NotifyData>(),Window=window,Id=1,Flags=1|2|4|0x80,Callback=Callback,Icon=icon,Tip="Codex Pulse",Info="",InfoTitle=""};
        if(!SetWindowSubclass(window,callback,1,0)){DestroyIcon(icon);throw Failure();}
        Add();
    }
    private void Add()
    {
        data.Version=0;Available=Shell_NotifyIconW(0,ref data);
        if(Available){data.Version=4;Available=Shell_NotifyIconW(4,ref data);}
    }
    public void Tooltip(string text)
    {
        data.Tip=text.Length>127?text[..127]:text;
        if(!disposed && Available && !Shell_NotifyIconW(1,ref data))Available=false;
    }
    public RectInt32 Anchor()
    {
        var identity=new NotifyIdentity{Size=(uint)Marshal.SizeOf<NotifyIdentity>(),Window=window,Id=1};
        if(Shell_NotifyIconGetRect(ref identity,out var rect)==0)return new(rect.Left,rect.Top,rect.Right-rect.Left,rect.Bottom-rect.Top);
        GetCursorPos(out var point);return new(point.X,point.Y,1,1);
    }
    private IntPtr Receive(IntPtr hwnd,uint message,nuint wParam,nint lParam,nuint id,nuint reference)
    {
        if(message==Callback)
        {
            var action=(uint)((long)lParam&0xffff);
            if(action is 0x400 or 0x401)Toggle?.Invoke();
            else if(action==0x7b)ContextMenu?.Invoke();
            return IntPtr.Zero;
        }
        if(message==taskbarCreated){Add();TaskbarRestored?.Invoke();}
        else if(message==0x218)
        {
            if(wParam==4)Power?.Invoke("system_will_sleep");
            else if(wParam is 7 or 18)Power?.Invoke("system_did_wake");
            return new IntPtr(1);
        }
        else if(message==0x11){SessionEnding?.Invoke();return new IntPtr(1);}
        else if(message==0x7e || message==0x2e0)DisplayChanged?.Invoke();
        return DefSubclassProc(hwnd,message,wParam,lParam);
    }
    public static int Menu(IntPtr owner,IReadOnlyList<(int Id,string Label)> items)
    {
        var menu=CreatePopupMenu();if(menu==IntPtr.Zero)throw Failure();
        try
        {
            foreach(var item in items)if(!AppendMenuW(menu,item.Id==0?0x800u:0u,(nuint)item.Id,item.Label))throw Failure();
            GetCursorPos(out var point);SetForegroundWindow(owner);
            var command=TrackPopupMenuEx(menu,0x100|2,point.X,point.Y,owner,IntPtr.Zero);
            PostMessageW(owner,0,0,0);return command;
        }
        finally{DestroyMenu(menu);}
    }
    public void Dispose()
    {
        if(disposed)return;disposed=true;
        Shell_NotifyIconW(2,ref data);RemoveWindowSubclass(window,callback,1);DestroyIcon(icon);Available=false;
    }
    private static IOException Failure()=>new("tray_platform_failed",new Win32Exception(Marshal.GetLastWin32Error()));
    private delegate IntPtr SubclassProc(IntPtr window,uint message,nuint wParam,nint lParam,nuint id,nuint reference);
    [StructLayout(LayoutKind.Sequential,CharSet=CharSet.Unicode)]private struct NotifyData
    {
        public uint Size;public IntPtr Window;public uint Id,Flags,Callback;public IntPtr Icon;
        [MarshalAs(UnmanagedType.ByValTStr,SizeConst=128)]public string Tip;
        public uint State,StateMask;
        [MarshalAs(UnmanagedType.ByValTStr,SizeConst=256)]public string Info;
        public uint Version;
        [MarshalAs(UnmanagedType.ByValTStr,SizeConst=64)]public string InfoTitle;
        public uint InfoFlags;public Guid Guid;public IntPtr BalloonIcon;
    }
    [StructLayout(LayoutKind.Sequential)]private struct NotifyIdentity{public uint Size;public IntPtr Window;public uint Id;public Guid Guid;}
    [StructLayout(LayoutKind.Sequential)]private struct Rect{public int Left,Top,Right,Bottom;}
    [StructLayout(LayoutKind.Sequential)]private struct Point{public int X,Y;}
    [DllImport("shell32.dll",CharSet=CharSet.Unicode)] [return:MarshalAs(UnmanagedType.Bool)]private static extern bool Shell_NotifyIconW(uint command,ref NotifyData data);
    [DllImport("shell32.dll")]private static extern int Shell_NotifyIconGetRect(ref NotifyIdentity identity,out Rect rectangle);
    [DllImport("comctl32.dll",SetLastError=true)] [return:MarshalAs(UnmanagedType.Bool)]private static extern bool SetWindowSubclass(IntPtr window,SubclassProc callback,nuint id,nuint reference);
    [DllImport("comctl32.dll")] [return:MarshalAs(UnmanagedType.Bool)]private static extern bool RemoveWindowSubclass(IntPtr window,SubclassProc callback,nuint id);
    [DllImport("comctl32.dll")]private static extern IntPtr DefSubclassProc(IntPtr window,uint message,nuint wParam,nint lParam);
    [DllImport("user32.dll",CharSet=CharSet.Unicode)]private static extern uint RegisterWindowMessageW(string name);
    [DllImport("user32.dll",CharSet=CharSet.Unicode,SetLastError=true)]private static extern IntPtr LoadImageW(IntPtr instance,string name,uint kind,int width,int height,uint flags);
    [DllImport("user32.dll")] [return:MarshalAs(UnmanagedType.Bool)]private static extern bool DestroyIcon(IntPtr icon);
    [DllImport("user32.dll")] [return:MarshalAs(UnmanagedType.Bool)]private static extern bool GetCursorPos(out Point point);
    [DllImport("user32.dll")] [return:MarshalAs(UnmanagedType.Bool)]private static extern bool SetForegroundWindow(IntPtr window);
    [DllImport("user32.dll")]private static extern IntPtr CreatePopupMenu();
    [DllImport("user32.dll",CharSet=CharSet.Unicode,SetLastError=true)] [return:MarshalAs(UnmanagedType.Bool)]private static extern bool AppendMenuW(IntPtr menu,uint flags,nuint id,string text);
    [DllImport("user32.dll")]private static extern int TrackPopupMenuEx(IntPtr menu,uint flags,int x,int y,IntPtr owner,IntPtr exclude);
    [DllImport("user32.dll")] [return:MarshalAs(UnmanagedType.Bool)]private static extern bool DestroyMenu(IntPtr menu);
    [DllImport("user32.dll",CharSet=CharSet.Unicode)] [return:MarshalAs(UnmanagedType.Bool)]private static extern bool PostMessageW(IntPtr window,uint message,nuint wParam,nint lParam);
}
