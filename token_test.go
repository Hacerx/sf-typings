package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mockTokenCLI(t *testing.T, response string) string {
	t.Helper()
	root, _, _ := fixture(t, map[string]any{"apexSources": []string{}}, "")
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
			creds, err := cliCredentials(context.Background(), "dev")
			if tc.valid {
				if err != nil || creds.token != "private-token" || creds.instance != "" {
					t.Fatalf("credentials invalid: token present=%t instance=%q err=%v", creds.token != "", creds.instance, err)
				}
			} else if err == nil || err.Error() != "org token unavailable" || creds.token != "" {
				t.Fatalf("unredacted failure: %v", err)
			}
			assertCalls(t, root, tokenCall)
		})
	}
}

func TestTokenFallbackRESTAndPreservation(t *testing.T) {
	root := mockTokenCLI(t, `{"status":0,"result":{"accessToken":"private-token"}}`)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-token" {
			t.Errorf("incorrect bearer")
		}
		switch r.URL.Path {
		case "/services/data/":
			io.WriteString(w, `[{"version":"60.0"}]`)
		case "/services/data/v60.0/sobjects/Account/describe":
			io.WriteString(w, `{"name":"Account","fields":[]}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	transport := server.Client().Transport
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "sample.my.salesforce.com" {
			t.Errorf("unexpected host %q", r.URL.Host)
		}
		copy := r.Clone(r.Context())
		copy.URL.Host = strings.TrimPrefix(server.URL, "https://")
		return transport.RoundTrip(copy)
	})}
	for _, tc := range []struct {
		name      string
		local     credentials
		localErr  error
		wantCalls string
	}{
		{"local_url", credentials{instance: "https://sample.my.salesforce.com"}, nil, "org auth show-access-token -o dev --no-prompt --json\n"},
		{"display_url_only", credentials{token: "discard-me"}, fmt.Errorf("local missing"), "org display --target-org dev --json\norg auth show-access-token -o dev --no-prompt --json\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := filepath.Join(root, "invoked")
			// Separate call logs per case without deleting existing files.
			t.Setenv("SF_TYPINGS_LOG", filepath.Join(t.TempDir(), "invoked"))
			log = os.Getenv("SF_TYPINGS_LOG")
			files, err := objectOutputs(context.Background(), "dev", []string{"Account"}, false, func(context.Context, string) (credentials, error) { return tc.local, tc.localErr }, cliCredentials, client)
			if err != nil || len(files) != 1 {
				t.Fatalf("outputs=%d err=%v", len(files), err)
			}
			data, err := os.ReadFile(log)
			if err != nil || string(data) != tc.wantCalls {
				t.Fatalf("CLI calls=%q err=%v", data, err)
			}
		})
	}
}

func TestDisplayHostValidatedBeforeToken(t *testing.T) {
	root := mockTokenCLI(t, "__display_untrusted__")
	_, err := objectOutputs(context.Background(), "dev", []string{"Account"}, false, func(context.Context, string) (credentials, error) { return credentials{}, fmt.Errorf("local missing") }, cliCredentials, nil)
	if err == nil || err.Error() != "untrusted org instance" {
		t.Fatalf("error=%v", err)
	}
	assertCalls(t, root, "org display --target-org dev --json\n")
}

func TestTokenFailurePreservesPriorOutput(t *testing.T) {
	root, binary, args := fixture(t, map[string]any{"orgAlias": "dev", "objects": []string{"Account"}}, "")
	prior := filepath.Join(root, "generated", "Account.d.ts")
	if err := os.MkdirAll(filepath.Dir(prior), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prior, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	out, errs, code := runCLI(t, root, binary, "__fail__", args...)
	if out != "" || errs != "sf-typings: org token unavailable\n" || code != 1 {
		t.Fatalf("stdout=%q stderr=%q exit=%d", out, errs, code)
	}
	assertUnchanged(t, prior)
	assertCalls(t, root, "org display --target-org dev --json\norg auth show-access-token -o dev --no-prompt --json\n")
}
