package subscriptionaccounts

import (
	"sort"
	"strings"
)

func Project(records Records, evaluatedAtMS int64, timeZone string) (Snapshot, error) {
	if err := ParseEvaluationTime(evaluatedAtMS); err != nil {
		return Snapshot{}, err
	}
	if _, err := LoadTimeZone(timeZone); err != nil {
		return Snapshot{}, err
	}
	linkedManual := make(map[string]Link, len(records.Links))
	linkedDetected := make(map[string]Link, len(records.Links))
	manualByID := make(map[string]ManualEntry, len(records.Manual))
	for _, entry := range records.Manual {
		manualByID[entry.ManualEntryID] = entry
	}
	for _, link := range records.Links {
		linkedDetected[link.AccountScope] = link
		linkedManual[link.ManualEntryID] = link
	}
	accounts := make([]Account, 0, len(records.Detected)+len(records.Manual))
	for _, detected := range records.Detected {
		var supplement *ManualEntry
		var link *Link
		if current, ok := linkedDetected[detected.AccountScope]; ok {
			copiedLink := current
			link = &copiedLink
			if entry, ok := manualByID[current.ManualEntryID]; ok {
				copiedEntry := entry
				supplement = &copiedEntry
			}
		}
		account, err := projectDetected(records.Binding, detected, supplement, link, evaluatedAtMS, timeZone)
		if err != nil {
			return Snapshot{}, err
		}
		accounts = append(accounts, account)
	}
	for _, entry := range records.Manual {
		if _, linked := linkedManual[entry.ManualEntryID]; linked {
			continue
		}
		account, err := projectStandaloneManual(entry, evaluatedAtMS, timeZone)
		if err != nil {
			return Snapshot{}, err
		}
		accounts = append(accounts, account)
	}
	sortAccounts(accounts)
	return Snapshot{
		Version:                 ContractVersion,
		EvaluatedAtMS:           evaluatedAtMS,
		TimeZone:                timeZone,
		AutomaticDateCapability: AutomaticDateCapabilityManualOnly,
		Accounts:                accounts,
		LinkCandidates:          linkCandidates(records.Detected, records.Manual, linkedDetected, linkedManual),
	}, nil
}

func CurrentAccount(
	records Records,
	fence AccountFence,
	evaluatedAtMS int64,
	timeZone string,
) (*Account, error) {
	if records.Binding.State != BindingConfirmed ||
		records.Binding.AccountScope == nil ||
		*records.Binding.AccountScope != fence.AccountScope ||
		records.Binding.BindingGeneration != fence.BindingGeneration ||
		fence.BindingGeneration <= 0 {
		return nil, ErrAccountBindingChanged
	}
	snapshot, err := Project(records, evaluatedAtMS, timeZone)
	if err != nil {
		return nil, err
	}
	for index := range snapshot.Accounts {
		if snapshot.Accounts[index].Current && snapshot.Accounts[index].accountScope == fence.AccountScope {
			account := snapshot.Accounts[index]
			return &account, nil
		}
	}
	return nil, ErrAccountBindingChanged
}

func projectDetected(
	binding Binding,
	detected DetectedAccount,
	manual *ManualEntry,
	link *Link,
	evaluatedAtMS int64,
	timeZone string,
) (Account, error) {
	automatic := AutomaticPlanFact{State: detected.AutomaticPlanState, Plan: detected.AutomaticPlan}
	var manualPlan *Plan
	if manual != nil {
		manualPlan = manual.ManualPlan
	}
	resolved, err := ResolvePlan(automatic, manualPlan)
	if err != nil {
		return Account{}, err
	}
	var date DateStatus
	if manual != nil {
		date, err = ResolveDateStatus(manual.MembershipDate, manual.DateKind, evaluatedAtMS, timeZone)
		if err != nil {
			return Account{}, err
		}
	} else {
		date = DateStatus{Source: ValueSourceUnavailable, State: DateStateUnavailable}
	}
	account := Account{
		AccountID:                 detected.DetectedAccountID,
		DetectedAccountID:         pointerTo(detected.DetectedAccountID),
		DetectedEmail:             clonePointer(detected.DetectedEmail),
		Current:                   isCurrent(binding, detected.AccountScope),
		Detected:                  true,
		HasManual:                 manual != nil,
		Linked:                    link != nil,
		DetectedEmailObservedAtMS: clonePointer(detected.DetectedEmailObservedAtMS),
		AutomaticPlan:             clonePointer(detected.AutomaticPlan),
		AutomaticPlanState:        detected.AutomaticPlanState,
		AutomaticPlanObservedAtMS: clonePointer(detected.AutomaticPlanObservedAtMS),
		ResolvedPlan:              resolved.Plan,
		ResolvedPlanSource:        resolved.Source,
		DateSource:                date.Source,
		DateState:                 date.State,
		DayDelta:                  date.Delta,
		DetectedRevision:          pointerTo(detected.Revision),
		accountScope:              detected.AccountScope,
	}
	if detected.AutomaticPlanState != AutomaticPlanUnavailable {
		source := AutomaticSourceAccountSandwich
		account.AutomaticPlanSource = &source
	}
	if manual != nil {
		account.ManualEntryID = pointerTo(manual.ManualEntryID)
		account.Alias = clonePointer(manual.Alias)
		account.ManualEmail = clonePointer(manual.Email)
		account.ManualPlan = clonePointer(manual.ManualPlan)
		account.MembershipDate = date.Date
		account.DateKind = date.Kind
		account.ManualRevision = pointerTo(manual.Revision)
	}
	if link != nil {
		account.LinkRevision = pointerTo(link.Revision)
	}
	account.DisplayEmail = firstNonEmpty(account.ManualEmail, account.DetectedEmail)
	return account, nil
}

func projectStandaloneManual(entry ManualEntry, evaluatedAtMS int64, timeZone string) (Account, error) {
	resolved, err := ResolvePlan(AutomaticPlanFact{State: AutomaticPlanUnavailable}, entry.ManualPlan)
	if err != nil {
		return Account{}, err
	}
	date, err := ResolveDateStatus(entry.MembershipDate, entry.DateKind, evaluatedAtMS, timeZone)
	if err != nil {
		return Account{}, err
	}
	account := Account{
		AccountID:          entry.ManualEntryID,
		ManualEntryID:      pointerTo(entry.ManualEntryID),
		Alias:              clonePointer(entry.Alias),
		DisplayEmail:       clonePointer(entry.Email),
		ManualEmail:        clonePointer(entry.Email),
		HasManual:          true,
		AutomaticPlanState: AutomaticPlanUnavailable,
		ManualPlan:         clonePointer(entry.ManualPlan),
		ResolvedPlan:       resolved.Plan,
		ResolvedPlanSource: resolved.Source,
		MembershipDate:     date.Date,
		DateKind:           date.Kind,
		DateSource:         date.Source,
		DateState:          date.State,
		DayDelta:           date.Delta,
		ManualRevision:     pointerTo(entry.Revision),
	}
	return account, nil
}

func isCurrent(binding Binding, accountScope string) bool {
	return binding.State == BindingConfirmed &&
		binding.AccountScope != nil &&
		*binding.AccountScope == accountScope
}

func linkCandidates(
	detected []DetectedAccount,
	manual []ManualEntry,
	linkedDetected map[string]Link,
	linkedManual map[string]Link,
) []LinkCandidate {
	var candidates []LinkCandidate
	for _, account := range detected {
		if _, linked := linkedDetected[account.AccountScope]; linked {
			continue
		}
		if account.EmailMatchKey == nil || *account.EmailMatchKey == "" {
			continue
		}
		for _, entry := range manual {
			if _, linked := linkedManual[entry.ManualEntryID]; linked {
				continue
			}
			if entry.EmailMatchKey == nil || *entry.EmailMatchKey != *account.EmailMatchKey {
				continue
			}
			candidates = append(candidates, LinkCandidate{
				DetectedAccountID: account.DetectedAccountID,
				ManualEntryID:     entry.ManualEntryID,
				Reason:            LinkCandidateSameEmail,
			})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].DetectedAccountID != candidates[j].DetectedAccountID {
			return candidates[i].DetectedAccountID < candidates[j].DetectedAccountID
		}
		return candidates[i].ManualEntryID < candidates[j].ManualEntryID
	})
	return candidates
}

func sortAccounts(accounts []Account) {
	sort.SliceStable(accounts, func(i, j int) bool {
		left, right := accounts[i], accounts[j]
		if left.Current != right.Current {
			return left.Current
		}
		if cmp := strings.Compare(strings.ToLower(optionalText(left.Alias)), strings.ToLower(optionalText(right.Alias))); cmp != 0 {
			return cmp < 0
		}
		if cmp := strings.Compare(strings.ToLower(optionalText(left.DisplayEmail)), strings.ToLower(optionalText(right.DisplayEmail))); cmp != 0 {
			return cmp < 0
		}
		return strings.ToLower(left.AccountID) < strings.ToLower(right.AccountID)
	})
}

func firstNonEmpty(values ...*string) *string {
	for _, value := range values {
		if value != nil && *value != "" {
			return pointerTo(*value)
		}
	}
	return nil
}

func optionalText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	return pointerTo(*value)
}
