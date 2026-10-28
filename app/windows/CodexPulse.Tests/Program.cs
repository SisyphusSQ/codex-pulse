using Codexpulse.Core.V1;
using Google.Protobuf;

static void Require(bool condition, string name)
{
    if (!condition) throw new InvalidOperationException(name);
    Console.WriteLine($"PASS {name}");
}

var unknown = NumericValue.Parser.ParseFrom(new NumericValue().ToByteArray());
Require(!unknown.HasValue, "optional absent remains unknown");
var zero = NumericValue.Parser.ParseFrom(new NumericValue { Value = 0 }.ToByteArray());
Require(zero.HasValue && zero.Value == 0, "explicit zero survives wire roundtrip");
var large = NumericValue.Parser.ParseFrom(new NumericValue { Value = long.MaxValue }.ToByteArray());
Require(large.Value == long.MaxValue, "int64 precision survives wire roundtrip");
var methods = CoreService.Descriptor.Methods;
Require(methods.Single(m => m.Name == "SubscribeInvalidations").IsServerStreaming,
    "invalidation retains server streaming contract");
Require(methods.Single(m => m.Name == "UpdateAPICredential").IsServerStreaming == false,
    "credential mutation uses generated unary contract");

if (args.Length == 3 && args[0] == "--transport") await TransportTests.Run(Path.GetFullPath(args[1]), Path.GetFullPath(args[2]));
if (args.Length == 3 && args[0] == "--wire-peer") await TransportTests.WirePeer(Path.GetFullPath(args[1]), Path.GetFullPath(args[2]));
if (args.Length == 0) { await ShellTests.Run(); DashboardTests.Run(); }
