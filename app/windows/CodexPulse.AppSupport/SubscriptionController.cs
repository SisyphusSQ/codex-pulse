using Codexpulse.Core.V1;
using CodexPulse.CoreClient;
using Grpc.Core;

namespace CodexPulse.AppSupport;

public enum SubscriptionAction { Idle,Running,Applied,Noop,Conflict,Unknown,Unavailable,ReadbackChanged,ReadbackConfirmed }

// A Home-scoped coordinator survives page reconstruction from account invalidation.
// It keeps drafts only in memory and confirms every command with an uncached List.
public sealed class SubscriptionController(FeatureClient client,Func<QueryContext> context,string homeGeneration):IDisposable
{
    public string HomeGeneration{get;}=homeGeneration;
    public QuerySlot<CodexSubscriptionAccountsResponse> List{get;}=new();
    public SubscriptionDraft? Draft{get;private set;}
    public SubscriptionAction Action{get;private set;}
    public string? Reason{get;private set;}
    public string? Field{get;private set;}
    public bool Busy=>Volatile.Read(ref mutating)!=0;
    public event Action? Changed;
    private int mutating;
    private bool disposed;
    public void BeginEdit(CodexSubscriptionAccount account){if(Busy || disposed)return;Draft=SubscriptionDraft.Edit(account);Action=SubscriptionAction.Idle;Reason=null;Changed?.Invoke();}
    public void BeginNew(){if(Busy || disposed)return;Draft=new();Action=SubscriptionAction.Idle;Reason=null;Changed?.Invoke();}
    public void CancelDraft(){if(Busy)return;Draft=null;Action=SubscriptionAction.Idle;Reason=null;Changed?.Invoke();}
    public bool RebaseDraft()
    {
        if(Busy || Draft?.Original is not {} old || List.State.Value?.Accounts.FirstOrDefault(account=>account.AccountId==old.AccountId) is not {} current)return false;
        Draft.Rebase(current);Action=SubscriptionAction.Idle;Reason=null;Changed?.Invoke();return true;
    }
    public async Task ReloadAsync(bool force=true)
    {
        if(disposed)return;
        var scope=context() with{Provider="codex"};
        var request=new CodexSubscriptionAccountsRequest{EvaluatedAtMs=DateTimeOffset.Now.ToUnixTimeMilliseconds(),TimeZone=scope.TimeZone};
        await List.LoadAsync((HomeGeneration,scope.Generation),token=>client.QueryAsync("ListCodexSubscriptionAccounts",request,scope,["account","settings"],
            (core,deadline,cancellation)=>core.ListCodexSubscriptionAccountsAsync(request,deadline:deadline,cancellationToken:cancellation),token,force),response=>response.Accounts.Count==0?LoadKind.Empty:LoadKind.Ready);
    }
    public async Task SaveAsync()
    {
        var draft=Draft;if(draft is null)return;
        if(draft.Creating)
        {
            var request=draft.CreateRequest();
            await ExecuteAsync((core,deadline,cancellation)=>core.CreateCodexSubscriptionAccountAsync(request,deadline:deadline,cancellationToken:cancellation),
                accounts=>accounts.Accounts.Any(account=>account.HasManualEntryId && account.ManualEntryId==request.ManualEntryId && ManualMatches(account,request.Manual)),true);
        }
        else
        {
            var request=draft.UpdateRequest();
            await ExecuteAsync((core,deadline,cancellation)=>core.UpdateCodexSubscriptionAccountAsync(request,deadline:deadline,cancellationToken:cancellation),
                accounts=>accounts.Accounts.Any(account=>account.AccountId==request.AccountId && ManualMatches(account,request.Manual)),true);
        }
    }
    public Task DeleteAsync(CodexSubscriptionAccount account)
    {
        var request=SubscriptionDraft.DeleteRequest(account);
        return ExecuteAsync((core,deadline,cancellation)=>core.DeleteCodexSubscriptionAccountAsync(request,deadline:deadline,cancellationToken:cancellation),
            accounts=>accounts.Accounts.All(item=>item.AccountId!=account.AccountId));
    }
    public Task LinkAsync(CodexSubscriptionAccount detected,CodexSubscriptionAccount manual)
    {
        if(!detected.Detected || !detected.HasDetectedAccountId || !manual.HasManualEntryId || !manual.HasManualRevision)throw new ArgumentException("link_unavailable");
        var request=new LinkCodexSubscriptionAccountRequest{DetectedAccountId=detected.DetectedAccountId,ManualEntryId=manual.ManualEntryId,ExpectedManualRevision=manual.ManualRevision};
        return ExecuteAsync((core,deadline,cancellation)=>core.LinkCodexSubscriptionAccountAsync(request,deadline:deadline,cancellationToken:cancellation),
            accounts=>accounts.Accounts.Any(account=>account.HasDetectedAccountId && account.DetectedAccountId==request.DetectedAccountId && account.Linked && account.ManualEntryId==request.ManualEntryId));
    }
    public Task UnlinkAsync(CodexSubscriptionAccount account)
    {
        if(!account.Linked || !account.HasDetectedAccountId || !account.HasManualEntryId || !account.HasManualRevision || !account.HasLinkRevision || !account.HasManualEmail || string.IsNullOrWhiteSpace(account.ManualEmail))throw new ArgumentException("manual_email_required_before_unlink");
        var request=new UnlinkCodexSubscriptionAccountRequest{DetectedAccountId=account.DetectedAccountId,ManualEntryId=account.ManualEntryId,ExpectedManualRevision=account.ManualRevision,ExpectedLinkRevision=account.LinkRevision};
        return ExecuteAsync((core,deadline,cancellation)=>core.UnlinkCodexSubscriptionAccountAsync(request,deadline:deadline,cancellationToken:cancellation),
            accounts=>accounts.Accounts.Any(item=>item.HasDetectedAccountId && item.DetectedAccountId==request.DetectedAccountId && !item.Linked) && accounts.Accounts.Any(item=>item.HasManualEntryId && item.ManualEntryId==request.ManualEntryId && !item.Linked));
    }
    public Task LinkLegacyAsync(CodexSubscriptionAccount account)
    {
        if(!account.Current || !account.Detected || !account.HasDetectedAccountId || !account.HasDetectedRevision || account.LegacyQuotaHistory?.State!=CodexLegacyQuotaHistoryState.Available)throw new ArgumentException("legacy_link_unavailable");
        var request=new LinkLegacyQuotaHistoryRequest{DetectedAccountId=account.DetectedAccountId,ExpectedDetectedRevision=account.DetectedRevision};
        return ExecuteAsync((core,deadline,cancellation)=>core.LinkLegacyQuotaHistoryAsync(request,deadline:deadline,cancellationToken:cancellation),
            accounts=>accounts.Accounts.Any(item=>item.HasDetectedAccountId && item.DetectedAccountId==request.DetectedAccountId && item.LegacyQuotaHistory?.State==CodexLegacyQuotaHistoryState.Linked));
    }
    public Task UnlinkLegacyAsync(CodexSubscriptionAccount account)
    {
        if(!account.HasDetectedAccountId || !account.HasDetectedRevision || account.LegacyQuotaHistory is not{State:CodexLegacyQuotaHistoryState.Linked,HasAssociationRevision:true} history)throw new ArgumentException("legacy_unlink_unavailable");
        var request=new UnlinkLegacyQuotaHistoryRequest{DetectedAccountId=account.DetectedAccountId,ExpectedDetectedRevision=account.DetectedRevision,ExpectedAssociationRevision=history.AssociationRevision};
        return ExecuteAsync((core,deadline,cancellation)=>core.UnlinkLegacyQuotaHistoryAsync(request,deadline:deadline,cancellationToken:cancellation),
            accounts=>accounts.Accounts.Any(item=>item.HasDetectedAccountId && item.DetectedAccountId==request.DetectedAccountId && item.LegacyQuotaHistory?.State!=CodexLegacyQuotaHistoryState.Linked));
    }
    private async Task ExecuteAsync(Func<CoreService.CoreServiceClient,DateTime,CancellationToken,AsyncUnaryCall<CodexSubscriptionMutationReceipt>> invoke,
        Func<CodexSubscriptionAccountsResponse,bool> confirm,bool closeEditor=false)
    {
        if(disposed || Interlocked.CompareExchange(ref mutating,1,0)!=0)return;
        Action=SubscriptionAction.Running;Reason=null;Field=null;Changed?.Invoke();
        try
        {
            var result=await client.MutateAsync(invoke);
            await ReloadAsync(true);
            if(disposed)return;
            var fresh=List.State.Kind is LoadKind.Ready or LoadKind.Empty;
            var receipt=result.Receipt;
            if(receipt?.Result==CodexSubscriptionMutationResult.Conflict){Action=SubscriptionAction.Conflict;Reason=receipt.HasReason?receipt.Reason:"revision_changed";}
            else if(result.Outcome!=MutationOutcome.Unknown && receipt?.Result is not(CodexSubscriptionMutationResult.Applied or CodexSubscriptionMutationResult.Noop)){Action=SubscriptionAction.Unknown;Reason="receipt_unrecognized";}
            else if(!fresh || List.State.Value is not {} accounts){Action=SubscriptionAction.Unknown;Reason="readback_unavailable";}
            else if(!confirm(accounts)){Action=result.Outcome==MutationOutcome.Unknown?SubscriptionAction.Unknown:SubscriptionAction.ReadbackChanged;Reason="readback_changed";}
            else
            {
                Action=result.Outcome==MutationOutcome.Unknown?SubscriptionAction.ReadbackConfirmed:receipt?.Result==CodexSubscriptionMutationResult.Noop?SubscriptionAction.Noop:SubscriptionAction.Applied;
                if(closeEditor)Draft=null;
            }
        }
        catch(RpcException error)
        {
            var detail=CoreConnection.ErrorDetail(error);Reason=detail?.Code??error.StatusCode.ToString();Field=detail?.HasField==true?detail.Field:null;Action=SubscriptionAction.Unavailable;
            await ReloadAsync(true);
        }
        catch(IOException){Action=SubscriptionAction.Unknown;Reason="connection";await ReloadAsync(true);}
        finally{Interlocked.Exchange(ref mutating,0);if(!disposed)Changed?.Invoke();}
    }
    private static bool ManualMatches(CodexSubscriptionAccount account,CodexSubscriptionManualFields fields)=>
        (account.HasManualEmail?account.ManualEmail:"")==(fields.HasEmail?fields.Email:"") &&
        (account.HasAlias?account.Alias:"")==(fields.HasAlias?fields.Alias:"") &&
        account.HasManualPlan==fields.HasPlan && (!fields.HasPlan || account.ManualPlan==fields.Plan) &&
        account.HasMembershipDate==fields.HasMembershipDate && (!fields.HasMembershipDate || account.MembershipDate==fields.MembershipDate) &&
        account.HasDateKind==fields.HasDateKind && (!fields.HasDateKind || account.DateKind==fields.DateKind);
    public void Dispose(){disposed=true;List.Dispose();Draft=null;Changed=null;}
}
