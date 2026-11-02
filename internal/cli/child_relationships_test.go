package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"sf-typings/internal/auth"
)

func TestChildGraphRunWithDeps(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		flags                             []string
		recursive, snapshots, rootContact bool
		bad, badObject, children          string
		retry                             bool
		want                              []string
	}{
		{name: "default", want: []string{"Account", "Contact", "User", "Team"}},
		{name: "merged child fields", snapshots: true, children: `[{"relationshipName":"Contacts","childSObject":"Contact","field":"RelatedRecordId"},{"relationshipName":"Contacts","childSObject":"Contact","field":"AccountId"},{"relationshipName":"Contacts","childSObject":"Contact","field":"AccountId"},{"relationshipName":"ChildAccounts","childSObject":"Account"}]`, want: []string{"Account", "Contact", "User", "Team"}},
		{name: "conflicting child targets", bad: `[{"relationshipName":"Contacts","childSObject":"Contact"},{"relationshipName":"Contacts","childSObject":"User"}]`, want: []string{"Account"}},
		{name: "recursive flag", flags: []string{"-all-children", "-snapshots"}, want: []string{"Account", "Contact", "User", "Team", "Note", "Extra"}},
		{name: "recursive JSON", recursive: true, snapshots: true, want: []string{"Account", "Contact", "User", "Team", "Note", "Extra"}},
		{name: "explicit false", recursive: true, snapshots: true, flags: []string{"-all-children=false", "-snapshots=false"}, want: []string{"Account", "Contact", "User", "Team"}},
		{name: "configured child root", rootContact: true, want: []string{"Account", "Contact", "User", "Team", "Note"}},
		{name: "retry child graph", retry: true, want: []string{"Account", "Contact", "User", "Team"}},
		{name: "target injection", bad: `[{"relationshipName":"Contacts","childSObject":"Contact"},{"relationshipName":"Bad","childSObject":"Contact/../../User"}]`, want: []string{"Account"}},
		{name: "parent child union", snapshots: true, children: `[{"relationshipName":"Contacts","childSObject":"Contact"},{"relationshipName":"ChildAccounts","childSObject":"Account"},{"relationshipName":"Customer","childSObject":"Account","field":"CustomerId"}]`, want: []string{"Account", "Contact", "User", "Team"}},
		{name: "scalar collision", bad: `[{"relationshipName":"OwnerId","childSObject":"Contact"}]`, want: []string{"Account"}},
		{name: "late child failure", recursive: true, snapshots: true, badObject: "Contact", bad: `[{"relationshipName":"Notes","childSObject":"../Note"}]`, want: []string{"Account", "Contact"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "output", "sobjects")
			if err := os.MkdirAll(dest, 0700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"Account.d.ts", "Account.describe.json"} {
				if err := os.WriteFile(filepath.Join(dest, name), []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			objects := []string{"Account"}
			if tc.rootContact {
				objects = append(objects, "Contact")
			}
			config := map[string]any{"orgAlias": "dev", "filterRelatedObjects": false, "objects": objects, "out": filepath.Dir(dest), "allChildren": tc.recursive, "snapshots": tc.snapshots}
			raw, _ := json.Marshal(config)
			cfg := filepath.Join(root, "config.json")
			if err := os.WriteFile(cfg, raw, 0600); err != nil {
				t.Fatal(err)
			}
			accountChildren := `[{"relationshipName":"Contacts","childSObject":"Contact"},{"relationshipName":"ChildAccounts","childSObject":"Account"},{"relationshipName":null,"childSObject":"Event"},{"relationshipName":"Old","childSObject":"OldEvent","deprecatedAndHidden":true}]`
			if tc.children != "" {
				accountChildren = tc.children
			}
			if tc.bad != "" && tc.badObject == "" {
				accountChildren = tc.bad
			}
			metadata := map[string]string{
				"Account": `{"name":"Account","fields":[{"name":"OwnerId","type":"reference","nillable":false,"relationshipName":"Owner","referenceTo":["User"]},{"name":"CustomerId","type":"reference","nillable":true,"relationshipName":"Customer","referenceTo":["Contact"]}],"childRelationships":` + accountChildren + `}`,
				"Contact": `{"name":"Contact","fields":[{"name":"AccountId","type":"reference","nillable":true,"relationshipName":"Account","referenceTo":["Account"]}],"childRelationships":[{"relationshipName":"Notes","childSObject":"Note"}]}`,
				"User":    `{"name":"User","fields":[{"name":"TeamId","type":"reference","nillable":false,"relationshipName":"Team","referenceTo":["Team"]}],"childRelationships":[{"relationshipName":"Extras","childSObject":"Extra"}]}`,
				"Extra":   `{"name":"Extra","fields":[]}`,
				"Team":    `{"name":"Team","fields":[]}`,
				"Note":    `{"name":"Note","fields":[],"childRelationships":[{"relationshipName":"Contacts","childSObject":"Contact"}]}`,
			}
			if tc.badObject == "Contact" {
				metadata["Contact"] = `{"name":"Contact","fields":[],"childRelationships":` + tc.bad + `}`
			}
			var requests []string
			versions, fallbacks := 0, 0
			failed := false
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/services/data/" {
					versions++
					io.WriteString(w, `[{"version":"60.0"}]`)
					return
				}
				name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/services/data/v60.0/sobjects/"), "/describe")
				requests = append(requests, name)
				if tc.retry && name == "Contact" && !failed {
					failed = true
					w.WriteHeader(401)
					return
				}
				body, ok := metadata[name]
				if !ok {
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				io.WriteString(w, body)
			}))
			defer server.Close()
			transport := server.Client().Transport
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "sample.my.salesforce.com" || r.Method != "GET" || (r.Header.Get("Authorization") != "Bearer local" && r.Header.Get("Authorization") != "Bearer fresh") {
					t.Errorf("unexpected request %s", r.URL)
				}
				clone := r.Clone(r.Context())
				clone.URL.Host = strings.TrimPrefix(server.URL, "https://")
				return transport.RoundTrip(clone)
			})}
			local := func(context.Context, string) (auth.Credentials, error) {
				return auth.Credentials{"local", "https://sample.my.salesforce.com"}, nil
			}
			fallback := func(context.Context, string) (auth.Credentials, error) {
				fallbacks++
				return auth.Credentials{Token: "fresh"}, nil
			}
			var out, errs strings.Builder
			args := append([]string{"-config", cfg}, tc.flags...)
			code := runWithDeps(args, &out, &errs, local, fallback, client)
			wantRequests := tc.want
			wantVersions := 1
			if tc.retry {
				wantRequests = append([]string{"Account", "Contact"}, tc.want...)
				wantVersions = 2
			}
			if !reflect.DeepEqual(requests, wantRequests) || versions != wantVersions || fallbacks != wantVersions-1 {
				t.Fatalf("requests=%v versions=%d fallback=%d stderr=%q", requests, versions, fallbacks, errs.String())
			}
			if tc.bad != "" {
				invalidObject := tc.badObject
				if invalidObject == "" {
					invalidObject = "Account"
				}
				if code != 1 || out.String() != "" || errs.String() != "sf-typings: invalid describe for "+invalidObject+"\n" {
					t.Fatalf("exit=%d out=%q err=%q", code, out.String(), errs.String())
				}
				for _, name := range []string{"Account.d.ts", "Account.describe.json"} {
					assertUnchanged(t, filepath.Join(dest, name))
				}
				entries, _ := os.ReadDir(dest)
				if len(entries) != 2 {
					t.Fatal("partial output")
				}
				return
			}
			if code != 0 || out.String() != fmt.Sprintf("Generated %d object(s).\n", len(tc.want)) || errs.String() != "" {
				t.Fatalf("exit=%d out=%q err=%q", code, out.String(), errs.String())
			}
			recursive := tc.recursive || tc.name == "recursive flag"
			snapshots := tc.snapshots || tc.name == "recursive flag"
			if tc.name == "explicit false" {
				recursive = false
				snapshots = false
			}
			for _, name := range tc.want {
				data, err := os.ReadFile(filepath.Join(dest, name+".d.ts"))
				if err != nil {
					t.Fatal(err)
				}
				if name == "Contact" && strings.Contains(string(data), "Notes?:") != (recursive || tc.rootContact) {
					t.Fatalf("Contact=%s", data)
				}
				if name == "User" && strings.Contains(string(data), "Extras?:") != recursive {
					t.Fatalf("User=%s", data)
				}
				if snapshots {
					data, err := os.ReadFile(filepath.Join(dest, name+".describe.json"))
					if err != nil || !json.Valid(data) {
						t.Fatalf("snapshot %s: %s %v", name, data, err)
					}
					var original, snapshot any
					if err := json.Unmarshal([]byte(metadata[name]), &original); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal(data, &snapshot); err != nil || !reflect.DeepEqual(original, snapshot) {
						t.Fatalf("changed snapshot %s: %v", name, err)
					}
				} else if name == "Account" {
					assertUnchanged(t, filepath.Join(dest, "Account.describe.json"))
				} else if _, err := os.Stat(filepath.Join(dest, name+".describe.json")); !os.IsNotExist(err) {
					t.Fatalf("unexpected snapshot %s", name)
				}
			}
			account, _ := os.ReadFile(filepath.Join(dest, "Account.d.ts"))
			if tc.name == "merged child fields" && (strings.Count(string(account), "Contacts?:") != 1 || !strings.Contains(string(account), "Contacts?: SFParent1[];") || !strings.Contains(string(account), "Child relationship via AccountId, RelatedRecordId; available when queried.")) {
				t.Fatalf("merged Account=%s", account)
			}
			if tc.name == "parent child union" {
				for _, want := range []string{"Customer?: SFParent1 | Account[] | null;", "Parent relationship via CustomerId; available when queried.", "Child relationship via CustomerId; available when queried.", "CustomerId?: string | null;"} {
					if !strings.Contains(string(account), want) {
						t.Fatalf("missing %q in %s", want, account)
					}
				}
				if strings.Count(string(account), "Customer?:") != 1 {
					t.Fatalf("duplicate union property: %s", account)
				}
			}
			if !strings.Contains(string(account), "Contacts?:") || !strings.Contains(string(account), "Owner?:") || !strings.Contains(string(account), "ChildAccounts?: Account[];") {
				t.Fatalf("Account=%s", account)
			}
		})
	}
}
