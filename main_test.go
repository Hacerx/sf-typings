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
		fmt.Fprintln(log, argv)
		if log.Close() != nil {
			os.Exit(4)
		}
		if argv == "org display --target-org dev --json" {
			if os.Getenv("SF_TYPINGS_RESPONSE") == "__display_untrusted__" {
				fmt.Print(`{"status":0,"result":{"instanceUrl":"https://evil.example","accessToken":"display-secret"}}`)
			} else {
				fmt.Print(`{"status":0,"result":{"instanceUrl":"https://sample.my.salesforce.com","accessToken":"display-secret"}}`)
			}
			os.Exit(0)
		}
		if os.Getenv("SF_TYPINGS_RESPONSE") == "__fail__" {
			fmt.Fprintln(os.Stderr, "secret-bearing failure")
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestRESTPublicCLI retains the original object success, failure, snapshot, and
// no-partial-publication checks through an injectable REST seam (never live auth).
func TestRESTPublicCLI(t *testing.T) {
	for _, tc := range []struct {
		name, result, wantError string
		objects                 []string
		status                  int
	}{
		{"success_and_snapshots", `{"name":"Account","fields":[{"name":"Name","type":"string","nillable":false},{"name":"AnnualRevenue","type":"currency","nillable":true},{"name":"IsActive__c","type":"boolean","nillable":false}],"extra":{"kept":true}}`, "", []string{"Account"}, 0},
		{"wrong_name", `{"name":"Contact","fields":[]}`, "invalid describe for Account", []string{"Account"}, 0},
		{"malicious_field", `{"name":"Account","fields":[{"name":"X; bad","type":"string","nillable":false}]}`, "invalid describe for Account", []string{"Account"}, 0},
		{"missing_nillable", `{"name":"Account","fields":[{"name":"Name","type":"string"}]}`, "invalid describe for Account", []string{"Account"}, 0},
		{"http_failure", ``, "REST Describe for Account: REST HTTP 403", []string{"Account"}, 403},
		{"second_describe_failure", `{"name":"Wrong","fields":[]}`, "invalid describe for Contact", []string{"Account", "Contact"}, 0},
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
			if tc.wantError != "" {
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
					io.WriteString(w, `[{"version":"10.9"},{"version":"10.10"}]`)
				case "/services/data/v10.10/sobjects/Account/describe":
					if tc.name == "second_describe_failure" {
						io.WriteString(w, `{"name":"Account","fields":[]}`)
					} else if tc.status != 0 {
						w.WriteHeader(tc.status)
					} else {
						io.WriteString(w, tc.result)
					}
				case "/services/data/v10.10/sobjects/Contact/describe":
					io.WriteString(w, tc.result)
				default:
					t.Errorf("unexpected REST path %s", r.URL.Path)
				}
			}))
			defer server.Close()
			transport := server.Client().Transport
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "sample.my.salesforce.com" {
					t.Errorf("unexpected target %s", r.URL.Host)
				}
				clone := r.Clone(r.Context())
				clone.URL.Host = strings.TrimPrefix(server.URL, "https://")
				return transport.RoundTrip(clone)
			})}
			calls := 0
			var out, errs strings.Builder
			code := runWithDeps([]string{"-config", cfg, "-out", outDir, "-snapshots"}, &out, &errs,
				func(_ context.Context, alias string) (credentials, error) {
					calls++
					if alias != "dev" {
						t.Errorf("alias=%q", alias)
					}
					return credentials{"private-token", "https://sample.my.salesforce.com"}, nil
				}, client)
			if calls != 1 {
				t.Fatalf("resolver calls=%d", calls)
			}
			if tc.wantError != "" {
				if out.String() != "" || errs.String() != "sf-typings: "+tc.wantError+"\n" || code != 1 {
					t.Fatalf("stdout=%q stderr=%q exit=%d", out.String(), errs.String(), code)
				}
				assertUnchanged(t, prior)
				if _, err := os.Stat(filepath.Join(outDir, "Account.describe.json")); !os.IsNotExist(err) {
					t.Fatalf("unexpected snapshot: %v", err)
				}
				if _, err := os.Stat(filepath.Join(outDir, "Contact.d.ts")); !os.IsNotExist(err) {
					t.Fatalf("unexpected Contact: %v", err)
				}
			} else {
				if out.String() != "Generated 1 object(s).\n" || errs.String() != "" || code != 0 {
					t.Fatalf("stdout=%q stderr=%q exit=%d", out.String(), errs.String(), code)
				}
				decl, err := os.ReadFile(prior)
				want := "// Generated by sf-typings. Do not edit.\nexport interface Account {\n  AnnualRevenue?: number | null;\n  IsActive__c?: boolean;\n  Name?: string;\n}\n"
				if err != nil || string(decl) != want {
					t.Fatalf("declaration=%q err=%v", decl, err)
				}
				snapshot, err := os.ReadFile(filepath.Join(outDir, "Account.describe.json"))
				if err != nil || !strings.Contains(string(snapshot), `"extra": {`) {
					t.Fatalf("snapshot=%q err=%v", snapshot, err)
				}
			}
		})
	}
}

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
		{"invalid_version", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `[{"version":"latest"}]`) }},
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
				}, client)
			if err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestRESTNoRetry(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(status)
			}))
			defer server.Close()
			transport := server.Client().Transport
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				clone := r.Clone(r.Context())
				clone.URL.Host = strings.TrimPrefix(server.URL, "https://")
				return transport.RoundTrip(clone)
			})}
			_, err := objectOutputs(context.Background(), "dev", []string{"Account"}, false,
				func(context.Context, string) (credentials, error) {
					return credentials{"private-token", "https://sample.my.salesforce.com"}, nil
				}, client)
			want := fmt.Sprintf("REST version lookup: REST HTTP %d", status)
			if calls != 1 || err == nil || err.Error() != want {
				t.Fatalf("calls=%d error=%v want=%q", calls, err, want)
			}
		})
	}
}

func TestRESTLocalFailure(t *testing.T) {
	for _, tc := range []struct {
		name             string
		creds            credentials
		localErr         error
		want             string
		displays, tokens int
	}{
		{"no_local_org", credentials{}, fmt.Errorf("secret-bearing lookup failure"), "org instance unavailable", 1, 0},
		{"missing_token", credentials{instance: "https://sample.my.salesforce.com"}, nil, "org token unavailable", 0, 1},
		{"incomplete_local_url", credentials{token: "secret"}, nil, "local org instance unavailable", 0, 0},
		{"untrusted_host", credentials{token: "secret", instance: "https://evil.test"}, nil, "untrusted org instance", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			displays, tokens := 0, 0
			_, err := objectOutputsWithFallback(context.Background(), "dev", []string{"Account"}, false,
				func(context.Context, string) (credentials, error) { return tc.creds, tc.localErr },
				func(context.Context, string) (credentials, error) {
					tokens++
					return credentials{}, fmt.Errorf("secret-bearing token error")
				},
				func(context.Context, string) (string, error) {
					displays++
					return "", fmt.Errorf("secret-bearing display error")
				}, nil)
			if err == nil || err.Error() != tc.want || displays != tc.displays || tokens != tc.tokens {
				t.Fatalf("error=%v displays=%d tokens=%d want=%q", err, displays, tokens, tc.want)
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
		out, errs, status := runCLI(t, root, binary, "", args...)
		if out != "" || errs != "sf-typings: invalid object API name\n" || status != 1 {
			t.Fatalf("stdout=%q stderr=%q exit=%d", out, errs, status)
		}
		assertUnchanged(t, prior)
		if _, err := os.Stat(filepath.Join(root, "invoked")); !os.IsNotExist(err) {
			t.Fatalf("sf invoked: %v", err)
		}
	})
	for _, tc := range []struct{ name, cfg, want string }{
		{"invalid_org", `{"orgAlias":"dev;echo secret","objects":["Account"]}`, "invalid org alias"},
		{"duplicate_objects", `{"orgAlias":"dev","objects":["Account","Account"]}`, "duplicate object API name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, binary, args := fixture(t, map[string]any{"apexSources": []string{}}, "")
			if err := os.WriteFile(args[1], []byte(tc.cfg), 0600); err != nil {
				t.Fatal(err)
			}
			out, errs, status := runCLI(t, root, binary, "", args...)
			if out != "" || errs != "sf-typings: "+tc.want+"\n" || status != 1 {
				t.Fatalf("stdout=%q stderr=%q exit=%d", out, errs, status)
			}
			if _, err := os.Stat(filepath.Join(root, "invoked")); !os.IsNotExist(err) {
				t.Fatalf("sf invoked: %v", err)
			}
		})
	}
}

func TestHelpIncludesApexOnly(t *testing.T) {
	root, binary, _ := fixture(t, map[string]any{"apexSources": []string{}}, "")
	out, errs, status := runCLI(t, root, binary, "", "-help")
	want := "Usage: sf-typings -config config.json -out generated [-snapshots]\nConfig: {\"orgAlias\":\"dev\",\"objects\":[\"Account\"],\"apexSources\":[\"classes\"]}\nObjects use local auth and direct REST Describe. When the local token is absent, sf org auth show-access-token -o <alias> --no-prompt --json supplies only the token; when the local org is unavailable, sf org display supplies only the instance URL. No HTTP 401 retry. Apex-only runs use local .cls files without an org alias. Never retrieves records.\n"
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
