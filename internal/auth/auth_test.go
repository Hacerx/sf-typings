package auth

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sf-typings/internal/testutil"
)

func mockTokenCLI(t *testing.T, response string) string {
	t.Helper()
	root := t.TempDir()
	testutil.MockSF(t, root)
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SF_TYPINGS_MOCK", "1")
	t.Setenv("SF_TYPINGS_RESPONSE", response)
	t.Setenv("SF_TYPINGS_LOG", filepath.Join(root, "invoked"))
	return root
}

func assertCalls(t *testing.T, root, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "invoked"))
	if err != nil || string(data) != want {
		t.Fatalf("CLI calls=%q err=%v want=%q", data, err, want)
	}
}

func TestCLIRefreshToken(t *testing.T) {
	root := t.TempDir()
	testutil.MockSF(t, root)
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SF_TYPINGS_MOCK", "1")
	t.Setenv("SF_TYPINGS_LOG", filepath.Join(root, "invoked"))
	for i, tc := range []struct {
		name, response string
		valid          bool
	}{
		{"token_only", `{"status":0,"result":{"accessToken":"fresh-secret"}}`, true},
		{"missing_token", `{"status":0,"result":{"instanceUrl":"https://evil.example"}}`, false},
		{"bad_status", `{"status":1,"result":{"accessToken":"fresh-secret"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SF_TYPINGS_RESPONSE", tc.response)
			creds, err := Token(context.Background(), "dev")
			if tc.valid {
				if err != nil || creds.Token != "fresh-secret" || creds.Instance != "" {
					t.Fatalf("token_only=%v err=%v", creds.Token == "fresh-secret", err)
				}
			} else {
				if err == nil || creds.Token != "" || strings.Contains(err.Error(), "fresh-secret") {
					t.Fatalf("token leaked or invalid response accepted: %v", err)
				}
			}
			invoked, err := os.ReadFile(filepath.Join(root, "invoked"))
			if err != nil || string(invoked) != strings.Repeat("org auth show-access-token -o dev --no-prompt --json\n", i+1) {
				t.Fatalf("invocation=%q err=%v", invoked, err)
			}
		})
	}
}

func TestTokenCLIFallback(t *testing.T) {
	const tokenCall = "org auth show-access-token -o dev --no-prompt --json\n"
	for _, tc := range []struct {
		name, response string
		valid          bool
	}{
		{"token_only", `{"status":0,"result":{"accessToken":"private-token","instanceUrl":"https://evil.example"}}`, true},
		{"invalid_json", `{"status":0,"result":{"accessToken":"private-token"`, false},
		{"no_token", `{"status":0,"result":{"instanceUrl":"https://evil.example"}}`, false},
		{"bad_status", `{"status":1,"result":{"accessToken":"private-token"}}`, false},
		{"process_failure", "__fail__", false},
		{"capped_stdout", strings.Repeat("x", (1<<20)+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := mockTokenCLI(t, tc.response)
			creds, err := Token(context.Background(), "dev")
			if tc.valid {
				if err != nil || creds.Token != "private-token" || creds.Instance != "" {
					t.Fatalf("credentials invalid: token present=%t instance=%q err=%v", creds.Token != "", creds.Instance, err)
				}
			} else if err == nil || err.Error() != "org token unavailable" || creds.Token != "" {
				t.Fatalf("unredacted failure: %v", err)
			}
			assertCalls(t, root, tokenCall)
		})
	}
}

func TestMain(m *testing.M) { testutil.MockMain(m) }
