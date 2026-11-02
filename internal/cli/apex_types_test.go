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

	"github.com/Hacerx/sf-typings/internal/auth"
)

func TestApexRecursiveTypesCLI(t *testing.T) {
	root, binary, args := fixture(t, map[string]any{"apexSources": []string{}}, "")
	sources := map[string]string{
		"Orders": `public class Orders {
 @AuraEnabled public static Map<Id, List<Envelope[]>> fetch(Map<Integer, Map<String, Boolean>> counts, List<String[]> names) { return null; }
 @AuraEnabled global static Other exchange(Other value) { return null; }
 @AuraEnabled public static SObject unresolved(SObject row, Object missing, Set<String> unsupported) { return null; }
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
	orders, err := os.ReadFile(filepath.Join(root, "generated", "apex", "Orders.d.ts"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := os.ReadFile(filepath.Join(root, "generated", "apex", "Other.d.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"counts: { [key: number]: { [key: string]: boolean } }; names: string[][]",
		"Promise<{ [key: string]: import('@sf-typings/apex/Orders').Envelope[][] }>",
		"value: import('@sf-typings/apex/Other').Other",
		"Promise<import('@sf-typings/apex/Other').Other>",
		"row: unknown; missing: unknown; unsupported: unknown",
		"declare module '@sf-typings/apex/Orders'",
		"export interface Envelope", `"label": string;`, `"first": number;`, `"second": number;`,
		`"other": import('@sf-typings/apex/Other').Other;`,
		`"next": import('@sf-typings/apex/Orders').Envelope;`,
	} {
		if !strings.Contains(string(orders), want) {
			t.Errorf("missing %q in\n%s", want, orders)
		}
	}
	for _, omitted := range []string{"secret", "shared", "omitted", "unannotated", "writeOnly", "privateGet", "unused", "export interface Orders", "export interface Unused", "export interface Type"} {
		if strings.Contains(string(orders), omitted) {
			t.Errorf("unexpected %q in\n%s", omitted, orders)
		}
	}
	for _, want := range []string{"export interface Other", `"envelope": import('@sf-typings/apex/Orders').Envelope;`, `"children": { [key: string]: import('@sf-typings/apex/Other').Other[] };`, `"active": boolean;`} {
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
		got, err := os.ReadFile(filepath.Join(root, "generated", "apex", name+".d.ts"))
		if err != nil || string(got) != string(original) {
			t.Fatalf("nondeterministic %s: %s %v", name, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "invoked")); !os.IsNotExist(err) {
		t.Fatalf("sf invoked: %v", err)
	}
}

func TestApexWrapperNamesCLI(t *testing.T) {
	root, binary, args := fixture(t, map[string]any{"apexSources": []string{}}, "")
	var paths []string
	for _, owner := range []string{"Orders", "Invoices"} {
		source := `public class ` + owner + ` {
 @AuraEnabled public static Box fetch() { return null; }
 public class Box {
  @AuraEnabled public boxValue value;
  @AuraEnabled public Box next;
  @AuraEnabled public type unsafeName;
  @AuraEnabled public type_ safeName;
 }
 public class boxValue { @AuraEnabled public String label; }
 public class type { @AuraEnabled public Boolean active; }
 public class type_ { @AuraEnabled public Integer count; }
}`
		path := filepath.Join(root, owner+".cls")
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	cfg, _ := json.Marshal(map[string]any{"apexSources": paths})
	if err := os.WriteFile(args[1], cfg, 0600); err != nil {
		t.Fatal(err)
	}
	out, errs, code := runCLI(t, root, binary, "", args...)
	if out != "Generated 0 object(s), 2 Apex class(es).\n" || errs != "" || code != 0 {
		t.Fatalf("stdout=%q stderr=%q exit=%d", out, errs, code)
	}
	for _, owner := range []string{"Orders", "Invoices"} {
		data, err := os.ReadFile(filepath.Join(root, "generated", "apex", owner+".d.ts"))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			"declare module '@sf-typings/apex/" + owner + "'",
			"export interface Box {", "export interface boxValue {",
			"export interface type_ {", "export interface type__ {",
			`"unsafeName": import('@sf-typings/apex/` + owner + `').type__;`,
			`"safeName": import('@sf-typings/apex/` + owner + `').type_;`,
			"Promise<import('@sf-typings/apex/" + owner + "').Box>",
			`"next": import('@sf-typings/apex/` + owner + `').Box;`,
			`"value": import('@sf-typings/apex/` + owner + `').boxValue;`,
		} {
			if !strings.Contains(string(data), want) {
				t.Errorf("missing %q in\n%s", want, data)
			}
		}
		if strings.Contains(string(data), "export interface Type") {
			t.Errorf("numeric wrapper name in\n%s", data)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "invoked")); !os.IsNotExist(err) {
		t.Fatalf("sf invoked: %v", err)
	}
}

// Equal basenames in different categories must coexist; legacy flat files stay untouched.
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
	if out.String() != "Generated 1 object(s), 1 Apex class(es).\n" || errs.String() != "" || code != 0 || calls != 2 {
		t.Fatalf("stdout=%q stderr=%q exit=%d calls=%d", out.String(), errs.String(), code, calls)
	}
	assertUnchanged(t, prior)
	for _, category := range []string{"apex", "sobjects"} {
		if _, err := os.Stat(filepath.Join(dest, category, "Account.d.ts")); err != nil {
			t.Fatal(err)
		}
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
