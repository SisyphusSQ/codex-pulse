using System.Security.AccessControl;
using System.Security.Principal;

namespace CodexPulse.CoreClient;

public static class PrivateRuntime
{
    public static void Ensure(string path)
    {
        if (!Path.IsPathFullyQualified(path)) throw new IOException("runtime_path_invalid");
        var user = WindowsIdentity.GetCurrent().User ?? throw new IOException("runtime_owner_unavailable");
        var directory = new DirectoryInfo(path);
        for (var parent = directory.Parent; parent is not null; parent = parent.Parent)
            if (parent.Exists && (parent.Attributes & FileAttributes.ReparsePoint) != 0)
                throw new IOException("runtime_reparse_rejected");
        if (!directory.Exists)
        {
            var security = new DirectorySecurity();
            security.SetOwner(user);
            security.SetAccessRuleProtection(true, false);
            foreach (var sid in new[] { user, new SecurityIdentifier(WellKnownSidType.LocalSystemSid, null) })
                security.AddAccessRule(new FileSystemAccessRule(sid, FileSystemRights.FullControl,
                    InheritanceFlags.ContainerInherit | InheritanceFlags.ObjectInherit,
                    PropagationFlags.None, AccessControlType.Allow));
            directory.Create(security);
        }
        directory.Refresh();
        if ((directory.Attributes & FileAttributes.ReparsePoint) != 0) throw new IOException("runtime_reparse_rejected");
        var current = directory.GetAccessControl(AccessControlSections.Owner | AccessControlSections.Access);
        if (!user.Equals(current.GetOwner(typeof(SecurityIdentifier)))) throw new IOException("runtime_owner_invalid");
        var userAllowed = false;
        foreach (FileSystemAccessRule rule in current.GetAccessRules(true, true, typeof(SecurityIdentifier)))
        {
            if (rule.AccessControlType != AccessControlType.Allow) continue;
            if (!rule.IdentityReference.Equals(user) &&
                !rule.IdentityReference.Equals(new SecurityIdentifier(WellKnownSidType.LocalSystemSid, null)))
                throw new IOException("runtime_acl_invalid");
            userAllowed |= rule.IdentityReference.Equals(user);
        }
        if (!userAllowed) throw new IOException("runtime_acl_invalid");
    }
}
