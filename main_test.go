package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
				func(context.Context, string) (credentials, error) {
					return credentials{"private-token", "https://sample.my.salesforce.com"}, nil
				},
				func(context.Context, string) (credentials, error) {
					fallbackCalls++
					return credentials{token: "renewed-token"}, nil
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
		func(context.Context, string) (credentials, error) {
			return credentials{"private-token", "https://sample.my.salesforce.com"}, nil
		},
		func(context.Context, string) (credentials, error) {
			t.Error("unexpected fallback")
			return credentials{}, fmt.Errorf("fallback")
		}, client)
	if err != nil || len(result) != 2 || !strings.Contains(string(result[0].data), "Name?: string") {
		t.Fatalf("result=%v err=%v", result, err)
	}
	fallbackCalled := false
	result, err = objectOutputs(context.Background(), "dev", []string{"Account"}, false,
		func(context.Context, string) (credentials, error) {
			return credentials{instance: "https://sample.my.salesforce.com"}, nil
		},
		func(context.Context, string) (credentials, error) {
			fallbackCalled = true
			return credentials{"private-token", "https://sample.my.salesforce.com"}, nil
		}, client)
	if err != nil || !fallbackCalled || len(result) != 1 {
		t.Fatalf("fallback=%v result=%v err=%v", fallbackCalled, result, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRESTSecurity(t *testing.T) {
	for _, instance := range []string{"http://sample.my.salesforce.com", "https://sample.my.salesforce.com.evil.test", "https://sample.my.salesforce.com:444", "https://user@sample.my.salesforce.com", "https://sample.my.salesforce.com/path", "https://evil.test"} {
		if _, ok := trustedInstance(instance); ok {
			t.Errorf("accepted untrusted instance %q", instance)
		}
	}
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
				func(context.Context, string) (credentials, error) {
					return credentials{"private-token", "https://sample.my.salesforce.com"}, nil
				},
				func(context.Context, string) (credentials, error) {
					t.Error("unexpected fallback")
					return credentials{}, nil
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
		func(context.Context, string) (credentials, error) {
			return credentials{instance: "https://evil.example"}, nil
		},
		func(context.Context, string) (credentials, error) {
			calls++
			return credentials{"secret-token", "https://evil.example"}, nil
		}, nil)
	if calls != 0 || err == nil || err.Error() != "untrusted org instance" || strings.Contains(err.Error(), "secret") {
		t.Fatalf("fallback=%d err=%v", calls, err)
	}
}

func TestMain(m *testing.M) {
	if os.Getenv("SF_TYPINGS_MOCK") == "1" {
		argv := strings.Join(os.Args[1:], " ")
		if argv != "org display --target-org dev --json" && argv != "org auth show-access-token -o dev --no-prompt --json" {
			fmt.Fprintln(os.Stderr, "unexpected sf arguments")
			os.Exit(3)
		}
		log, err := os.OpenFile(os.Getenv("SF_TYPINGS_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(4)
		}
		if _, err := fmt.Fprintln(log, argv); err != nil {
			os.Exit(4)
		}
		if err := log.Close(); err != nil {
			os.Exit(4)
		}
		if argv == "org display --target-org dev --json" {
			if os.Getenv("SF_TYPINGS_RESPONSE") == "__display_untrusted__" {
				fmt.Print(`{"status":0,"result":{"instanceUrl":"https://evil.example","accessToken":"display-secret"}}`)
				os.Exit(0)
			}
			fmt.Print(`{"status":0,"result":{"instanceUrl":"https://sample.my.salesforce.com","accessToken":"display-secret"}}`)
			os.Exit(0)
		}
		if os.Getenv("SF_TYPINGS_RESPONSE") == "__fail__" {
			fmt.Fprintln(os.Stderr, "mock secret")
			os.Exit(7)
		}
		fmt.Print(os.Getenv("SF_TYPINGS_RESPONSE"))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fixture(t *testing.T, config any, response string) (string, string, []string) {
	t.Helper()
	root := t.TempDir()
	b, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	if err := os.WriteFile(configPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	mock := filepath.Join(root, "sf")
	if runtime.GOOS == "windows" {
		mock += ".exe"
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mock, content, 0700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "sf-typings")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, output)
	}
	return root, binary, []string{"-config", configPath, "-out", filepath.Join(root, "generated")}
}

func runCLI(t *testing.T, root, binary, response string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(), "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"), "SF_TYPINGS_MOCK=1", "SF_TYPINGS_RESPONSE="+response, "SF_TYPINGS_LOG="+filepath.Join(root, "invoked"), "HOME="+root, "USERPROFILE="+root)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	if e, ok := err.(*exec.ExitError); ok {
		return stdout.String(), stderr.String(), e.ExitCode()
	}
	t.Fatal(err)
	return "", "", -1
}

func TestHelpIncludesApexOnly(t *testing.T) {
	root, binary, _ := fixture(t, map[string]any{"apexSources": []string{}}, "")
	out, errs, status := runCLI(t, root, binary, "", "-help")
	want := "Usage: sf-typings -config config.json -out generated [-snapshots]\nConfig: {\"orgAlias\":\"dev\",\"objects\":[\"Account\"],\"apexSources\":[\"classes\"]}\nObjects use REST Describe. If local org URL is missing, sf org display supplies only the URL; sf org auth show-access-token supplies the fallback token. A local-token HTTP 401 triggers one token-only REST retry. Apex-only runs use local .cls files. Never retrieves records.\n"
	if out != want || errs != "" || status != 0 {
		t.Fatalf("stdout=%q stderr=%q exit=%d", out, errs, status)
	}
}

func TestApexCLI(t *testing.T) {
	cases := []struct {
		name, source, want, message string
	}{
		{"annotated methods and types", `public class Orders {
  @AuraEnabled(cacheable=true) public static List<String> fetch(Id accountId, Boolean active) { return null; }
  public static String hidden() { return ''; }
  @AuraEnabled public static Custom unknowns(Map<String, Integer> data) { return null; }
}`, "// Generated by sf-typings. Do not edit.\ndeclare module '@salesforce/apex/Orders.fetch' {\n  export default function fetch(params: { accountId: string; active: boolean }): Promise<string[]>;\n}\ndeclare module '@salesforce/apex/Orders.unknowns' {\n  export default function unknowns(params: { data: unknown }): Promise<unknown>;\n}\n", ""},
		{"comment is not annotation", `public class Orders {
  // @AuraEnabled
  public static String hidden() { return ''; }
  @AuraEnabled public static void go() { }
}`, "// Generated by sf-typings. Do not edit.\ndeclare module '@salesforce/apex/Orders.go' {\n  export default function go(): Promise<void>;\n}\n", ""},
		{"only accessible static methods", `public class Orders {
  @AuraEnabled private static String privateMethod() { return ''; }
  @AuraEnabled public String instanceMethod() { return ''; }
  @AuraEnabled public static String exposed() { return ''; }
}`, "// Generated by sf-typings. Do not edit.\ndeclare module '@salesforce/apex/Orders.exposed' {\n  export default function exposed(): Promise<string>;\n}\n", ""},
		{"invalid syntax preserves output", `public class Orders { @AuraEnabled public static String broken( { }`, "", "invalid Apex source"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, binary, args := fixture(t, map[string]any{"apexSources": []string{}}, "")
			source := filepath.Join(root, "Orders.cls")
			if err := os.WriteFile(source, []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, _ := json.Marshal(map[string]any{"apexSources": []string{source}})
			if err := os.WriteFile(args[1], cfg, 0600); err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(root, "generated")
			if err := os.Mkdir(dest, 0700); err != nil {
				t.Fatal(err)
			}
			existing := filepath.Join(dest, "sentinel.d.ts")
			if err := os.WriteFile(existing, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			out, errs, status := runCLI(t, root, binary, "", args...)
			if tc.message != "" {
				if out != "" || errs != "sf-typings: "+tc.message+"\n" || status != 1 {
					t.Fatalf("stdout=%q stderr=%q exit=%d", out, errs, status)
				}
				if _, err := os.Stat(filepath.Join(dest, "Orders.d.ts")); !os.IsNotExist(err) {
					t.Fatalf("unexpected output: %v", err)
				}
			} else {
				if out != "Generated 0 object(s), 1 Apex class(es).\n" || errs != "" || status != 0 {
					t.Fatalf("stdout=%q stderr=%q exit=%d", out, errs, status)
				}
				got, err := os.ReadFile(filepath.Join(dest, "Orders.d.ts"))
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != tc.want {
					t.Fatalf("declaration=%q want=%q", got, tc.want)
				}
			}
			got, err := os.ReadFile(existing)
			if err != nil || string(got) != "original" {
				t.Fatalf("sentinel=%q err=%v", got, err)
			}
			if _, err := os.Stat(filepath.Join(root, "invoked")); !os.IsNotExist(err) {
				t.Fatalf("sf invoked: %v", err)
			}
		})
	}
}

func TestApexDirectoryCLI(t *testing.T) {
	root, binary, args := fixture(t, map[string]any{"apexSources": []string{}}, "")
	dir := filepath.Join(root, "classes", "nested")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Orders.cls"), []byte(`public class Orders { @AuraEnabled public static Integer count() { return 1; } }`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("ignored"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(map[string]any{"apexSources": []string{filepath.Join(root, "classes")}})
	if err := os.WriteFile(args[1], cfg, 0600); err != nil {
		t.Fatal(err)
	}
	out, errs, status := runCLI(t, root, binary, "", args...)
	if out != "Generated 0 object(s), 1 Apex class(es).\n" || errs != "" || status != 0 {
		t.Fatalf("stdout=%q stderr=%q exit=%d", out, errs, status)
	}
	got, err := os.ReadFile(filepath.Join(root, "generated", "Orders.d.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "function count(): Promise<number>;") {
		t.Fatalf("declaration=%q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "invoked")); !os.IsNotExist(err) {
		t.Fatalf("sf invoked: %v", err)
	}
}

func TestApexFailureCLI(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sources []string
		source  string
		message string
	}{
		{"missing file", []string{"missing.cls"}, "", "cannot read Apex source"},
		{"duplicate class", []string{"Orders.cls", "Orders.cls"}, "public class Orders { @AuraEnabled public static String fetch() { return ''; } }", "duplicate Apex class"},
		{"filename mismatch", []string{"Orders.cls"}, "public class Other { @AuraEnabled public static String fetch() { return ''; } }", "invalid Apex source"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, binary, args := fixture(t, map[string]any{"apexSources": []string{}}, "")
			var sources []string
			for _, name := range tc.sources {
				path := filepath.Join(root, name)
				if tc.source != "" {
					if err := os.WriteFile(path, []byte(tc.source), 0600); err != nil {
						t.Fatal(err)
					}
				}
				sources = append(sources, path)
			}
			cfg, _ := json.Marshal(map[string]any{"apexSources": sources})
			if err := os.WriteFile(args[1], cfg, 0600); err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(root, "generated")
			if err := os.Mkdir(dest, 0700); err != nil {
				t.Fatal(err)
			}
			original := filepath.Join(dest, "Account.d.ts")
			if err := os.WriteFile(original, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			response := `{"status":0,"result":{"name":"Account","fields":[]}}`
			out, errs, status := runCLI(t, root, binary, response, args...)
			if out != "" || errs != "sf-typings: "+tc.message+"\n" || status != 1 {
				t.Fatalf("stdout=%q stderr=%q exit=%d", out, errs, status)
			}
			got, err := os.ReadFile(original)
			if err != nil || string(got) != "original" {
				t.Fatalf("existing=%q err=%v", got, err)
			}
			if _, err := os.Stat(filepath.Join(dest, "Orders.d.ts")); !os.IsNotExist(err) {
				t.Fatalf("unexpected Apex output: %v", err)
			}
		})
	}
}

func TestPublicCLI(t *testing.T) {
	t.Run("invalid_config_preserves_prior_output_without_sf", func(t *testing.T) {
		root, binary, args := fixture(t, map[string]any{"orgAlias": "dev", "objects": []string{"../Account"}}, "")
		prior := filepath.Join(root, "generated", "Account.d.ts")
		if err := os.MkdirAll(filepath.Dir(prior), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(prior, []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
		out, errs, status := runCLI(t, root, binary, "__fail__", args...)
		if out != "" || errs != "sf-typings: invalid object API name\n" || status != 1 {
			t.Fatalf("stdout=%q stderr=%q exit=%d", out, errs, status)
		}
		assertUnchanged(t, prior)
		if _, err := os.Stat(filepath.Join(root, "invoked")); !os.IsNotExist(err) {
			t.Fatalf("sf invoked: %v", err)
		}
	})
	t.Run("fallback_failure_preserves_prior_output", func(t *testing.T) {
		root, binary, args := fixture(t, map[string]any{"orgAlias": "dev", "objects": []string{"Account"}}, "")
		prior := filepath.Join(root, "generated", "Account.d.ts")
		if err := os.MkdirAll(filepath.Dir(prior), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(prior, []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
		out, errs, status := runCLI(t, root, binary, "__fail__", args...)
		if out != "" || errs != "sf-typings: org token unavailable\n" || status != 1 {
			t.Fatalf("stdout=%q stderr=%q exit=%d", out, errs, status)
		}
		assertUnchanged(t, prior)
		invoked, err := os.ReadFile(filepath.Join(root, "invoked"))
		if err != nil || string(invoked) != "org display --target-org dev --json\norg auth show-access-token -o dev --no-prompt --json\n" {
			t.Fatalf("sf invocation=%q err=%v", invoked, err)
		}
	})
	t.Run("token_fallback_failure_preserves_prior_output", func(t *testing.T) {
		root, binary, args := fixture(t, map[string]any{"orgAlias": "dev", "objects": []string{"Account"}}, "")
		writeLocalOrgWithoutToken(t, root)
		prior := filepath.Join(root, "generated", "Account.d.ts")
		if err := os.MkdirAll(filepath.Dir(prior), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(prior, []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
		out, errs, status := runCLI(t, root, binary, "__fail__", args...)
		if out != "" || errs != "sf-typings: org token unavailable\n" || status != 1 {
			t.Fatalf("stdout=%q stderr=%q exit=%d", out, errs, status)
		}
		assertUnchanged(t, prior)
		invoked, err := os.ReadFile(filepath.Join(root, "invoked"))
		if err != nil || string(invoked) != "org auth show-access-token -o dev --no-prompt --json\n" {
			t.Fatalf("invocation=%q err=%v", invoked, err)
		}
	})
}

func TestPublicCLITokenOnlyRejectsMalformed(t *testing.T) {
	for _, tc := range []struct{ name, response string }{
		{"missing_access_token", `{"status":0,"result":{"instanceUrl":"https://evil.example"}}`},
		{"invalid_json", `{"status":0,"result":{"accessToken":"mock-secret"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, binary, args := fixture(t, map[string]any{"orgAlias": "dev", "objects": []string{"Account"}}, "")
			writeLocalOrgWithoutToken(t, root)
			prior := filepath.Join(root, "generated", "Account.d.ts")
			if err := os.MkdirAll(filepath.Dir(prior), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(prior, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			out, errs, status := runCLI(t, root, binary, tc.response, args...)
			if out != "" || errs != "sf-typings: org token unavailable\n" || status != 1 {
				t.Fatalf("stdout=%q stderr=%q exit=%d", out, errs, status)
			}
			assertUnchanged(t, prior)
			invoked, err := os.ReadFile(filepath.Join(root, "invoked"))
			if err != nil || string(invoked) != "org auth show-access-token -o dev --no-prompt --json\n" {
				t.Fatalf("invocation=%q err=%v", invoked, err)
			}
		})
	}
}

func TestPublicCLIMissingLocalURLOnly(t *testing.T) {
	for _, tc := range []struct{ name, response, log, want string }{
		{"token_error", "__fail__", "org display --target-org dev --json\norg auth show-access-token -o dev --no-prompt --json\n", "sf-typings: org token unavailable\n"},
		{"bad_display_host", "__display_untrusted__", "org display --target-org dev --json\n", "sf-typings: untrusted org instance\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, binary, args := fixture(t, map[string]any{"orgAlias": "dev", "objects": []string{"Account"}}, "")
			prior := filepath.Join(root, "generated", "Account.d.ts")
			if err := os.MkdirAll(filepath.Dir(prior), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(prior, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			out, errs, code := runCLI(t, root, binary, tc.response, args...)
			if out != "" || errs != tc.want || code != 1 {
				t.Fatalf("stdout=%q stderr=%q exit=%d", out, errs, code)
			}
			assertUnchanged(t, prior)
			log, err := os.ReadFile(filepath.Join(root, "invoked"))
			if err != nil || string(log) != tc.log {
				t.Fatalf("CLI calls=%q err=%v", log, err)
			}
		})
	}
}

func writeLocalOrgWithoutToken(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ".sf")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "alias.json"), []byte(`{"orgs":{"dev":"fixture@example.test"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixture@example.test.json"), []byte(`{"username":"fixture@example.test","instanceUrl":"https://sample.my.salesforce.com"}`), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCLIRefreshToken(t *testing.T) {
	root, _, _ := fixture(t, map[string]any{"apexSources": []string{}}, "")
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
			creds, err := cliCredentials(context.Background(), "dev")
			if tc.valid {
				if err != nil || creds.token != "fresh-secret" || creds.instance != "" {
					t.Fatalf("token_only=%v err=%v", creds.token == "fresh-secret", err)
				}
			} else {
				if err == nil || creds.token != "" || strings.Contains(err.Error(), "fresh-secret") {
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

func assertUnchanged(t *testing.T, path string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "original" {
		t.Fatalf("prior output=%q err=%v", got, err)
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
			local := func(context.Context, string) (credentials, error) {
				if tc.initialCLI {
					return credentials{instance: "https://old.my.salesforce.com"}, nil
				}
				return credentials{"stale-token", "https://old.my.salesforce.com"}, nil
			}
			fallback := func(context.Context, string) (credentials, error) {
				fallbackCalls++
				if tc.fallbackStatus == "error" {
					return credentials{}, fmt.Errorf("raw CLI secret")
				}
				token := "fresh-token"
				if tc.initialCLI {
					token = "stale-token"
				}
				return credentials{token, tc.fallbackURL}, nil
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
		func(context.Context, string) (credentials, error) { return credentials{}, fmt.Errorf("local missing") }, cliCredentials, client)
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
				func(context.Context, string) (credentials, error) {
					return credentials{"private-token", "https://sample.my.salesforce.com"}, nil
				},
				func(context.Context, string) (credentials, error) {
					t.Error("unexpected CLI fallback")
					return credentials{}, fmt.Errorf("fallback")
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
