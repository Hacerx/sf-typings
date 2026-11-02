package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Hacerx/sf-typings/internal/auth"
)

// Run the injected CLI in a child so relative output never depends on a global chdir.
func TestDefaultOutputInjected(t *testing.T) {
	if os.Getenv("SF_TYPINGS_DEFAULT_CHILD") != "1" {
		return
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			args = args[i+1:]
			break
		}
	}
	calls, requests := 0, 0
	resolver := func(context.Context, string) (auth.Credentials, error) {
		calls++
		return auth.Credentials{Token: "synthetic", Instance: "https://sample.my.salesforce.com"}, nil
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		body := ""
		switch r.URL.Path {
		case "/services/data/":
			body = `[{"version":"60.0"}]`
		case "/services/data/v60.0/sobjects/":
			body = `{"sobjects":[{"name":"Account"}]}`
		case "/services/data/v60.0/sobjects/Account/describe":
			body = `{"name":"Account","fields":[]}`
		default:
			return nil, fmt.Errorf("unexpected request")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	code := runWithDeps(args, os.Stdout, os.Stderr, resolver, resolver, client)
	wantCalls, wantRequests := 0, 0
	if code == 0 {
		wantCalls, wantRequests = 1, 3
	}
	if calls != wantCalls || requests != wantRequests {
		fmt.Fprintf(os.Stderr, "unexpected counters: auth=%d requests=%d\n", calls, requests)
		os.Exit(99)
	}
	os.Exit(code)
}

func TestDefaultOutput(t *testing.T) {
	_, binary, _ := fixture(t, map[string]any{}, "")
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, dest string
		out        any
		hasOut     bool
		flags      []string
		injected   bool
		wantError  string
	}{
		{name: "omitted offline", dest: "sf-typings"},
		{name: "omitted both categories", dest: "sf-typings", injected: true},
		{name: "JSON replaces default", dest: "custom", out: "custom", hasOut: true, injected: true},
		{name: "flag replaces JSON", dest: "chosen", out: "custom", hasOut: true, flags: []string{"-out", "chosen"}, injected: true},
		{name: "flag replaces default", dest: "chosen", flags: []string{"-out", "chosen"}, injected: true},
		{name: "empty JSON", out: "", hasOut: true, injected: true, wantError: "arguments"},
		{name: "flag overrides empty JSON", dest: "chosen", out: "", hasOut: true, flags: []string{"-out", "chosen"}, injected: true},
		{name: "empty flag", out: "custom", hasOut: true, flags: []string{"-out", ""}, injected: true, wantError: "arguments"},
		{name: "null JSON cannot be repaired", out: nil, hasOut: true, flags: []string{"-out", "chosen"}, injected: true, wantError: "sf-typings: invalid config JSON\n"},
		{name: "wrong JSON cannot be repaired", out: true, hasOut: true, flags: []string{"-out", "chosen"}, injected: true, wantError: "sf-typings: invalid config JSON\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, configDir := t.TempDir(), t.TempDir()
			source := "public class Orders { @AuraEnabled public static String fetch() { return null; } }"
			if tc.injected {
				source = "public class Orders { @AuraEnabled public static Schema.Account fetch() { return null; } }"
			}
			writeLayoutFile(t, filepath.Join(root, "Orders.cls"), source)
			cfg := map[string]any{"apexSources": []string{"Orders.cls"}}
			if tc.injected {
				cfg["orgAlias"] = "dev"
				cfg["snapshots"] = true
			}
			if tc.hasOut {
				cfg["out"] = tc.out
			}
			raw, _ := json.Marshal(cfg)
			path := filepath.Join(configDir, "config.json")
			writeLayoutFile(t, path, string(raw))
			// Rejections preserve every root; successful runs must not create unselected roots.
			for _, dir := range []string{"sf-typings", "custom", "chosen"} {
				if tc.wantError != "" || dir == tc.dest {
					writeLayoutFile(t, filepath.Join(root, dir, "sentinel.txt"), "original")
				}
			}
			args := append([]string{"-config", path}, tc.flags...)
			cmd := exec.Command(binary, args...)
			if tc.injected {
				cmd = exec.Command(testBinary, append([]string{"-test.run=^TestDefaultOutputInjected$", "--"}, args...)...)
				cmd.Env = append(os.Environ(), "SF_TYPINGS_DEFAULT_CHILD=1")
			}
			cmd.Dir = root
			var out, errs strings.Builder
			cmd.Stdout, cmd.Stderr = &out, &errs
			code := 0
			if err := cmd.Run(); err != nil {
				exit, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatal(err)
				}
				code = exit.ExitCode()
			}
			want := "Generated 0 object(s), 1 Apex class(es).\n"
			if tc.injected {
				want = "Generated 1 object(s), 1 Apex class(es).\n"
			}
			if tc.wantError != "" {
				wantErr := tc.wantError
				if wantErr == "arguments" {
					wantErr = "sf-typings: invalid arguments\nUsage: sf-typings -config config.json [-out generated] [-only all|apex|sobjects] [-snapshots] [-all-children] [-timeout 5m] [-filter-related-objects=false]\n"
				}
				if code != 1 || out.String() != "" || errs.String() != wantErr {
					t.Fatalf("exit=%d stdout=%q stderr=%q", code, out.String(), errs.String())
				}
			} else if code != 0 || out.String() != want || errs.String() != "" {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, out.String(), errs.String())
			}
			for _, dir := range []string{"sf-typings", "custom", "chosen"} {
				if tc.wantError == "" && dir != tc.dest {
					if _, err := os.Stat(filepath.Join(root, dir)); !os.IsNotExist(err) {
						t.Fatalf("unexpected root %s: %v", dir, err)
					}
					continue
				}
				assertUnchanged(t, filepath.Join(root, dir, "sentinel.txt"))
				expected := []string{"sentinel.txt"}
				if tc.wantError == "" && dir == tc.dest {
					expected = []string{"apex/Orders.d.ts", "sentinel.txt"}
					if tc.injected {
						expected = append(expected, "sobjects/Account.d.ts", "sobjects/Account.describe.json")
						data, err := os.ReadFile(filepath.Join(root, dir, "apex", "Orders.d.ts"))
						if err != nil || !strings.Contains(string(data), "import('../sobjects/Account').Account") {
							t.Fatalf("declaration=%s err=%v", data, err)
						}
					}
				}
				if got := layoutNames(t, filepath.Join(root, dir)); !reflect.DeepEqual(got, expected) {
					t.Fatalf("%s files=%v want=%v", dir, got, expected)
				}
			}
			if got := layoutNames(t, configDir); !reflect.DeepEqual(got, []string{"config.json"}) {
				t.Fatalf("config directory files=%v", got)
			}
		})
	}
}
