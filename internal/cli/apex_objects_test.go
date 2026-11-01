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

func TestApexObjectsCLI(t *testing.T) {
	for _, mode := range []string{
		"inferred", "configured", "missing_alias", "bad_alias", "metadata_failure",
		"malformed", "retry", "offline", "configured_both", "wildcard", "unrestricted",
	} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			source := `public class Orders {
 @AuraEnabled public static Account direct(Schema.Account row) { return null; }
 @AuraEnabled public static Map<String,List<Box>> nested(List<Account> rows) { return null; }
 public class Box { @AuraEnabled public Map<Id,List<Schema.Contact>> rows; @AuraEnabled public Missing missing; @AuraEnabled public SObject generic; @AuraEnabled public Object value; }
 }`
			if mode == "malformed" {
				source = `public class Orders { broken`
			}
			if mode == "offline" {
				// A local Account shadows the catalog identity and stays offline.
				source = `public class Orders { @AuraEnabled public static Account fetch(String value) { return null; } public class Account { @AuraEnabled public String name; } }`
			}
			path := filepath.Join(root, "Orders.cls")
			if err := os.WriteFile(path, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(root, "generated")
			if err := os.Mkdir(dest, 0700); err != nil {
				t.Fatal(err)
			}
			prior := filepath.Join(dest, "sentinel")
			if err := os.WriteFile(prior, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			cfg := map[string]any{"orgAlias": "dev", "apexSources": []string{path}, "out": dest}
			if mode == "missing_alias" || mode == "offline" {
				delete(cfg, "orgAlias")
			}
			if mode == "bad_alias" {
				cfg["orgAlias"] = "invalid alias"
			}
			if mode == "configured" || mode == "malformed" || mode == "unrestricted" {
				cfg["objects"] = []string{"Account"}
			}
			if mode == "configured_both" {
				cfg["objects"] = []string{"Account", "Contact"}
			}
			if mode == "wildcard" {
				cfg["objects"] = []string{"Acc*"}
			}
			if mode == "unrestricted" {
				cfg["filterRelatedObjects"] = false
			}
			raw, _ := json.Marshal(cfg)
			configPath := filepath.Join(root, "config.json")
			if err := os.WriteFile(configPath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			requests := map[string]int{}
			authCalls, fallbackCalls := 0, 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests[r.URL.Path]++
				status, body := 200, ""
				switch r.URL.Path {
				case "/services/data/":
					body = `[{"version":"60.0"}]`
				case "/services/data/v60.0/sobjects/":
					body = `{"sobjects":[{"name":"Account"},{"name":"Contact"}]}`
					if mode == "metadata_failure" {
						status, body = 403, "private response"
					}
					if mode == "retry" && r.Header.Get("Authorization") == "Bearer stale" {
						status = 401
					}
				case "/services/data/v60.0/sobjects/Account/describe":
					body = `{"name":"Account","fields":[{"name":"Name","type":"string","nillable":false}],"childRelationships":[{"relationshipName":"Contacts","childSObject":"Contact","field":"AccountId"}]}`
				case "/services/data/v60.0/sobjects/Contact/describe":
					body = `{"name":"Contact","fields":[{"name":"AccountId","type":"reference","nillable":true,"relationshipName":"Account","referenceTo":["Account"]}],"childRelationships":[{"relationshipName":"Unrelated","childSObject":"Unrelated","field":"ContactId"},{"relationshipName":"AllowedChildren","childSObject":"Account","field":"ContactId"}]}`
				default:
					t.Fatalf("unexpected request %s", r.URL.Path)
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			var out, errs strings.Builder
			code := runWithDeps([]string{"-config", configPath}, &out, &errs,
				func(context.Context, string) (auth.Credentials, error) {
					authCalls++
					return auth.Credentials{Token: "stale", Instance: "https://sample.my.salesforce.com"}, nil
				},
				func(context.Context, string) (auth.Credentials, error) {
					fallbackCalls++
					return auth.Credentials{Token: "fresh"}, nil
				}, client)
			wantErr := ""
			switch mode {
			case "missing_alias", "bad_alias":
				wantErr = "sf-typings: invalid org alias\n"
			case "metadata_failure":
				wantErr = "sf-typings: REST object catalog: REST HTTP 403\n"
			case "malformed":
				wantErr = "sf-typings: invalid Apex source\n"
			}
			wantAuth, wantRequests, wantFallback := 1, 4, 0
			if mode == "retry" {
				wantRequests, wantFallback = 6, 1
			}
			if mode == "metadata_failure" {
				wantRequests = 2
			}
			if mode == "missing_alias" || mode == "bad_alias" || mode == "malformed" || mode == "offline" {
				wantAuth, wantRequests = 0, 0
			}
			total := 0
			for _, n := range requests {
				total += n
			}
			if authCalls != wantAuth || total != wantRequests || fallbackCalls != wantFallback {
				t.Fatalf("auth=%d fallback=%d requests=%v stdout=%q stderr=%q", authCalls, fallbackCalls, requests, out.String(), errs.String())
			}
			if wantErr != "" {
				if code != 1 || out.String() != "" || errs.String() != wantErr {
					t.Fatalf("exit=%d stdout=%q stderr=%q", code, out.String(), errs.String())
				}
				assertUnchanged(t, prior)
				entries, _ := os.ReadDir(dest)
				if len(entries) != 1 {
					t.Fatalf("published %v", entries)
				}
				return
			}
			wantOut := "Generated 2 object(s), 1 Apex class(es).\n"
			if mode == "offline" {
				wantOut = "Generated 0 object(s), 1 Apex class(es).\n"
			}
			if code != 0 || out.String() != wantOut || errs.String() != "" {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, out.String(), errs.String())
			}
			data, err := os.ReadFile(filepath.Join(dest, "Orders.d.ts"))
			if err != nil {
				t.Fatal(err)
			}
			if mode == "offline" {
				if strings.Contains(string(data), "import('./Account')") || !strings.Contains(string(data), `"name": string;`) {
					t.Fatalf("local shadow: %s", data)
				}
				return
			}
			for _, want := range []string{
				"row: import('./Account').Account", "Promise<import('./Account').Account>",
				"rows: import('./Account').Account[]", `"rows": { [key: string]: import('./Contact').Contact[] }`,
				`"missing": unknown`, `"generic": unknown`, `"value": unknown`,
			} {
				if !strings.Contains(string(data), want) {
					t.Errorf("missing %q in %s", want, data)
				}
			}
			account, _ := os.ReadFile(filepath.Join(dest, "Account.d.ts"))
			contact, _ := os.ReadFile(filepath.Join(dest, "Contact.d.ts"))
			if strings.Contains(string(contact), "Unrelated?") {
				t.Fatalf("broadened children: %s", contact)
			}
			wantRootChildren := mode == "configured_both" || mode == "unrestricted"
			if wantRootChildren != strings.Contains(string(account), "Contacts?:") {
				t.Fatalf("configured root children: %s", account)
			}
			if (mode == "configured_both") != strings.Contains(string(contact), "AllowedChildren?:") {
				t.Fatalf("inferred root gained default children: %s", contact)
			}
			wantParent := mode == "configured" || mode == "configured_both" || mode == "wildcard" || mode == "unrestricted"
			if wantParent != strings.Contains(string(contact), "Account?:") {
				t.Fatalf("selector provenance: %s", contact)
			}
		})
	}
}
