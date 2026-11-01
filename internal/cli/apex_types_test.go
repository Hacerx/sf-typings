package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sf-typings/internal/auth"
)

func TestApexRecursiveTypesCLI(t *testing.T) {
	root, binary, args := fixture(t, map[string]any{"apexSources": []string{}}, "")
	sources := map[string]string{
		"Orders": `public class Orders {
 @AuraEnabled public static Map<Id, List<Envelope[]>> fetch(Map<Integer, Map<String, Boolean>> counts, List<String[]> names) { return null; }
 @AuraEnabled global static Other exchange(Other value) { return null; }
 @AuraEnabled public static Account unresolved(Schema.Account row, Missing missing, Set<String> unsupported) { return null; }
 public class Envelope {
  @AuraEnabled public String label;
  @AuraEnabled public Integer first, second;
  @AuraEnabled public Other other { get; private set; }
  @AuraEnabled public Envelope next;
  @AuraEnabled private String secret;
  @AuraEnabled public static String shared;
  @AuraEnabled public transient String omitted;
  public String unannotated;
  @AuraEnabled public String writeOnly { set; }
  @AuraEnabled public String privateGet { private get; set; }
 }
 public class Unused { @AuraEnabled public String unused; }
}`,
		"Other": `public class Other {
 @AuraEnabled public Orders.Envelope envelope;
 @AuraEnabled public Map<String, List<Other>> children;
 @AuraEnabled public Boolean active { get; set; }
}`,
	}
	for name, source := range sources {
		if err := os.WriteFile(filepath.Join(root, name+".cls"), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Reverse source order: cross-file lookup must not depend on discovery order.
	cfg, _ := json.Marshal(map[string]any{"apexSources": []string{filepath.Join(root, "Other.cls"), filepath.Join(root, "Orders.cls")}})
	if err := os.WriteFile(args[1], cfg, 0600); err != nil {
		t.Fatal(err)
	}
	out, errs, code := runCLI(t, root, binary, "", args...)
	if out != "Generated 0 object(s), 2 Apex class(es).\n" || errs != "" || code != 0 {
		t.Fatalf("stdout=%q stderr=%q exit=%d", out, errs, code)
	}
	orders, err := os.ReadFile(filepath.Join(root, "generated", "Orders.d.ts"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := os.ReadFile(filepath.Join(root, "generated", "Other.d.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"counts: { [key: number]: { [key: string]: boolean } }; names: string[][]",
		"Promise<{ [key: string]: import('@sf-typings/apex/Orders').Type2[][] }>",
		"value: import('@sf-typings/apex/Other').Type4",
		"Promise<import('@sf-typings/apex/Other').Type4>",
		"row: unknown; missing: unknown; unsupported: unknown",
		"declare module '@sf-typings/apex/Orders'",
		"export interface Type2", `"label": string;`, `"first": number;`, `"second": number;`,
		`"other": import('@sf-typings/apex/Other').Type4;`,
		`"next": import('@sf-typings/apex/Orders').Type2;`,
	} {
		if !strings.Contains(string(orders), want) {
			t.Errorf("missing %q in\n%s", want, orders)
		}
	}
	for _, omitted := range []string{"secret", "shared", "omitted", "unannotated", "writeOnly", "privateGet", "unused", "export interface Type1", "export interface Type3"} {
		if strings.Contains(string(orders), omitted) {
			t.Errorf("unexpected %q in\n%s", omitted, orders)
		}
	}
	for _, want := range []string{`"envelope": import('@sf-typings/apex/Orders').Type2;`, `"children": { [key: string]: import('@sf-typings/apex/Other').Type4[] };`, `"active": boolean;`} {
		if !strings.Contains(string(other), want) {
			t.Errorf("missing %q in\n%s", want, other)
		}
	}
	cfg, _ = json.Marshal(map[string]any{"apexSources": []string{filepath.Join(root, "Orders.cls"), filepath.Join(root, "Other.cls")}})
	if err := os.WriteFile(args[1], cfg, 0600); err != nil {
		t.Fatal(err)
	}
	out, errs, code = runCLI(t, root, binary, "", args...)
	if out != "Generated 0 object(s), 2 Apex class(es).\n" || errs != "" || code != 0 {
		t.Fatalf("repeat stdout=%q stderr=%q exit=%d", out, errs, code)
	}
	for name, original := range map[string][]byte{"Orders": orders, "Other": other} {
		got, err := os.ReadFile(filepath.Join(root, "generated", name+".d.ts"))
		if err != nil || string(got) != string(original) {
			t.Fatalf("nondeterministic %s: %s %v", name, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "invoked")); !os.IsNotExist(err) {
		t.Fatalf("sf invoked: %v", err)
	}
}

// Exercise the existing publication collision gate, not the generator alone.
func TestApexOutputCollisionCLI(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "Account.cls")
	if err := os.WriteFile(source, []byte(`public class Account { @AuraEnabled public static Account fetch() { return null; } @AuraEnabled public String name; }`), 0600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "generated")
	if err := os.Mkdir(dest, 0700); err != nil {
		t.Fatal(err)
	}
	prior := filepath.Join(dest, "Account.d.ts")
	if err := os.WriteFile(prior, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	cfg, _ := json.Marshal(map[string]any{"orgAlias": "dev", "objects": []string{"Account"}, "apexSources": []string{source}, "out": dest})
	if err := os.WriteFile(configPath, cfg, 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `[{"version":"60.0"}]`
		if r.URL.Path == "/services/data/v60.0/sobjects/Account/describe" {
			body = `{"name":"Account","fields":[]}`
		} else if r.URL.Path != "/services/data/" {
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	var out, errs strings.Builder
	code := runWithDeps([]string{"-config", configPath}, &out, &errs,
		func(context.Context, string) (auth.Credentials, error) {
			return auth.Credentials{Token: "synthetic", Instance: "https://sample.my.salesforce.com"}, nil
		},
		func(context.Context, string) (auth.Credentials, error) {
			t.Fatal("unexpected fallback")
			return auth.Credentials{}, nil
		}, client)
	if out.String() != "" || errs.String() != "sf-typings: duplicate output name\n" || code != 1 || calls != 2 {
		t.Fatalf("stdout=%q stderr=%q exit=%d calls=%d", out.String(), errs.String(), code, calls)
	}
	assertUnchanged(t, prior)
	entries, err := os.ReadDir(dest)
	if err != nil || len(entries) != 1 {
		t.Fatalf("outputs=%v err=%v", entries, err)
	}
}

func TestApexDuplicateIdentityCLI(t *testing.T) {
	for _, source := range []string{
		`public class Orders { @AuraEnabled public static void go(String value, Integer VALUE) {} }`,
		`public class Orders { @AuraEnabled public static void go() {} @AuraEnabled public static void GO(String value) {} }`,
		`public class Orders { public class Item {} public class ITEM {} }`,
		`public class Orders { @AuraEnabled public static Item go() { return null; } public class Item { @AuraEnabled public String value; @AuraEnabled public Integer VALUE; } }`,
	} {
		t.Run(source, func(t *testing.T) {
			root, binary, args := fixture(t, map[string]any{"apexSources": []string{}}, "")
			path := filepath.Join(root, "Orders.cls")
			if err := os.WriteFile(path, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, _ := json.Marshal(map[string]any{"apexSources": []string{path}})
			if err := os.WriteFile(args[1], cfg, 0600); err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(root, "generated")
			if err := os.Mkdir(dest, 0700); err != nil {
				t.Fatal(err)
			}
			prior := filepath.Join(dest, "Orders.d.ts")
			if err := os.WriteFile(prior, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			out, errs, code := runCLI(t, root, binary, "", args...)
			if out != "" || errs != "sf-typings: invalid Apex source\n" || code != 1 {
				t.Fatalf("stdout=%q stderr=%q exit=%d", out, errs, code)
			}
			assertUnchanged(t, prior)
			entries, err := os.ReadDir(dest)
			if err != nil || len(entries) != 1 {
				t.Fatalf("outputs=%v err=%v", entries, err)
			}
			if _, err := os.Stat(filepath.Join(root, "invoked")); !os.IsNotExist(err) {
				t.Fatalf("sf invoked: %v", err)
			}
		})
	}
}
