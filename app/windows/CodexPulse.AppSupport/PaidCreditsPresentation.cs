using System.Globalization;
using System.Numerics;
using Codexpulse.Core.V1;

namespace CodexPulse.AppSupport;

public static class PaidCreditsPresentation
{
    public static string Amount(string? value,bool unlimited,Localization l)
    {
        if(unlimited)return l.Text("无限额","Unlimited");
        if(value is null)return "—";
        var parts=value.Split('.');
        if(parts.Length>2 || !BigInteger.TryParse(parts[0],NumberStyles.None,CultureInfo.InvariantCulture,out var integer) || integer<0 || parts.Length==2 && (parts[1].Length==0 || parts[1].Any(c=>c is <'0' or >'9')))return "—";
        return integer.ToString("N0",l.Culture)+(parts.Length==2?l.Culture.NumberFormat.NumberDecimalSeparator+parts[1]:"");
    }
    public static string Balance(PaidCredits credit,Localization l)=>Amount(credit.HasBalance?credit.Balance:null,credit.HasUnlimited && credit.Unlimited,l);
}
