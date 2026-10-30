package salesforce

import (
	"testing"
)

func TestTrustedInstance(t *testing.T) {
	for _, instance := range []string{"http://sample.my.salesforce.com", "https://sample.my.salesforce.com.evil.test", "https://sample.my.salesforce.com:444", "https://user@sample.my.salesforce.com", "https://sample.my.salesforce.com/path", "https://evil.test"} {
		if _, ok := TrustedInstance(instance); ok {
			t.Errorf("accepted untrusted instance %q", instance)
		}
	}
}
