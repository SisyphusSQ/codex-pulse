package preferences

func cloneSnapshot(value Snapshot) Snapshot {
	value.CodexHome = CloneCodexHome(value.CodexHome)
	value.Updates = cloneUpdatePreferences(value.Updates)
	value.DetachedHomes = append([]CodexHomePreferences(nil), value.DetachedHomes...)
	value.PendingSwitch = cloneHomeSwitchJournal(value.PendingSwitch)
	value.PendingResume = cloneHomeResumeJournal(value.PendingResume)
	value.LastSwitch = cloneHomeSwitchAudit(value.LastSwitch)
	return value
}

func cloneHomeSwitchJournal(value *HomeSwitchJournal) *HomeSwitchJournal {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneHomeResumeJournal(value *HomeResumeJournal) *HomeResumeJournal {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneHomeSwitchAudit(value *HomeSwitchAudit) *HomeSwitchAudit {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneUpdatePreferences(value UpdatePreferences) UpdatePreferences {
	value.SkippedVersion = cloneString(value.SkippedVersion)
	value.SnoozeUntilMS = cloneInt64(value.SnoozeUntilMS)
	value.LastCheckAtMS = cloneInt64(value.LastCheckAtMS)
	return value
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
