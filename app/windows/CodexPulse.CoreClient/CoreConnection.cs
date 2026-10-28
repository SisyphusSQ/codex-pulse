using System.IO.Pipes;
using System.Runtime.InteropServices;
using Codexpulse.Core.V1;
using Grpc.Core;
using Grpc.Core.Interceptors;
using Grpc.Net.Client;

namespace CodexPulse.CoreClient;

public sealed class CoreConnection : IDisposable
{
    public const string ContractVersion = "core-rpc-v9";
    public const int MaximumMessageBytes = 16 * 1024 * 1024;
    private readonly GrpcChannel channel;
    public CoreService.CoreServiceClient Raw { get; }

    public CoreConnection(string pipeName, uint serverProcessId, string token)
    {
        var handler = new SocketsHttpHandler
        {
            UseProxy = false,
            ConnectTimeout = TimeSpan.FromSeconds(30),
            ConnectCallback = async (_, cancellation) =>
            {
                var pipe = new NamedPipeClientStream(".", pipeName, PipeDirection.InOut,
                    PipeOptions.Asynchronous | PipeOptions.CurrentUserOnly);
                try
                {
                    await pipe.ConnectAsync(cancellation).ConfigureAwait(false);
                    if (!GetNamedPipeServerProcessId(pipe.SafePipeHandle, out var actual) || actual != serverProcessId)
                        throw new IOException("helper_identity_mismatch");
                    return pipe;
                }
                catch
                {
                    await pipe.DisposeAsync().ConfigureAwait(false);
                    throw;
                }
            }
        };
        channel = GrpcChannel.ForAddress("http://localhost", new GrpcChannelOptions
        {
            HttpHandler = handler,
            MaxReceiveMessageSize = MaximumMessageBytes,
            MaxSendMessageSize = MaximumMessageBytes,
            DisposeHttpClient = true
        });
        Raw = new CoreService.CoreServiceClient(channel.Intercept(new Authentication("Bearer " + token)));
    }

    public async Task<HandshakeResponse> HandshakeAsync(string version, CancellationToken cancellation = default)
    {
        var response = await Raw.HandshakeAsync(new HandshakeRequest
        {
            ClientName = "codex-pulse-windows", ClientVersion = version, ContractVersion = ContractVersion
        }, deadline: DateTime.UtcNow.AddSeconds(30), cancellationToken: cancellation).ResponseAsync.ConfigureAwait(false);
        if (response.ContractVersion != ContractVersion || response.Transport != "grpc+namedpipe" || response.HelperVersion != version)
            throw new IOException("helper_contract_mismatch");
        return response;
    }

    public static ErrorDetail? ErrorDetail(RpcException exception)
    {
        var status = exception.GetRpcStatus();
        return status?.Details.FirstOrDefault(detail => detail.Is(Codexpulse.Core.V1.ErrorDetail.Descriptor))
            ?.Unpack<Codexpulse.Core.V1.ErrorDetail>();
    }

    public static bool MutationOutcomeMayBeUnknown(RpcException exception) =>
        exception.StatusCode is StatusCode.DeadlineExceeded or StatusCode.Cancelled or StatusCode.Unavailable;

    public void Dispose() => channel.Dispose();

    [DllImport("kernel32.dll", SetLastError = true)]
    [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool GetNamedPipeServerProcessId(Microsoft.Win32.SafeHandles.SafePipeHandle pipe, out uint processId);

    private sealed class Authentication(string header) : Interceptor
    {
        private ClientInterceptorContext<TRequest, TResponse> AddHeader<TRequest, TResponse>(ClientInterceptorContext<TRequest, TResponse> context)
            where TRequest : class where TResponse : class
        {
            var headers = new Metadata();
            if (context.Options.Headers is { } existing)
                foreach (var entry in existing) headers.Add(entry);
            headers.Add("authorization", header);
            return new ClientInterceptorContext<TRequest, TResponse>(context.Method, context.Host, context.Options.WithHeaders(headers));
        }

        public override TResponse BlockingUnaryCall<TRequest, TResponse>(TRequest request, ClientInterceptorContext<TRequest, TResponse> context,
            BlockingUnaryCallContinuation<TRequest, TResponse> continuation) => continuation(request, AddHeader(context));

        public override AsyncUnaryCall<TResponse> AsyncUnaryCall<TRequest, TResponse>(TRequest request, ClientInterceptorContext<TRequest, TResponse> context,
            AsyncUnaryCallContinuation<TRequest, TResponse> continuation) => continuation(request, AddHeader(context));

        public override AsyncServerStreamingCall<TResponse> AsyncServerStreamingCall<TRequest, TResponse>(TRequest request, ClientInterceptorContext<TRequest, TResponse> context,
            AsyncServerStreamingCallContinuation<TRequest, TResponse> continuation) => continuation(request, AddHeader(context));
    }
}
