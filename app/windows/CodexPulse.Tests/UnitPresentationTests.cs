using Codexpulse.Core.V1;
using CodexPulse.AppSupport;

internal static class UnitPresentationTests
{
    public static void Run()
    {
        var l=new Localization();
        void Require(bool condition,string name)
        {
            if(!condition)throw new InvalidOperationException(name);
            Console.WriteLine("PASS "+name);
        }
        l.Set("zh-CN");
        // Same examples as macOS testTokenQuantityFormatterUsesLocalizedMagnitudeUnits.
        foreach(var (value,expected) in new (long,string)[]{(0,"0"),(999_999,"100 万"),(1_000_000,"100 万"),(9_999_999,"1000 万"),(99_999_999,"1 亿"),(1_360_518_381,"13.61 亿"),(4_002_161_268,"40.02 亿")})
            Require(l.Tokens(value)==expected,"Chinese token units: "+value);
        Require(l.Tokens(790_000_000,true)=="7.9亿","compact Chinese tokens match menu presentation");
        Require(l.Tokens((long?)null)=="—" && l.Tokens(-1)=="—" && l.Tokens(new NumericValue())=="—","unknown token values remain unknown");
        Require(l.Tokens(long.MaxValue)=="92233720368.55 亿","int64 tokens do not overflow during display scaling");
        l.Set("en-US");
        foreach(var (value,expected) in new (long,string)[]{(999,"999"),(1_000,"1K"),(999_999,"1M"),(23_000_000,"23M"),(999_999_999,"1B"),(1_360_518_381,"1.36B")})
            Require(l.Tokens(value)==expected,"English token units: "+value);
        Require(l.Tokens(1_360_518_381,true)=="1.4B","compact English tokens use one decimal");
        Require(l.Money(new(){Value=70_220_000})=="$70.22" && l.Money(new(){Value=1})=="$0.000001","dollar display retains small nonzero amounts");
        Require(AnalysisQueries.Duration(new(){Value=1234,Unit="milliseconds"},l)=="1.234 s","duration retains milliseconds like macOS");
        Require(AnalysisQueries.Duration(new(){Value=1234,Unit="seconds"},l)=="—" && AnalysisQueries.Tps(new(){AverageOutputMilliTps=new(){Value=1000,Unit="tokens"}},l)=="—","mismatched duration and throughput units remain unknown like macOS");
        Require(AnalysisQueries.CacheRate(new(){Value=100,Unit="percent"},l)=="—" && AnalysisQueries.CacheRate(new(){Value=100,Unit="basis_points",UnknownReason="partial"},l)=="—","cache rate requires basis points and confirmed numeric facts like macOS");
        foreach(var locale in new[]{"zh-CN","en-US"})
        {
            l.Set(locale);
            foreach(var (value,expected) in new (long,string)[]{(0,"0.0 B"),(1023,"1023.0 B"),(1024,"1.0 KB"),(1536,"1.5 KB"),(1_048_576,"1.0 MB"),(1_073_741_824,"1.0 GB"),(1_099_511_627_776,"1.0 TB")})
                Require(RuntimeCopy.Bytes(new(){Value=value},l)==expected,"macOS byte units: "+locale+" "+value);
        }
        Require(RuntimeCopy.Bytes(null,l)=="—" && RuntimeCopy.Bytes(new(),l)=="—","missing byte values remain unknown");
        var midnight=new DateTimeOffset(2026,10,29,0,30,0,TimeSpan.Zero).ToUnixTimeMilliseconds();
        Require(l.Timestamp(midnight,"America/Los_Angeles","yyyy-MM-dd")=="2026-10-28" && l.Timestamp(midnight,"Asia/Shanghai","yyyy-MM-dd")=="2026-10-29","chart dates follow the reporting zone rather than the machine zone");
        var repeatedHour=new DateTimeOffset(2026,11,1,8,30,0,TimeSpan.Zero).ToUnixTimeMilliseconds();
        Require(l.Timestamp(repeatedHour,"America/Los_Angeles","HH:mm zzz")=="01:30 -07:00" && l.Timestamp(repeatedHour+3_600_000,"America/Los_Angeles","HH:mm zzz")=="01:30 -08:00","reporting timestamps retain daylight-saving offsets");
        Require(l.Timestamp(null,"UTC")=="—" && l.Timestamp(-1,"UTC")=="—","missing or invalid reporting timestamps remain unknown");
    }
}
