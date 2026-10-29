using System.Globalization;
using Codexpulse.Core.V1;

namespace CodexPulse.AppSupport;

// Only explicit manual fields enter a command. Detected email/plan are separate
// read-only context, so editing an alias cannot silently override an automatic plan.
public sealed class SubscriptionDraft
{
    public CodexSubscriptionAccount? Original{get;private set;}
    public string ManualId{get;private set;}=Guid.NewGuid().ToString("D");
    public string Email{get;set;}="";
    public string Alias{get;set;}="";
    public CodexSubscriptionPlan? Plan{get;set;}
    public CodexSubscriptionDateKind? DateKind{get;set;}
    public string MembershipDate{get;set;}="";
    public bool Creating=>Original is null;
    public static SubscriptionDraft Edit(CodexSubscriptionAccount account)=>new()
    {
        Original=account.Clone(),ManualId=account.HasManualEntryId?account.ManualEntryId:Guid.NewGuid().ToString("D"),
        Email=account.HasManualEmail?account.ManualEmail:"",Alias=account.HasAlias?account.Alias:"",
        Plan=account.HasManualPlan?account.ManualPlan:null,DateKind=account.HasDateKind?account.DateKind:null,MembershipDate=account.HasMembershipDate?account.MembershipDate:""
    };
    public void Rebase(CodexSubscriptionAccount account)
    {if(Original is null || Original.AccountId!=account.AccountId)throw new ArgumentException("account_changed");Original=account.Clone();if(account.HasManualEntryId)ManualId=account.ManualEntryId;}
    public CodexSubscriptionManualFields Fields()
    {
        var fields=new CodexSubscriptionManualFields();
        if(!string.IsNullOrWhiteSpace(Email))fields.Email=Email.Trim();
        if(!string.IsNullOrWhiteSpace(Alias))fields.Alias=Alias.Trim();
        if(Plan is {} plan && plan!=CodexSubscriptionPlan.Unspecified)fields.Plan=plan;
        if(DateKind is CodexSubscriptionDateKind.NextRenewal or CodexSubscriptionDateKind.MembershipExpiry)
        {
            if(!DateOnly.TryParseExact(MembershipDate,"yyyy-MM-dd",CultureInfo.InvariantCulture,DateTimeStyles.None,out _))throw new ArgumentException("membership_date_invalid");
            fields.MembershipDate=MembershipDate;fields.DateKind=DateKind.Value;
        }
        if(Creating && !fields.HasEmail)throw new ArgumentException("standalone_email_required");
        if(Original is{Detected:true,HasManual:false} && !fields.HasEmail && !fields.HasAlias && !fields.HasPlan && !fields.HasMembershipDate)throw new ArgumentException("manual_values_empty");
        return fields;
    }
    public CreateCodexSubscriptionAccountRequest CreateRequest()=>Creating?new(){ManualEntryId=ManualId,Manual=Fields()}:throw new InvalidOperationException("existing_account");
    public UpdateCodexSubscriptionAccountRequest UpdateRequest()
    {
        var account=Original??throw new InvalidOperationException("new_account");
        var request=new UpdateCodexSubscriptionAccountRequest{AccountId=account.AccountId,Manual=Fields()};
        if(account.HasManualRevision)request.ExpectedManualRevision=account.ManualRevision;
        if(account.HasLinkRevision)request.ExpectedLinkRevision=account.LinkRevision;
        if(account.Detected && !account.HasManual)request.NewManualEntryId=ManualId;
        return request;
    }
    public static DeleteCodexSubscriptionAccountRequest DeleteRequest(CodexSubscriptionAccount account)
    {
        if(account.Current)throw new ArgumentException("current_account_cannot_delete");
        var request=new DeleteCodexSubscriptionAccountRequest{AccountId=account.AccountId};
        if(account.HasDetectedRevision)request.ExpectedDetectedRevision=account.DetectedRevision;
        if(account.HasManualRevision)request.ExpectedManualRevision=account.ManualRevision;
        if(account.HasLinkRevision)request.ExpectedLinkRevision=account.LinkRevision;
        return request;
    }
}
