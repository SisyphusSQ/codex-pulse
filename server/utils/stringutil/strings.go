package stringutil

import "strings"

func StartWith(str string, starts ...string) (ok bool) {
	for _, start := range starts {
		ok = ok || strings.HasPrefix(str, start)
	}
	return
}

func IsAnyEmpty(ss ...string) bool {
	for _, s := range ss {
		if s == "" {
			return true
		}
	}
	return false
}
