package store

func cloneLightString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
