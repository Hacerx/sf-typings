package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"sf-typings/internal/auth"
	"sf-typings/internal/generator"
)

func TestSeparateModes(t *testing.T) {
	for _, mode := range []string{"all", "apex", "sobjects", "sobjects-missing", "flag-apex", "flag-sobjects", "flag-all"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "Orders.cls")
			text := `public class Orders { @AuraEnabled public static Other fetch(Schema.Account row) { return null; } }`
			if strings.HasPrefix(mode, "sobjects") || mode == "flag-sobjects" {
				text = `malformed excluded Apex`
			}
			writeLayoutFile(t, source, text)
			other := filepath.Join(root, "Other.cls")
			writeLayoutFile(t, other, `public class Other { @AuraEnabled public Account row; }`)
			same := filepath.Join(root, "Account.cls")
			writeLayoutFile(t, same, `public class Account { @AuraEnabled public static String name() { return null; } }`)
			if mode == "sobjects-missing" {
				source = filepath.Join(root, "nonexistent.cls")
			}
			dest := filepath.Join(root, "generated")
			priorApex := filepath.Join(dest, "apex", "Orders.d.ts")
			priorObject := filepath.Join(dest, "sobjects", "Unrelated.d.ts")
			writeLayoutFile(t, priorApex, "original")
			writeLayoutFile(t, priorObject, "original")
			priorSnapshot := filepath.Join(dest, "sobjects", "Unrelated.describe.json")
			writeLayoutFile(t, priorSnapshot, "original")
			writeLayoutFile(t, filepath.Join(dest, "sobjects", "Account.d.ts"), "original")
			writeLayoutFile(t, filepath.Join(dest, "sobjects", "Account.describe.json"), "original")
			// The local Account class shadows bare Account, so Schema.Account proves
			// inference while cross-file wrappers and same-basename outputs coexist.
			cfg := map[string]any{"orgAlias": "dev", "objects": []string{"Account", "Unrelated"}, "apexSources": []string{source, other, same}, "out": dest, "snapshots": true}
			selected := mode
			if strings.HasPrefix(mode, "sobjects") {
				selected = "sobjects"
			}
			args := []string{}
			if mode == "flag-apex" {
				selected = "apex"
				cfg["only"] = "sobjects"
				args = []string{"-only", "apex"}
			} else if mode == "flag-sobjects" {
				selected = "sobjects"
				cfg["only"] = "apex"
				args = []string{"-only", "sobjects"}
			} else if mode == "flag-all" {
				selected = "all"
				cfg["only"] = "apex"
				args = []string{"-only", "all"}
			} else if mode != "all" {
				cfg["only"] = selected
			}
			raw, _ := json.Marshal(cfg)
			path := filepath.Join(root, "config.json")
			writeLayoutFile(t, path, string(raw))
			authCalls, requests := 0, []string{}
			resolver := func(context.Context, string) (auth.Credentials, error) {
				authCalls++
				return auth.Credentials{Token: "synthetic", Instance: "https://sample.my.salesforce.com"}, nil
			}
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests = append(requests, r.URL.Path)
				body := ""
				switch r.URL.Path {
				case "/services/data/":
					body = `[{"version":"60.0"}]`
				case "/services/data/v60.0/sobjects/":
					body = `{"sobjects":[{"name":"Account"},{"name":"Unrelated"}]}`
				case "/services/data/v60.0/sobjects/Account/describe":
					body = `{"name":"Account","fields":[{"name":"Name","type":"string","nillable":false},{"name":"ParentId","type":"reference","nillable":true,"relationshipName":"Parent","referenceTo":["Unrelated"]}]}`
				case "/services/data/v60.0/sobjects/Unrelated/describe":
					body = `{"name":"Unrelated","fields":[]}`
				default:
					t.Fatalf("unexpected request %s", r.URL.Path)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			var out, errs strings.Builder
			code := runWithDeps(append([]string{"-config", path}, args...), &out, &errs, resolver, resolver, client)
			want := "Generated 2 object(s), 3 Apex class(es).\n"
			wantRequests := 4
			if selected == "apex" {
				want = "Generated 1 object(s), 3 Apex class(es).\n"
				wantRequests = 3
			}
			if selected == "sobjects" {
				want = "Generated 2 object(s).\n"
				wantRequests = 3
			}
			if code != 0 || out.String() != want || errs.String() != "" || authCalls != 1 || len(requests) != wantRequests {
				t.Fatalf("exit=%d out=%q err=%q auth=%d requests=%v", code, out.String(), errs.String(), authCalls, requests)
			}
			names := layoutNames(t, dest)
			expected := []string{"apex/Orders.d.ts", "sobjects/Account.d.ts", "sobjects/Account.describe.json", "sobjects/Unrelated.d.ts", "sobjects/Unrelated.describe.json"}
			if selected == "apex" {
				assertUnchanged(t, priorObject)
				assertUnchanged(t, priorSnapshot)
			}
			if selected == "sobjects" {
				assertUnchanged(t, priorApex)
			} else {
				expected = append(expected, "apex/Account.d.ts", "apex/Other.d.ts")
				data, _ := os.ReadFile(priorApex)
				if !strings.Contains(string(data), "import('../sobjects/Account').Account") || !strings.Contains(string(data), "import('@sf-typings/apex/Other').Other") {
					t.Fatalf("declaration: %s", data)
				}
			}
			sort.Strings(expected)
			if !reflect.DeepEqual(names, expected) {
				t.Fatalf("files=%v want=%v", names, expected)
			}
			account, err := os.ReadFile(filepath.Join(dest, "sobjects", "Account.d.ts"))
			if err != nil || strings.Contains(string(account), `from "./Unrelated"`) != (selected != "apex") {
				t.Fatalf("sibling reference: %s %v", account, err)
			}
			snapshot, err := os.ReadFile(filepath.Join(dest, "sobjects", "Account.describe.json"))
			if err != nil || !json.Valid(snapshot) || !strings.Contains(string(snapshot), `"Unrelated"`) {
				t.Fatalf("snapshot: %s %v", snapshot, err)
			}
			if mode == "all" && os.Getenv("SF_TYPINGS_LAYOUT_PROOF") == "1" {
				for _, name := range names {
					data, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(name)))
					if err != nil {
						t.Fatal(err)
					}
					writeLayoutFile(t, filepath.Join("..", "..", ".tmp", "apex-types-check", "layout-proof", "generated", filepath.FromSlash(name)), string(data))
				}
			}
		})
	}
}

func TestOnlyValidation(t *testing.T) {
	for _, tc := range []struct {
		name             string
		value            any
		flag             *string
		objects, sources bool
		want             string
	}{
		{name: "empty JSON", value: "", objects: true, sources: true, want: "invalid only (use all, apex, or sobjects)"},
		{name: "invalid JSON", value: "ALL", objects: true, sources: true, want: "invalid only (use all, apex, or sobjects)"},
		{name: "null", value: nil, objects: true, sources: true, want: "invalid config JSON"},
		{name: "wrong type", value: 1, objects: true, sources: true, want: "invalid config JSON"},
		{name: "null cannot be overridden", value: nil, flag: layoutString("all"), objects: true, sources: true, want: "invalid config JSON"},
		{name: "wrong type cannot be overridden", value: true, flag: layoutString("apex"), objects: true, sources: true, want: "invalid config JSON"},
		{name: "invalid cannot be overridden", value: "bad", flag: layoutString("all"), objects: true, sources: true, want: "invalid only (use all, apex, or sobjects)"},
		{name: "empty flag", value: "all", flag: layoutString(""), objects: true, sources: true, want: "invalid only (use all, apex, or sobjects)"},
		{name: "invalid flag", value: "all", flag: layoutString("bad"), objects: true, sources: true, want: "invalid only (use all, apex, or sobjects)"},
		{name: "missing Apex", value: "apex", objects: true, want: "apexSources must be nonempty for only apex"},
		{name: "missing objects", value: "sobjects", sources: true, want: "objects must be nonempty for only sobjects"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "generated")
			prior := filepath.Join(dest, "apex", "Orders.d.ts")
			writeLayoutFile(t, prior, "original")
			cfg := map[string]any{"orgAlias": "dev", "out": dest, "only": tc.value}
			if tc.objects {
				cfg["objects"] = []string{"Account"}
			}
			if tc.sources {
				cfg["apexSources"] = []string{filepath.Join(root, "missing.cls")}
			}
			raw, _ := json.Marshal(cfg)
			path := filepath.Join(root, "config.json")
			writeLayoutFile(t, path, string(raw))
			args := []string{"-config", path}
			if tc.flag != nil {
				args = append(args, "-only", *tc.flag)
			}
			calls := 0
			resolver := func(context.Context, string) (auth.Credentials, error) { calls++; return auth.Credentials{}, nil }
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, nil })}
			var out, errs strings.Builder
			code := runWithDeps(args, &out, &errs, resolver, resolver, client)
			if code != 1 || out.String() != "" || errs.String() != "sf-typings: "+tc.want+"\n" || calls != 0 {
				t.Fatalf("exit=%d out=%q err=%q calls=%d", code, out.String(), errs.String(), calls)
			}
			assertUnchanged(t, prior)
			if got := layoutNames(t, dest); !reflect.DeepEqual(got, []string{"apex/Orders.d.ts"}) {
				t.Fatalf("files=%v", got)
			}
		})
	}
}

func TestOutputNestedSafety(t *testing.T) {
	for _, kind := range []string{"directory target", "file component", "symlink component", "symlink target", "escape"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "generated")
			prior := filepath.Join(dest, "apex", "Orders.d.ts")
			writeLayoutFile(t, prior, "original")
			files := []generator.File{{Name: "apex/Orders.d.ts", Data: []byte("changed")}, {Name: "sobjects/Account.d.ts", Data: []byte("new")}}
			want := "unsafe output target"
			switch kind {
			case "directory target":
				if err := os.MkdirAll(filepath.Join(dest, "sobjects", "Account.d.ts"), 0700); err != nil {
					t.Fatal(err)
				}
			case "file component":
				writeLayoutFile(t, filepath.Join(dest, "sobjects"), "original")
				want = "unsafe output directory"
			case "symlink component", "symlink target":
				outside := filepath.Join(root, "outside")
				writeLayoutFile(t, filepath.Join(outside, "Account.d.ts"), "original")
				link, target := filepath.Join(dest, "sobjects"), outside
				if kind == "symlink target" {
					if err := os.MkdirAll(link, 0700); err != nil {
						t.Fatal(err)
					}
					link = filepath.Join(link, "Account.d.ts")
					target = filepath.Join(outside, "Account.d.ts")
				} else {
					want = "unsafe output directory"
				}
				if err := os.Symlink(target, link); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				defer assertUnchanged(t, filepath.Join(outside, "Account.d.ts"))
			case "escape":
				files[1].Name = "../escape.d.ts"
			}
			if err := save(dest, files); err == nil || err.Error() != want {
				t.Fatalf("error=%v want=%q", err, want)
			}
			assertUnchanged(t, prior)
		})
	}
}

// Opt-in artifact export keeps ordinary tests isolated in t.TempDir.
func TestSeparateLayoutProof(t *testing.T) {
	if os.Getenv("SF_TYPINGS_LAYOUT_PROOF") != "1" {
		t.Skip("set SF_TYPINGS_LAYOUT_PROOF=1 to export the mock CLI proof")
	}
	TestSeparateModes(t)
}

func TestSeparateUnsafeCLI(t *testing.T) {
	for _, kind := range []string{"file component", "directory target", "symlink component", "metadata failure"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "generated")
			prior := filepath.Join(dest, "apex", "Orders.d.ts")
			writeLayoutFile(t, prior, "original")
			source := filepath.Join(root, "Orders.cls")
			writeLayoutFile(t, source, `public class Orders { @AuraEnabled public static String go() { return null; } }`)
			want := "sf-typings: unsafe output directory\n"
			switch kind {
			case "file component":
				writeLayoutFile(t, filepath.Join(dest, "sobjects"), "original")
			case "directory target":
				if err := os.MkdirAll(filepath.Join(dest, "sobjects", "Account.d.ts"), 0700); err != nil {
					t.Fatal(err)
				}
				want = "sf-typings: unsafe output target\n"
			case "symlink component":
				outside := filepath.Join(root, "outside")
				writeLayoutFile(t, filepath.Join(outside, "Account.d.ts"), "original")
				if err := os.Symlink(outside, filepath.Join(dest, "sobjects")); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				defer assertUnchanged(t, filepath.Join(outside, "Account.d.ts"))
			case "metadata failure":
				writeLayoutFile(t, filepath.Join(dest, "sobjects", "Account.d.ts"), "original")
				want = "sf-typings: invalid describe for Account\n"
			}
			raw, _ := json.Marshal(map[string]any{"orgAlias": "dev", "objects": []string{"Account"}, "apexSources": []string{source}, "out": dest})
			path := filepath.Join(root, "config.json")
			writeLayoutFile(t, path, string(raw))
			before := layoutNames(t, dest)
			authCalls, requests := 0, 0
			resolver := func(context.Context, string) (auth.Credentials, error) {
				authCalls++
				return auth.Credentials{Token: "synthetic", Instance: "https://sample.my.salesforce.com"}, nil
			}
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				body := `[{"version":"60.0"}]`
				if r.URL.Path != "/services/data/" {
					if r.URL.Path != "/services/data/v60.0/sobjects/Account/describe" {
						t.Fatalf("unexpected request %s", r.URL.Path)
					}
					body = `{"name":"Account","fields":[]}`
					if kind == "metadata failure" {
						body = `{"name":"Wrong","fields":[]}`
					}
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			var out, errs strings.Builder
			code := runWithDeps([]string{"-config", path}, &out, &errs, resolver, resolver, client)
			if code != 1 || out.String() != "" || errs.String() != want || authCalls != 1 || requests != 2 {
				t.Fatalf("exit=%d out=%q err=%q auth=%d requests=%d", code, out.String(), errs.String(), authCalls, requests)
			}
			assertUnchanged(t, prior)
			if !reflect.DeepEqual(before, layoutNames(t, dest)) {
				t.Fatal("partial output")
			}
			if kind == "file component" {
				assertUnchanged(t, filepath.Join(dest, "sobjects"))
			}
			if kind == "metadata failure" {
				assertUnchanged(t, filepath.Join(dest, "sobjects", "Account.d.ts"))
			}
		})
	}
}

func layoutString(s string) *string { return &s }
func writeLayoutFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func layoutNames(t *testing.T, root string) []string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			names = append(names, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	return names
}
