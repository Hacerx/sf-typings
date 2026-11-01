package cli

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestParentGraphRun(t *testing.T) {
	for _, mode := range []string{"cycle", "unknown", "malformed", "forbidden", "retry", "transitive"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			cfg := filepath.Join(root, "config.json")
			out := filepath.Join(root, "out")
			if err := os.WriteFile(cfg, []byte(`{"orgAlias":"dev","filterRelatedObjects":false,"objects":["Contact"]}`), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(out, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(out, "Contact.d.ts"), []byte("prior"), 0600); err != nil {
				t.Fatal(err)
			}
			contact := `{"name":"Contact","extra":{"preserved":true},"fields":[{"name":"AccountId","type":"reference","nillable":true,"relationshipName":"Account","referenceTo":["Account"]}],"childRelationships":[{"childSObject":"NeverFetch"}]}`
			account := `{"name":"Account","fields":[{"name":"ContactId","type":"reference","nillable":false,"relationshipName":"Contact","referenceTo":["Contact"]}]}`
			if mode == "transitive" {
				account = `{"name":"Account","fields":[{"name":"OwnerId","type":"reference","nillable":true,"relationshipName":"Owner","referenceTo":["User","User"]},{"name":"IgnoredId","type":"reference","nillable":true,"referenceTo":["NeverFetch"]}]}`
			}
			if mode == "malformed" {
				contact = strings.ReplaceAll(contact, `["Account"]`, `["../Bad"]`)
			}
			if mode == "unknown" {
				account = `{"name":"Wrong","fields":[]}`
			}
			var requests []string
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				p := r.URL.Path
				requests = append(requests, p)
				switch p {
				case "/services/data/":
					io.WriteString(w, `[{"version":"60.0"}]`)
				case "/services/data/v60.0/sobjects/Contact/describe":
					io.WriteString(w, contact)
				case "/services/data/v60.0/sobjects/Account/describe":
					if mode == "forbidden" {
						w.WriteHeader(403)
						return
					}
					if mode == "retry" && r.Header.Get("Authorization") == "Bearer stale" {
						w.WriteHeader(401)
						return
					}
					io.WriteString(w, account)
				case "/services/data/v60.0/sobjects/User/describe":
					io.WriteString(w, `{"name":"User","fields":[{"name":"ManagerId","type":"reference","nillable":true,"relationshipName":"Manager","referenceTo":["User","Contact"]}]}`)
				default:
					t.Errorf("unexpected path %s", p)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			transport := server.Client().Transport
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "sample.my.salesforce.com" {
					t.Errorf("host %s", r.URL.Host)
				}
				c := r.Clone(r.Context())
				c.URL.Host = strings.TrimPrefix(server.URL, "https://")
				return transport.RoundTrip(c)
			})}
			fallbacks := 0
			local := func(context.Context, string) (auth.Credentials, error) {
				return auth.Credentials{Token: "stale", Instance: "https://sample.my.salesforce.com"}, nil
			}
			fallback := func(context.Context, string) (auth.Credentials, error) {
				fallbacks++
				return auth.Credentials{Token: "fresh"}, nil
			}
			var stdout, stderr bytes.Buffer
			code := runWithDeps([]string{"-config", cfg, "-out", out, "-snapshots"}, &stdout, &stderr, local, fallback, client)
			wantRequests := []string{"/services/data/", "/services/data/v60.0/sobjects/Contact/describe"}
			if mode != "malformed" {
				wantRequests = append(wantRequests, "/services/data/v60.0/sobjects/Account/describe")
			}
			if mode == "retry" {
				wantRequests = append(wantRequests, wantRequests...)
			}
			if mode == "transitive" {
				wantRequests = append(wantRequests, "/services/data/v60.0/sobjects/User/describe")
			}
			if !reflect.DeepEqual(requests, wantRequests) {
				t.Errorf("requests=%v want=%v", requests, wantRequests)
			}
			wantFallbacks := 0
			if mode == "retry" {
				wantFallbacks = 1
			}
			if fallbacks != wantFallbacks {
				t.Errorf("fallbacks=%d", fallbacks)
			}
			wantErr := ""
			if mode == "unknown" {
				wantErr = "sf-typings: invalid describe for Account\n"
			}
			if mode == "malformed" {
				wantErr = "sf-typings: invalid describe for Contact\n"
			}
			if mode == "forbidden" {
				wantErr = "sf-typings: REST Describe for Account: REST HTTP 403\n"
			}
			if wantErr != "" {
				if code != 1 || stdout.String() != "" || stderr.String() != wantErr {
					t.Errorf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
				}
				entries, _ := os.ReadDir(out)
				prior, _ := os.ReadFile(filepath.Join(out, "Contact.d.ts"))
				if len(entries) != 1 || string(prior) != "prior" {
					t.Fatal("prior outputs changed")
				}
				return
			}
			wantStdout := "Generated 2 object(s).\n"
			wantFiles := 4
			if mode == "transitive" {
				wantStdout = "Generated 3 object(s).\n"
				wantFiles = 6
			}
			if code != 0 || stdout.String() != wantStdout || stderr.Len() != 0 {
				t.Errorf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			entries, _ := os.ReadDir(out)
			if len(entries) != wantFiles {
				t.Errorf("outputs=%d", len(entries))
			}
			raws := map[string]string{"Contact": contact, "Account": account}
			if mode == "transitive" {
				raws["User"] = `{"name":"User","fields":[{"name":"ManagerId","type":"reference","nillable":true,"relationshipName":"Manager","referenceTo":["User","Contact"]}]}`
			}
			for name, raw := range raws {
				snap, err := os.ReadFile(filepath.Join(out, name+".describe.json"))
				if err != nil {
					t.Error(err)
					continue
				}
				var got, want any
				json.Unmarshal(snap, &got)
				json.Unmarshal([]byte(raw), &want)
				if !reflect.DeepEqual(got, want) {
					t.Error("incomplete snapshot")
				}
				decl, err := os.ReadFile(filepath.Join(out, name+".d.ts"))
				if err != nil || !strings.Contains(string(decl), "import type") {
					t.Errorf("declaration %s: %s %v", name, decl, err)
				}
				if mode == "cycle" || mode == "retry" {
					target, nullable := "Account", " | null"
					if name == "Account" {
						target = "Contact"
						nullable = ""
					}
					want := "// Generated by sf-typings. Do not edit.\nimport type { " + target + " as SFParent1 } from \"./" + target + "\";\n/**\n */\nexport interface " + name + " {\n  /** Parent relationship via " + target + "Id; available when queried. */\n  " + target + "?: SFParent1" + nullable + ";\n  /**\n   * @referenceTo " + target + "\n   * @relationshipName " + target + "\n   * @sfType reference\n   */\n  " + target + "Id?: string" + nullable + ";\n}\n"
					if string(decl) != want {
						t.Errorf("full declaration %s: got %q want %q", name, decl, want)
					}
				}
			}
		})
	}
}
