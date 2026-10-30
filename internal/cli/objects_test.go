package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sf-typings/internal/auth"
)

func TestRESTFailureDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, path, want string
		status           int
	}{
		{"versions", "/services/data/", "REST version lookup: REST HTTP 401", http.StatusUnauthorized},
		{"describe", "/services/data/v60.0/sobjects/Account/describe", "REST Describe for Account: REST HTTP 403", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tc.path {
					w.WriteHeader(tc.status)
					return
				}
				if r.URL.Path == "/services/data/" {
					io.WriteString(w, `[{"version":"60.0"}]`)
					return
				}
				t.Errorf("unexpected path %s", r.URL.Path)
			}))
			defer server.Close()
			transport := server.Client().Transport
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				clone := r.Clone(r.Context())
				clone.URL.Host = strings.TrimPrefix(server.URL, "https://")
				return transport.RoundTrip(clone)
			})}
			fallbackCalls := 0
			_, err := objectOutputs(context.Background(), "dev", []string{"Account"}, false,
				func(context.Context, string) (auth.Credentials, error) {
					return auth.Credentials{"private-token", "https://sample.my.salesforce.com"}, nil
				},
				func(context.Context, string) (auth.Credentials, error) {
					fallbackCalls++
					return auth.Credentials{Token: "renewed-token"}, nil
				}, client)
			wantFallbacks := 0
			if tc.status == http.StatusUnauthorized {
				wantFallbacks = 1
			}
			if err == nil || err.Error() != tc.want || fallbackCalls != wantFallbacks || strings.Contains(err.Error(), "private-token") || strings.Contains(err.Error(), "renewed-token") {
				t.Fatalf("diagnostic=%v want=%q fallback=%d", err, tc.want, fallbackCalls)
			}
		})
	}
}

func TestRESTDescribe(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-token" {
			t.Error("missing bearer token")
		}
		switch r.URL.Path {
		case "/services/data/":
			io.WriteString(w, `[{"version":"10.9"},{"version":"10.10"}]`)
		case "/services/data/v10.10/sobjects/Account/describe":
			io.WriteString(w, `{"name":"Account","fields":[{"name":"Name","type":"string","nillable":false}]}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	transport := server.Client().Transport
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		clone.URL.Host = strings.TrimPrefix(server.URL, "https://")
		return transport.RoundTrip(clone)
	})}
	result, err := objectOutputs(context.Background(), "dev", []string{"Account"}, true,
		func(context.Context, string) (auth.Credentials, error) {
			return auth.Credentials{"private-token", "https://sample.my.salesforce.com"}, nil
		},
		func(context.Context, string) (auth.Credentials, error) {
			t.Error("unexpected fallback")
			return auth.Credentials{}, fmt.Errorf("fallback")
		}, client)
	if err != nil || len(result) != 2 || !strings.Contains(string(result[0].Data), "Name?: string") {
		t.Fatalf("result=%v err=%v", result, err)
	}
	fallbackCalled := false
	result, err = objectOutputs(context.Background(), "dev", []string{"Account"}, false,
		func(context.Context, string) (auth.Credentials, error) {
			return auth.Credentials{Instance: "https://sample.my.salesforce.com"}, nil
		},
		func(context.Context, string) (auth.Credentials, error) {
			fallbackCalled = true
			return auth.Credentials{"private-token", "https://sample.my.salesforce.com"}, nil
		}, client)
	if err != nil || !fallbackCalled || len(result) != 1 {
		t.Fatalf("fallback=%v result=%v err=%v", fallbackCalled, result, err)
	}
}

func TestRESTSecurity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://evil.test/secret", http.StatusFound)
		}},
		{"oversize", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, strings.Repeat("x", (2<<20)+1)) }},
		{"invalid version", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `[{"version":"latest"}]`) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(tc.handler)
			defer server.Close()
			transport := server.Client().Transport
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == "evil.test" {
					t.Error("redirect sent bearer to foreign host")
				}
				clone := r.Clone(r.Context())
				clone.URL.Host = strings.TrimPrefix(server.URL, "https://")
				return transport.RoundTrip(clone)
			})}
			_, err := objectOutputs(context.Background(), "dev", []string{"Account"}, false,
				func(context.Context, string) (auth.Credentials, error) {
					return auth.Credentials{"private-token", "https://sample.my.salesforce.com"}, nil
				},
				func(context.Context, string) (auth.Credentials, error) {
					t.Error("unexpected fallback")
					return auth.Credentials{}, nil
				}, client)
			if err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestRESTFallbackAndRejection(t *testing.T) {
	calls := 0
	_, err := objectOutputs(context.Background(), "dev", []string{"Account"}, false,
		func(context.Context, string) (auth.Credentials, error) {
			return auth.Credentials{Instance: "https://evil.example"}, nil
		},
		func(context.Context, string) (auth.Credentials, error) {
			calls++
			return auth.Credentials{"secret-token", "https://evil.example"}, nil
		}, nil)
	if calls != 0 || err == nil || err.Error() != "untrusted org instance" || strings.Contains(err.Error(), "secret") {
		t.Fatalf("fallback=%d err=%v", calls, err)
	}
}

func TestREST401FallbackRunWithDeps(t *testing.T) {
	for _, tc := range []struct {
		name, stage, fallbackURL, fallbackStatus, wantError string
		initialStatus                                       int
		initialCLI                                          bool
		wantFallback                                        int
		wantCode                                            int
	}{
		{"describe_recovers", "describe", "https://fresh.my.salesforce.com", "", "", 401, false, 1, 0},
		{"versions_recovers", "versions", "https://fresh.my.salesforce.com", "", "", 401, false, 1, 0},
		{"second_401", "describe", "https://fresh.my.salesforce.com", "401", "sf-typings: REST Describe for Account: REST HTTP 401\n", 401, false, 1, 1},
		{"untrusted_fallback_ignored", "describe", "https://evil.example", "", "", 401, false, 1, 0},
		{"fallback_error_redacted", "describe", "https://fresh.my.salesforce.com", "error", "sf-typings: org token unavailable\n", 401, false, 1, 1},
		{"non_401", "describe", "https://fresh.my.salesforce.com", "", "sf-typings: REST Describe for Account: REST HTTP 403\n", 403, false, 0, 1},
		{"versions_non_401", "versions", "https://fresh.my.salesforce.com", "", "sf-typings: REST version lookup: REST HTTP 403\n", 403, false, 0, 1},
		{"already_CLI", "describe", "https://fresh.my.salesforce.com", "", "sf-typings: REST Describe for Account: REST HTTP 401\n", 401, true, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			cfg := filepath.Join(root, "config.json")
			if err := os.WriteFile(cfg, []byte(`{"orgAlias":"dev","objects":["Account"]}`), 0600); err != nil {
				t.Fatal(err)
			}
			prior := filepath.Join(root, "output", "Account.d.ts")
			if err := os.MkdirAll(filepath.Dir(prior), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(prior, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			var requests []string
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				token := r.Header.Get("Authorization")
				requests = append(requests, token+" "+r.URL.Path)
				if token != "Bearer stale-token" && token != "Bearer fresh-token" {
					t.Error("unexpected bearer")
				}
				if token == "Bearer stale-token" && ((tc.stage == "versions" && r.URL.Path == "/services/data/") || (tc.stage == "describe" && strings.HasSuffix(r.URL.Path, "/describe"))) {
					w.WriteHeader(tc.initialStatus)
					return
				}
				if token == "Bearer fresh-token" && tc.fallbackStatus == "401" && strings.HasSuffix(r.URL.Path, "/describe") {
					w.WriteHeader(401)
					return
				}
				switch r.URL.Path {
				case "/services/data/":
					io.WriteString(w, `[{"version":"60.0"}]`)
				case "/services/data/v60.0/sobjects/Account/describe":
					io.WriteString(w, `{"name":"Account","fields":[{"name":"Name","type":"string","nillable":false}]}`)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			}))
			defer server.Close()
			transport := server.Client().Transport
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "old.my.salesforce.com" {
					t.Errorf("unexpected REST host %q", r.URL.Host)
				}
				clone := r.Clone(r.Context())
				clone.URL.Host = strings.TrimPrefix(server.URL, "https://")
				return transport.RoundTrip(clone)
			})}
			fallbackCalls := 0
			local := func(context.Context, string) (auth.Credentials, error) {
				if tc.initialCLI {
					return auth.Credentials{Instance: "https://old.my.salesforce.com"}, nil
				}
				return auth.Credentials{"stale-token", "https://old.my.salesforce.com"}, nil
			}
			fallback := func(context.Context, string) (auth.Credentials, error) {
				fallbackCalls++
				if tc.fallbackStatus == "error" {
					return auth.Credentials{}, fmt.Errorf("raw CLI secret")
				}
				token := "fresh-token"
				if tc.initialCLI {
					token = "stale-token"
				}
				return auth.Credentials{token, tc.fallbackURL}, nil
			}
			var out, errs strings.Builder
			code := runWithDeps([]string{"-config", cfg, "-out", filepath.Dir(prior)}, &out, &errs, local, fallback, client)
			if fallbackCalls != tc.wantFallback || code != tc.wantCode || errs.String() != tc.wantError {
				t.Fatalf("fallback=%d exit=%d stdout=%q stderr=%q requests=%v", fallbackCalls, code, out.String(), errs.String(), requests)
			}
			if tc.wantCode == 0 {
				if out.String() != "Generated 1 object(s).\n" {
					t.Fatalf("stdout=%q", out.String())
				}
				data, err := os.ReadFile(prior)
				if err != nil || !strings.Contains(string(data), "Name?: string;") {
					t.Fatalf("output=%q err=%v", data, err)
				}
			} else {
				if out.String() != "" {
					t.Fatalf("stdout=%q", out.String())
				}
				assertUnchanged(t, prior)
			}
			if tc.wantFallback == 1 && !tc.initialCLI && tc.fallbackStatus != "error" && tc.fallbackURL == "https://fresh.my.salesforce.com" {
				if len(requests) < 2 || !strings.HasPrefix(requests[len(requests)-1], "Bearer fresh-token ") {
					t.Fatalf("stale token reused: %v", requests)
				}
			}
			if tc.name == "untrusted_fallback_ignored" && (len(requests) != 4 || !strings.HasPrefix(requests[3], "Bearer fresh-token ")) {
				t.Fatalf("fallback host/token mismatch: %v", requests)
			}
			if tc.name == "second_401" && len(requests) != 4 {
				t.Fatalf("more than one REST retry: %v", requests)
			}
		})
	}
}

func TestMissingLocalURLRESTWithDisplayOnly(t *testing.T) {
	root, _, args := fixture(t, map[string]any{"orgAlias": "dev", "objects": []string{"Account"}}, "")
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SF_TYPINGS_MOCK", "1")
	t.Setenv("SF_TYPINGS_RESPONSE", `{"status":0,"result":{"accessToken":"show-secret"}}`)
	t.Setenv("SF_TYPINGS_LOG", filepath.Join(root, "invoked"))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer show-secret" {
			t.Errorf("wrong token source: %q", r.Header.Get("Authorization"))
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
			t.Errorf("untrusted host %q", r.URL.Host)
		}
		clone := r.Clone(r.Context())
		clone.URL.Host = strings.TrimPrefix(server.URL, "https://")
		return transport.RoundTrip(clone)
	})}
	var stdout, stderr strings.Builder
	code := runWithDeps(args, &stdout, &stderr,
		func(context.Context, string) (auth.Credentials, error) {
			return auth.Credentials{}, fmt.Errorf("local missing")
		}, auth.Token, client)
	if code != 0 || stderr.String() != "" || stdout.String() != "Generated 1 object(s).\n" {
		t.Fatalf("stdout=%q stderr=%q exit=%d", stdout.String(), stderr.String(), code)
	}
	if _, err := os.Stat(filepath.Join(root, "generated", "Account.d.ts")); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(filepath.Join(root, "invoked"))
	if err != nil || string(log) != "org display --target-org dev --json\norg auth show-access-token -o dev --no-prompt --json\n" {
		t.Fatalf("CLI calls=%q err=%v", log, err)
	}
}

func TestRESTRunWithDeps(t *testing.T) {
	for _, tc := range []struct {
		name      string
		objects   []string
		invalid   bool
		wantError string
	}{
		{"success", []string{"Account"}, false, ""},
		{"invalid_describe", []string{"Account"}, true, "sf-typings: invalid describe for Account\n"},
		{"second_object_failure", []string{"Account", "Contact"}, true, "sf-typings: invalid describe for Contact\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			cfg := filepath.Join(root, "config.json")
			data, _ := json.Marshal(map[string]any{"orgAlias": "dev", "objects": tc.objects})
			if err := os.WriteFile(cfg, data, 0600); err != nil {
				t.Fatal(err)
			}
			outDir := filepath.Join(root, "generated")
			if err := os.Mkdir(outDir, 0700); err != nil {
				t.Fatal(err)
			}
			prior := filepath.Join(outDir, "Account.d.ts")
			if tc.invalid {
				if err := os.WriteFile(prior, []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer private-token" {
					t.Error("missing bearer")
				}
				switch r.URL.Path {
				case "/services/data/":
					io.WriteString(w, `[{"version":"60.0"}]`)
				case "/services/data/v60.0/sobjects/Account/describe":
					if tc.name == "invalid_describe" {
						io.WriteString(w, `{"name":"Wrong","fields":[]}`)
					} else {
						io.WriteString(w, `{"name":"Account","fields":[{"name":"Name","type":"string","nillable":false}],"extra":{"kept":true}}`)
					}
				case "/services/data/v60.0/sobjects/Contact/describe":
					io.WriteString(w, `{"name":"Contact","fields":[{"name":"Broken","type":"string"}]}`)
				default:
					t.Errorf("unexpected REST path %s", r.URL.Path)
				}
			}))
			defer server.Close()
			transport := server.Client().Transport
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				clone := r.Clone(r.Context())
				clone.URL.Host = strings.TrimPrefix(server.URL, "https://")
				return transport.RoundTrip(clone)
			})}
			var out, errs strings.Builder
			status := runWithDeps([]string{"-config", cfg, "-out", outDir, "-snapshots"}, &out, &errs,
				func(context.Context, string) (auth.Credentials, error) {
					return auth.Credentials{"private-token", "https://sample.my.salesforce.com"}, nil
				},
				func(context.Context, string) (auth.Credentials, error) {
					t.Error("unexpected CLI fallback")
					return auth.Credentials{}, fmt.Errorf("fallback")
				}, client)
			if tc.invalid {
				if out.String() != "" || errs.String() != tc.wantError || status != 1 {
					t.Fatalf("stdout=%q stderr=%q exit=%d", out.String(), errs.String(), status)
				}
				assertUnchanged(t, prior)
				if _, err := os.Stat(filepath.Join(outDir, "Account.describe.json")); !os.IsNotExist(err) {
					t.Fatalf("unexpected snapshot: %v", err)
				}
			} else {
				if out.String() != "Generated 1 object(s).\n" || errs.String() != "" || status != 0 {
					t.Fatalf("stdout=%q stderr=%q exit=%d", out.String(), errs.String(), status)
				}
				decl, err := os.ReadFile(prior)
				if err != nil || !strings.Contains(string(decl), "Name?: string;") {
					t.Fatalf("declaration=%q err=%v", decl, err)
				}
				snapshot, err := os.ReadFile(filepath.Join(outDir, "Account.describe.json"))
				if err != nil || !strings.Contains(string(snapshot), `"extra"`) {
					t.Fatalf("snapshot=%q err=%v", snapshot, err)
				}
			}
		})
	}
}
