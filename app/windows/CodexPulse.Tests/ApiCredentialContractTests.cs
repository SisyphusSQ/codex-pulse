using Codexpulse.Core.V1;
using CodexPulse.AppSupport;
using CodexPulse.CoreClient;

internal static class ApiCredentialContractTests
{
    private static void Require(bool value,string name){if(!value)throw new InvalidOperationException(name);Console.WriteLine("PASS "+name);}
    public static async Task Run(string executable,string runtime)
    {
        // Fixed closed loopback proxy prevents either fixed HTTPS API endpoint
        // from being contacted. Only synthetic secrets are used in this process.
        foreach(var key in new[]{"HTTPS_PROXY","https_proxy","HTTP_PROXY","http_proxy"})Environment.SetEnvironmentVariable(key,"http://127.0.0.1:1");
        foreach(var key in new[]{"NO_PROXY","no_proxy"})Environment.SetEnvironmentVariable(key,"");
        HelperSession? session=null;var restarts=0;
        try
        {
            var home=Path.Combine(runtime,"synthetic-home");session=await HelperSession.StartAsync(executable,runtime,home,"dev");
            var context=new QueryContext("",new(2026,10,29),new(2026,10,30),"UTC",1);
            using var controller=new ApiSubscriptionsController(new(()=>session!.Connection.Raw),()=>context,async()=>
            {
                if(!await session!.StopAsync())return false;
                await session.DisposeAsync();session=await HelperSession.StartAsync(executable,runtime,home,"dev");restarts++;
                return session.State==CoreConnectionState.Connected;
            });
            await controller.ReloadAsync();Require(controller.Credentials.State.Value is{DeepSeekConfigured:false,OpenCodeGoConfigured:false} && controller.Current.State.Value?.DeepSeek.Status.State=="unconfigured","actual generated C# to Go API RPC starts with no configured keys or fabricated source data");
            await controller.UpdateAsync("deepseek",System.Text.Encoding.UTF8.GetBytes("synthetic-deepseek-contract-key"));
            Require(controller.Action==ApiCredentialAction.Applied && controller.Credentials.State.Value is{DeepSeekConfigured:true,OpenCodeGoConfigured:false} && restarts==1,"actual Windows private credential store persists an independent key across a clean Helper restart");
            Require(controller.Current.State.Value?.DeepSeek.Status.State=="unavailable" && controller.Current.State.Value.OpenCodeGo.Status.State=="unconfigured","blocked loopback network failure remains independent of the unconfigured second API source");
            await controller.UpdateAsync("opencode_go",System.Text.Encoding.UTF8.GetBytes("synthetic-go-contract-key"));
            Require(controller.Credentials.State.Value is{DeepSeekConfigured:true,OpenCodeGoConfigured:true} && restarts==2,"saving the second API key preserves the first service's configuration after restart");
            await controller.UpdateAsync("deepseek",[],true);
            Require(controller.Action==ApiCredentialAction.Applied && controller.Credentials.State.Value is{DeepSeekConfigured:false,OpenCodeGoConfigured:true} && controller.Current.State.Value?.DeepSeek.Status.State=="unconfigured","actual credential deletion removes only its source and hides its old generation");
            await controller.UpdateAsync("opencode_go",[],true);
            Require(controller.Credentials.State.Value is{DeepSeekConfigured:false,OpenCodeGoConfigured:false} && restarts==4,"both synthetic API keys can be deleted and remain absent after the fourth actual restart");
            Require(await session.StopAsync(),"API credential contract Helper and private SQLite stores drain cleanly");
        }
        finally{if(session is not null)await session.DisposeAsync();}
    }
}
