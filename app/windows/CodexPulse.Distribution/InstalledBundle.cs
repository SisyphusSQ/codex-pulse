namespace CodexPulse.Distribution;

public sealed record InstalledBundle(string Root,InstalledVersion Version,VerifiedRelease Release)
{
    public static InstalledBundle? Discover(string appDirectory,UpdatePolicy policy)
    {
        var directory=new DirectoryInfo(Path.GetFullPath(appDirectory));var releases=directory.Parent;
        if(releases?.Name!="releases" || releases.Parent is null || !directory.Name.StartsWith('b') || !long.TryParse(directory.Name.AsSpan(1),out _))return null;
        var installation=new Installation(releases.Parent.FullName,policy);var current=installation.Current()??throw new IOException("installation_missing");
        if(!installation.VersionPath(current).Equals(directory.FullName.TrimEnd(Path.DirectorySeparatorChar),StringComparison.OrdinalIgnoreCase))throw new IOException("installation_version_changed");
        return new(installation.Root,current,installation.VerifyInstalled(current));
    }
}
