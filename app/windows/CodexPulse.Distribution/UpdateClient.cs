using System.Net;
using System.Net.Security;
using System.Security.Cryptography;
using System.Security.Cryptography.X509Certificates;

namespace CodexPulse.Distribution;

public sealed class UpdateClient:IDisposable
{
    private readonly UpdatePolicy policy;
    private readonly HttpClient client;
    private readonly X509Certificate2? developmentRoot;
    public UpdateClient(UpdatePolicy policy)
    {
        this.policy=policy;_ = policy.Feed("stable");
        var handler=new HttpClientHandler{AllowAutoRedirect=false,AutomaticDecompression=DecompressionMethods.None};
        if(policy.DevelopmentTlsCertificate.Length>0)
        {
            var root=X509CertificateLoader.LoadCertificate(Convert.FromBase64String(policy.DevelopmentTlsCertificate));developmentRoot=root;
            handler.UseProxy=false;
            handler.ServerCertificateCustomValidationCallback=(request,certificate,_,errors)=>
            {
                if(request.RequestUri?.IsLoopback!=true || certificate is null || (errors&SslPolicyErrors.RemoteCertificateNameMismatch)!=0)return false;
                using var chain=new X509Chain();chain.ChainPolicy.TrustMode=X509ChainTrustMode.CustomRootTrust;chain.ChainPolicy.CustomTrustStore.Add(root);chain.ChainPolicy.RevocationMode=X509RevocationMode.NoCheck;
                chain.ChainPolicy.ApplicationPolicy.Add(new("1.3.6.1.5.5.7.3.1"));return chain.Build(certificate);
            };
        }
        client=new(handler){Timeout=TimeSpan.FromMinutes(10)};
    }
    public async Task<VerifiedRelease> CheckAsync(string channel,CancellationToken cancellation=default)
    {
        using var deadline=CancellationTokenSource.CreateLinkedTokenSource(cancellation);deadline.CancelAfter(TimeSpan.FromSeconds(30));
        using var response=await client.GetAsync(policy.Feed(channel),HttpCompletionOption.ResponseHeadersRead,deadline.Token);response.EnsureSuccessStatusCode();
        if(response.Content.Headers.ContentLength>BundleFormat.MaximumEnvelopeBytes)throw new IOException("update_feed_limit");
        using var memory=new MemoryStream();await CopyBounded(await response.Content.ReadAsStreamAsync(deadline.Token),memory,BundleFormat.MaximumEnvelopeBytes,deadline.Token);
        var verified=BundleFormat.Verify(memory.ToArray(),policy);if(verified.Manifest.Channel!=channel)throw new IOException("update_channel_mismatch");return verified;
    }
    public async Task<string> DownloadAsync(VerifiedRelease release,string directory,IProgress<double>? progress=null,CancellationToken cancellation=default)
    {
        using var target=PinnedDirectory.Open(directory,true,true);var manifest=release.Manifest;var name=manifest.ZipName;
        var uri=new Uri(policy.Feed(manifest.Channel),name);
        using var response=await client.GetAsync(uri,HttpCompletionOption.ResponseHeadersRead,cancellation);response.EnsureSuccessStatusCode();
        if(response.Content.Headers.ContentLength is {} length && length!=manifest.ZipSize)throw new IOException("update_download_size");
        using(var output=target.Create(name))
        {
            using var input=await response.Content.ReadAsStreamAsync(cancellation);using var hash=IncrementalHash.CreateHash(HashAlgorithmName.SHA256);var buffer=new byte[64*1024];long total=0;
            while(true){var read=await input.ReadAsync(buffer,cancellation);if(read==0)break;total+=read;if(total>manifest.ZipSize)throw new IOException("update_download_size");hash.AppendData(buffer,0,read);await output.WriteAsync(buffer.AsMemory(0,read),cancellation);progress?.Report(total/(double)manifest.ZipSize);}
            if(total!=manifest.ZipSize || !Convert.ToHexStringLower(hash.GetHashAndReset()).Equals(manifest.ZipSHA256,StringComparison.OrdinalIgnoreCase))throw new IOException("update_download_hash");output.Flush(true);
        }
        target.AtomicWrite("release.json",release.Envelope);return target.Resolve(name);
    }
    private static async Task CopyBounded(Stream input,Stream output,int limit,CancellationToken cancellation)
    {using(input){var buffer=new byte[64*1024];var size=0;while(true){var read=await input.ReadAsync(buffer,cancellation);if(read==0)break;size+=read;if(size>limit)throw new IOException("update_feed_limit");await output.WriteAsync(buffer.AsMemory(0,read),cancellation);}}}
    public void Dispose(){client.Dispose();developmentRoot?.Dispose();}
}
