package cli

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sf-typings/internal/auth"
	"sf-typings/internal/generator"
)

func TestDescribeDocsRunWithDeps(t *testing.T) {
	root := t.TempDir()
	cfg, dest := filepath.Join(root, "config.json"), filepath.Join(root, "sobjects", "Account.d.ts")
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
	local := func(context.Context, string) (auth.Credentials, error) {
		return auth.Credentials{"offline-token", "https://sample.my.salesforce.com"}, nil
	}
	fallback := func(context.Context, string) (auth.Credentials, error) {
		t.Fatal("unexpected authentication fallback")
		return auth.Credentials{}, nil
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
	for _, noise := range []string{"@future", "@length 0", "@defaultValue null", "@referenceTo", "@nillable", "@metadata"} {
		if strings.Contains(string(decl), noise) {
			t.Errorf("public output retained irrelevant metadata %s: %s", noise, decl)
		}
	}
	snapshotPath := filepath.Join(root, "sobjects", "Account.describe.json")
	snapshot, err := os.ReadFile(snapshotPath)
	if err != nil || string(snapshot) != string(docsGenerate(t, body)[1].Data) {
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

const docsDescribe = `{"name":"Account","label":"Accounts","createable":false,"future":{"z":null,"a":900719925474099312345},"fields":[
{"name":"Status","type":"picklist","nillable":true,"label":"Status label","inlineHelpText":"Choose a status","restrictedPicklist":false,"dependentPicklist":true,"controllerName":"Kind","picklistValues":[{"value":"Z","label":"Last","active":true,"defaultValue":false,"validFor":"AA=="},{"value":"A","label":"Old","active":false,"defaultValue":true},{"value":"?","label":"Unspecified"}]},
{"name":"Amount","type":"currency","nillable":false,"length":0,"precision":18,"scale":2,"createable":false,"updateable":true,"calculated":true,"calculatedFormula":"1 + 2","defaultValue":null,"referenceTo":["Account"],"relationshipName":"Parent","future":{"ratio":1.234567890123456789,"flag":false}}
]}`

func docsGenerate(t *testing.T, body string) []generator.File {
	t.Helper()
	files, err := generator.Generate("Account", []byte(`{"status":0,"result":`+body+`}`), true)
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
