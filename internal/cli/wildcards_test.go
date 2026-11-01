package cli

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

	"sf-typings/internal/auth"
)

func TestWildcardObjects(t *testing.T) {
	for _, tc := range []struct {
		name      string
		selectors []string
		option    any
		flags     []string
		want      []string
		wantError string
	}{
		{name: "prefix default", selectors: []string{"AC_*"}, want: []string{"AC_A__c", "AC_B__c"}},
		{name: "suffix", selectors: []string{"*__c"}, want: []string{"AC_A__c", "AC_B__c", "Other__c"}},
		{name: "multiple spans", selectors: []string{"A*C_*__c"}, want: []string{"AC_A__c", "AC_B__c"}},
		{name: "empty span", selectors: []string{"AC_*A__c"}, want: []string{"AC_A__c"}},
		{name: "all", selectors: []string{"*"}, want: []string{"AC_A__c", "AC_B__c", "Account", "Other__c"}},
		{name: "case sensitive", selectors: []string{"ac_*"}, wantError: "sf-typings: object patterns matched no objects\n"},
		{name: "unmatched union", selectors: []string{"Missing*", "AC_A__c"}, want: []string{"AC_A__c"}},
		{name: "overlap", selectors: []string{"AC_*", "*__c", "AC_A__c"}, want: []string{"AC_A__c", "AC_B__c", "Other__c"}},
		{name: "unfiltered config", selectors: []string{"AC_*"}, option: false, want: []string{"AC_A__c", "AC_B__c", "Account", "Other__c"}},
		{name: "unfiltered flag", selectors: []string{"AC_*"}, flags: []string{"-filter-related-objects=false"}, want: []string{"AC_A__c", "AC_B__c", "Account", "Other__c"}},
		{name: "flag override", selectors: []string{"AC_*"}, option: false, flags: []string{"-filter-related-objects=true"}, want: []string{"AC_A__c", "AC_B__c"}},
		{name: "exact default", selectors: []string{"AC_A__c"}, want: []string{"AC_A__c"}},
		{name: "no match", selectors: []string{"Missing*"}, wantError: "sf-typings: object patterns matched no objects\n"},
		{name: "invalid pattern", selectors: []string{"AC_?"}, wantError: "sf-typings: invalid object API name\n"},
		{name: "wrong option", selectors: []string{"AC_*"}, option: "false", wantError: "sf-typings: invalid config JSON\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			outDir := filepath.Join(dir, "out")
			os.Mkdir(outDir, 0700)
			prior := filepath.Join(outDir, "prior.txt")
			os.WriteFile(prior, []byte("original"), 0600)
			cfg := map[string]any{"orgAlias": "dev", "objects": tc.selectors, "out": outDir, "snapshots": true}
			if tc.option != nil {
				cfg["filterRelatedObjects"] = tc.option
			}
			raw, _ := json.Marshal(cfg)
			path := filepath.Join(dir, "config.json")
			os.WriteFile(path, raw, 0600)
			calls := map[string]int{}
			authCalls := 0
			resolver := func(context.Context, string) (auth.Credentials, error) {
				authCalls++
				return auth.Credentials{Token: "token", Instance: "https://sample.my.salesforce.com"}, nil
			}
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls[r.URL.Path]++
				body := `[{"version":"60.0"}]`
				switch {
				case r.URL.Path == "/services/data/":
				case r.URL.Path == "/services/data/v60.0/sobjects/":
					body = `{"sobjects":[{"name":"Other__c"},{"name":"AC_B__c"},{"name":"Account"},{"name":"AC_A__c"}]}`
				default:
					name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/services/data/v60.0/sobjects/"), "/describe")
					body = fmt.Sprintf(`{"name":%q,"fields":[]}`, name)
					if name == "AC_A__c" {
						body = `{"name":"AC_A__c","fields":[{"name":"WhoId","type":"reference","nillable":true,"relationshipName":"Who","referenceTo":["Account","AC_B__c"]}],"childRelationships":[{"relationshipName":"Others","childSObject":"Other__c","field":"ParentId"}]}`
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			var stdout, stderr strings.Builder
			code := runWithDeps(append([]string{"-config", path}, tc.flags...), &stdout, &stderr, resolver, resolver, client)
			if tc.wantError != "" {
				if code != 1 || stdout.String() != "" || stderr.String() != tc.wantError {
					t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
				}
				entries, _ := os.ReadDir(outDir)
				if len(entries) != 1 {
					t.Fatalf("published files: %v", entries)
				}
				assertUnchanged(t, prior)
				if tc.name == "invalid pattern" || tc.name == "wrong option" {
					if authCalls != 0 || len(calls) != 0 {
						t.Fatal("invalid input caused org access")
					}
				}
				return
			}
			if code != 0 || stderr.String() != "" || stdout.String() != fmt.Sprintf("Generated %d object(s).\n", len(tc.want)) {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			for _, name := range tc.want {
				if _, err := os.Stat(filepath.Join(outDir, name+".d.ts")); err != nil {
					t.Fatal(err)
				}
				if calls["/services/data/v60.0/sobjects/"+name+"/describe"] != 1 {
					t.Fatalf("calls=%v", calls)
				}
			}
			entries, _ := os.ReadDir(outDir)
			if len(entries) != len(tc.want)*2+1 {
				t.Fatalf("files=%v", entries)
			}
			declaration, _ := os.ReadFile(filepath.Join(outDir, "AC_A__c.d.ts"))
			filtered := len(tc.want) < 3 || tc.name == "suffix" || tc.name == "overlap"
			if filtered && strings.Contains(string(declaration), `from "./Account"`) {
				t.Fatalf("excluded import: %s", declaration)
			}
			if len(tc.want) == 1 && strings.Contains(string(declaration), "Who?:") {
				t.Fatalf("excluded relationship: %s", declaration)
			}
			snapshot, _ := os.ReadFile(filepath.Join(outDir, "AC_A__c.describe.json"))
			if !strings.Contains(string(snapshot), "Account") || !strings.Contains(string(snapshot), "Other__c") {
				t.Fatalf("snapshot pruned: %s", snapshot)
			}
			wantCatalog := 1
			if tc.name == "exact default" {
				wantCatalog = 0
			}
			if calls["/services/data/v60.0/sobjects/"] != wantCatalog {
				t.Fatalf("catalog calls=%v", calls)
			}
		})
	}
}
