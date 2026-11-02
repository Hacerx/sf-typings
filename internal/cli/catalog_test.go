package cli

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hacerx/sf-typings/internal/auth"
)

func TestObjectCatalogFailuresAndRetry(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
		retry            bool
	}{
		{name: "malformed", body: `{"sobjects":null}`, want: "sf-typings: invalid REST object catalog\n"},
		{name: "bad identifier", body: `{"sobjects":[{"name":"../Account"}]}`, want: "sf-typings: invalid REST object catalog\n"},
		{name: "forbidden", status: 403, want: "sf-typings: REST object catalog: REST HTTP 403\n"},
		{name: "retry", body: `{"sobjects":[{"name":"AC_A__c"}]}`, retry: true},
		{name: "second unauthorized", status: 401, retry: true, want: "sf-typings: REST object catalog: REST HTTP 401\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := filepath.Join(dir, "config.json")
			outDir := filepath.Join(dir, "out")
			os.Mkdir(outDir, 0700)
			prior := filepath.Join(outDir, "prior.txt")
			os.WriteFile(prior, []byte("original"), 0600)
			os.WriteFile(cfg, []byte(`{"orgAlias":"dev","objects":["AC_*"]}`), 0600)
			fallbacks, requests, catalogs, describes := 0, 0, 0, 0
			local := func(context.Context, string) (auth.Credentials, error) {
				return auth.Credentials{Token: "old", Instance: "https://sample.my.salesforce.com"}, nil
			}
			fallback := func(context.Context, string) (auth.Credentials, error) {
				fallbacks++
				return auth.Credentials{Token: "new"}, nil
			}
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				requests++
				status := 200
				body := `[{"version":"60.0"}]`
				switch r.URL.Path {
				case "/services/data/":
				case "/services/data/v60.0/sobjects/":
					catalogs++
					body = tc.body
					if tc.status != 0 {
						status = tc.status
					}
					if tc.retry && catalogs == 1 {
						status = 401
					}
				case "/services/data/v60.0/sobjects/AC_A__c/describe":
					describes++
					body = `{"name":"AC_A__c","fields":[]}`
				default:
					t.Fatalf("unexpected request %s", r.URL.Path)
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			var stdout, stderr strings.Builder
			code := runWithDeps([]string{"-config", cfg, "-out", outDir}, &stdout, &stderr, local, fallback, client)
			wantFallbacks, wantCatalogs := 0, 1
			if tc.retry {
				wantFallbacks = 1
				wantCatalogs = 2
			}
			if fallbacks != wantFallbacks || catalogs != wantCatalogs {
				t.Fatalf("fallbacks=%d catalogs=%d", fallbacks, catalogs)
			}
			entries, _ := os.ReadDir(outDir)
			assertUnchanged(t, prior)
			if tc.want != "" {
				if code != 1 || stdout.String() != "" || stderr.String() != tc.want || describes != 0 || len(entries) != 1 || requests != wantCatalogs*2 {
					t.Fatalf("exit=%d stdout=%q stderr=%q describes=%d requests=%d files=%v", code, stdout.String(), stderr.String(), describes, requests, entries)
				}
			} else if code != 0 || stdout.String() != "Generated 1 object(s).\n" || stderr.String() != "" || describes != 1 || len(entries) != 2 || requests != 5 {
				t.Fatalf("exit=%d stdout=%q stderr=%q describes=%d requests=%d files=%v", code, stdout.String(), stderr.String(), describes, requests, entries)
			}
		})
	}
}
