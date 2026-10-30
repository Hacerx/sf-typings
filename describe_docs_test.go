package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const docsDescribe = `{"name":"Account","label":"Accounts","createable":false,"future":{"z":null,"a":900719925474099312345},"fields":[
{"name":"Status","type":"picklist","nillable":true,"label":"Status label","inlineHelpText":"Choose a status","restrictedPicklist":false,"dependentPicklist":true,"controllerName":"Kind","picklistValues":[{"value":"Z","label":"Last","active":true,"defaultValue":false,"validFor":"AA=="},{"value":"A","label":"Old","active":false,"defaultValue":true},{"value":"?","label":"Unspecified"}]},
{"name":"Amount","type":"currency","nillable":false,"length":0,"precision":18,"scale":2,"createable":false,"updateable":true,"calculated":true,"calculatedFormula":"1 + 2","defaultValue":null,"referenceTo":["Account"],"relationshipName":"Parent","future":{"ratio":1.234567890123456789,"flag":false}}
]}`

func docsGenerate(t *testing.T, body string) []output {
	t.Helper()
	files, err := generate("Account", []byte(`{"status":0,"result":`+body+`}`), true)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func requireDocs(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in declaration:\n%s", want, got)
		}
	}
}

func TestDescribeDocsMetadata(t *testing.T) {
	files := docsGenerate(t, docsDescribe)
	got := string(files[0].data)
	requireDocs(t, got, " * @label Accounts", " * @createable false", `@future {"a":900719925474099312345,"z":null}`,
		`@label Status label`, `@inlineHelpText Choose a status`, "@length 0", "@precision 18", "@scale 2",
		"@updateable true", "@calculated true", `@calculatedFormula 1 + 2`, "@defaultValue null",
		`@referenceTo ["Account"]`, `@relationshipName Parent`, `@future {"flag":false,"ratio":1.234567890123456789}`, `@sfType currency`, `@metadata "name" "Amount"`,
		"Amount?: number;", "Status?: 'Z' | 'A' | '?' | (string & {}) | null;")
	objectDoc := strings.Split(got, "export interface Account")[0]
	if strings.Contains(objectDoc, "@fields") || strings.Index(objectDoc, "@createable") > strings.Index(objectDoc, "@future") {
		t.Fatalf("object metadata not separated/sorted: %s", objectDoc)
	}
	if strings.Index(got, "Amount?:") > strings.Index(got, "Status?:") {
		t.Fatal("field order changed")
	}
	var original any
	if err := decode([]byte(docsDescribe), &original, false); err != nil {
		t.Fatal(err)
	}
	wantSnapshot, _ := json.MarshalIndent(original, "", "  ")
	if string(files[1].data) != string(wantSnapshot)+"\n" {
		t.Fatal("metadata documentation changed the snapshot")
	}
	if again := docsGenerate(t, docsDescribe); string(again[0].data) != got {
		t.Fatal("nondeterministic documentation")
	}
}

func TestDescribeDocsPicklists(t *testing.T) {
	got := string(docsGenerate(t, docsDescribe)[0].data)
	requireDocs(t, got,
		`@picklistValue {"active":true,"defaultValue":false,"label":"Last","validFor":"AA==","value":"Z"}`,
		`@picklistValue {"active":false,"defaultValue":true,"label":"Old","value":"A"}`,
		`@picklistValue {"label":"Unspecified","value":"?"}`,
		"Unrestricted picklists may accept values not listed here.",
		"Dependent picklists require controller/validFor filtering.",
		"Describe options are not record-type filtered; availability may vary by record type.")
	if strings.Index(got, `"value":"Z"`) > strings.Index(got, `"value":"A"`) {
		t.Fatal("picklist input order changed")
	}
	for _, kind := range []string{"picklist", "multipicklist"} {
		body := fmt.Sprintf(`{"name":"Account","fields":[{"name":"Choice","type":%q,"nillable":false,"restrictedPicklist":true,"picklistValues":[]}]}`, kind)
		decl := string(docsGenerate(t, body)[0].data)
		requireDocs(t, decl, "Choice?: string;", "@picklistValues []", "not record-type filtered")
		if strings.Contains(decl, "Unrestricted picklists") || strings.Contains(decl, "Dependent picklists require") {
			t.Fatalf("incorrect context warning: %s", decl)
		}
	}
}

func TestDescribeDocsEscaping(t *testing.T) {
	body := `{"name":"Account","label":"*/\r\n@deprecated\u2028@hidden\u2029*/","future":{"*/\n@tag":["*/",null]},"fields":[{"name":"Name","type":"string","nillable":false,"inlineHelpText":"*/\r@tag\n* @other","picklistValues":[{"value":"*/","label":"\n@tag"}]}]}`
	got := string(docsGenerate(t, body)[0].data)
	if strings.Count(got, "*/") != 2 || strings.ContainsAny(got, "\r\u2028\u2029") {
		t.Fatalf("unsafe comment boundary: %s", got)
	}
	allowed := map[string]bool{"@metadata": true, "@label": true, "@future": true, "@sfType": true, "@nillable": true, "@inlineHelpText": true, "@picklistValue": true}
	for _, line := range strings.Split(got, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
		if strings.HasPrefix(line, "@") && !allowed[strings.Fields(line)[0]] {
			t.Fatalf("unexpected JSDoc tag: %q", line)
		}
	}
	requireDocs(t, got, `@label *\/\r\n\u0040deprecated`, `@picklistValue {`)
	requireDocs(t, got, `*\/`, `\r\n\u0040deprecated`, `\u2028\u0040hidden`, `\u2029`, "Name?: string;")
}

func TestDescribeDocsValidationAndTypes(t *testing.T) {
	for _, fields := range []string{
		`null`, `{}`, `[null]`, `[42]`,
		`[{"name":"Bad","type":"string","nillable":null}]`,
		`[{"name":"Bad","type":42,"nillable":false}]`,
		`[{"name":"Bad*/","type":"string","nillable":false}]`,
		`[{"name":"A","type":"string","nillable":false},{"name":"A","type":"boolean","nillable":true}]`,
	} {
		_, err := generate("Account", []byte(`{"status":0,"result":{"name":"Account","fields":`+fields+`}}`), true)
		if err == nil || err.Error() != "invalid describe for Account" {
			t.Errorf("accepted invalid fields %s: %v", fields, err)
		}
	}
	for kind, want := range map[string]string{"reference": "string", "boolean": "boolean", "long": "number", "datetime": "string", "futureType": "unknown"} {
		body := fmt.Sprintf(`{"name":"Account","fields":[{"name":"Value","type":%q,"nillable":true,"future":null,"picklistValues":null}]}`, kind)
		requireDocs(t, string(docsGenerate(t, body)[0].data), "Value?: "+want+" | null;", "@future null", "@picklistValues null")
	}
}

func TestDescribeDocsRunWithDeps(t *testing.T) {
	root := t.TempDir()
	cfg, dest := filepath.Join(root, "config.json"), filepath.Join(root, "Account.d.ts")
	if err := os.WriteFile(cfg, []byte(`{"orgAlias":"dev","objects":["Account"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	body := docsDescribe
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		response := body
		if r.URL.Path == "/services/data/" {
			response = `[{"version":"60.0"}]`
		} else if r.URL.Path != "/services/data/v60.0/sobjects/Account/describe" {
			t.Fatalf("unexpected request: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response)), Header: make(http.Header)}, nil
	})}
	local := func(context.Context, string) (credentials, error) {
		return credentials{"offline-token", "https://sample.my.salesforce.com"}, nil
	}
	fallback := func(context.Context, string) (credentials, error) {
		t.Fatal("unexpected authentication fallback")
		return credentials{}, nil
	}
	var out, errs strings.Builder
	args := []string{"-config", cfg, "-out", root, "-snapshots"}
	if code := runWithDeps(args, &out, &errs, local, fallback, client); code != 0 || out.String() != "Generated 1 object(s).\n" || errs.String() != "" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, out.String(), errs.String())
	}
	decl, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	requireDocs(t, string(decl), `@label Accounts`, `@sfType currency`, `@picklistValue {"active":true`, `@picklistValue {"active":false`, "Status?: 'Z' | 'A' | '?' | (string & {}) | null;", "@calculatedFormula 1 + 2", "@createable false", "@updateable true")
	snapshotPath := filepath.Join(root, "Account.describe.json")
	snapshot, err := os.ReadFile(snapshotPath)
	if err != nil || string(snapshot) != string(docsGenerate(t, body)[1].data) {
		t.Fatalf("snapshot changed: %v", err)
	}
	body = `{"name":"Account","fields":[{"name":"Broken","type":"string"}]}`
	out.Reset()
	errs.Reset()
	if code := runWithDeps(args, &out, &errs, local, fallback, client); code != 1 || out.String() != "" || errs.String() != "sf-typings: invalid describe for Account\n" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, out.String(), errs.String())
	}
	for path, want := range map[string][]byte{dest: decl, snapshotPath: snapshot} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(want) {
			t.Fatalf("failed generation overwrote %s: %v", path, err)
		}
	}
}
