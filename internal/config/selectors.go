package config

import (
	"regexp"
	"strings"
)

// ValidObjectSelector permits API names and '*' matching zero or more characters.
// Other glob syntax, whitespace, and path separators are deliberately rejected.
func ValidObjectSelector(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if c != '*' && c != '_' && !(c >= 'A' && c <= 'Z') && !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') {
			return false
		}
	}
	if strings.Contains(value, "*") {
		return value[0] == '*' || value[0] >= 'A' && value[0] <= 'Z' || value[0] >= 'a' && value[0] <= 'z'
	}
	return apiName.MatchString(value)
}

// MatchesObject matches API names case-sensitively; '*' also matches an empty span.
func MatchesObject(selectors []string, name string) bool {
	for _, selector := range selectors {
		pattern := "^" + strings.ReplaceAll(selector, "*", ".*") + "$"
		// Selectors have already passed validation; no other regexp syntax is allowed.
		if matched, _ := regexp.MatchString(pattern, name); matched {
			return true
		}
	}
	return false
}
