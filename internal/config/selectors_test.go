package config

import "testing"

func TestObjectSelectors(t *testing.T) {
	for _, tc := range []struct {
		selector, name string
		valid, match   bool
	}{
		{"AC_*", "AC_", true, true},
		{"AC_*", "ac_One__c", true, false},
		{"*__c", "AC_One__c", true, true},
		{"*__c", "Thing__mdt", true, false},
		{"*", "Account", true, true},
		{"A*C*__c", "AC__c", true, true},
		{"Account", "Account", true, true},
		{"Account", "Contact", true, false},
		{"AC_?", "AC_A", false, false},
		{"[A]*", "Account", false, false},
		{"../*", "Account", false, false},
		{"", "Account", false, false},
		{"1*", "Account", false, false},
	} {
		t.Run(tc.selector+"/"+tc.name, func(t *testing.T) {
			if got := ValidObjectSelector(tc.selector); got != tc.valid {
				t.Fatalf("valid=%v", got)
			}
			if tc.valid && MatchesObject([]string{tc.selector}, tc.name) != tc.match {
				t.Fatal("wrong match")
			}
		})
	}
}
