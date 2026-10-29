namespace CodexPulse.AppSupport;

public sealed class RestartBudget
{
    private readonly Queue<DateTimeOffset> failures=[];
    public TimeSpan? Claim(DateTimeOffset now)
    {
        while(failures.TryPeek(out var oldest) && now-oldest>=TimeSpan.FromMinutes(10))failures.Dequeue();
        if(failures.Count>=3)return null;
        var delay=TimeSpan.FromSeconds(1<<failures.Count);failures.Enqueue(now);return delay;
    }
}

// Only the latest power transition and one foreground notification are pending.
// A slow RPC therefore cannot accumulate UI requests across sleep or activation.
public sealed class LifecycleMailbox(Func<string,Task> deliver)
{
    private string? power;
    private bool active,running,stopped;
    public Task Completion{get;private set;}=Task.CompletedTask;
    public void Post(string name)
    {
        if(stopped)return;
        switch(name)
        {
            case "system_will_sleep":case "system_did_wake":power=name;active=false;break;
            case "application_did_become_active":active=true;break;
            default:throw new ArgumentException("lifecycle_event_invalid");
        }
        if(!running)Completion=DrainAsync();
    }
    private async Task DrainAsync()
    {
        running=true;
        try
        {
            while(!stopped && (power is not null || active))
            {
                var next=power??"application_did_become_active";
                if(power is not null)power=null;else active=false;
                await deliver(next);
            }
        }
        finally{running=false;}
    }
    public void Stop(){stopped=true;power=null;active=false;}
}
