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

func TestChildDepthRun(t *testing.T) {
	for _, mode := range []string{"flag", "config", "default", "override", "parent", "shortcut", "queued shortcut", "root", "cycles", "retry", "failure", "retry failure", "boundary union"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			dest := filepath.Join(dir, "out", "sobjects")
			if err := os.MkdirAll(dest, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dest, "N1.d.ts"), []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			metadata := map[string]map[string]any{}
			for i := 1; i <= 6; i++ {
				name := fmt.Sprintf("N%d", i)
				m := map[string]any{"name": name, "fields": []any{}}
				if i < 6 {
					m["childRelationships"] = []any{map[string]any{"relationshipName": "Children", "childSObject": fmt.Sprintf("N%d", i+1)}}
				}
				metadata[name] = m
			}
			parent := func(name, target string) {
				metadata[name]["fields"] = []any{map[string]any{"name": "ParentId", "type": "reference", "nillable": true, "relationshipName": "Children", "referenceTo": []string{target}}}
			}
			roots := []string{"N1"}
			want := []string{"N1", "N2", "N3", "N4", "N5"}
			switch mode {
			case "default", "override":
				want = []string{"N1", "N2"}
			case "parent":
				metadata["N4"]["childRelationships"] = []any{}
				parent("N4", "Bridge")
				metadata["Bridge"] = map[string]any{"name": "Bridge", "fields": []any{}, "childRelationships": []any{map[string]any{"relationshipName": "Children", "childSObject": "N5"}}}
				want = []string{"N1", "N2", "N3", "N4", "Bridge", "N5"}
			case "shortcut":
				parent("N1", "Bridge")
				metadata["Bridge"] = map[string]any{"name": "Bridge", "fields": []any{}, "childRelationships": []any{map[string]any{"relationshipName": "Children", "childSObject": "N4"}}}
				want = []string{"N1", "Bridge", "N2", "N4", "N3", "N5", "N6"}
			case "queued shortcut":
				metadata["N1"]["childRelationships"] = []any{map[string]any{"relationshipName": "First", "childSObject": "N2"}, map[string]any{"relationshipName": "Second", "childSObject": "N3"}}
				metadata["N2"]["childRelationships"] = []any{map[string]any{"relationshipName": "Children", "childSObject": "N4"}}
				parent("N3", "N4")
				want = []string{"N1", "N2", "N3", "N4", "N5", "N6"}
			case "root":
				roots = append(roots, "N4")
				want = []string{"N1", "N4", "N2", "N5", "N3", "N6"}
			case "cycles":
				metadata["N3"]["childRelationships"] = []any{map[string]any{"relationshipName": "Self", "childSObject": "N3"}, map[string]any{"relationshipName": "Back", "childSObject": "N1"}}
				want = []string{"N1", "N2", "N3"}
			case "boundary union":
				parent("N5", "Parent")
				metadata["Parent"] = map[string]any{"name": "Parent", "fields": []any{}, "childRelationships": []any{map[string]any{"relationshipName": "Children", "childSObject": "N6"}}}
				want = append(want, "Parent")
			case "failure", "retry failure":
				metadata["N4"]["childRelationships"] = []any{map[string]any{"relationshipName": "Bad", "childSObject": "../Bad"}}
				want = []string{"N1", "N2", "N3", "N4"}
			}
			var requests []string
			versions, retries := 0, 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/services/data/" {
					versions++
					io.WriteString(w, `[{"version":"60.0"}]`)
					return
				}
				name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/services/data/v60.0/sobjects/"), "/describe")
				requests = append(requests, name)
				if strings.HasPrefix(mode, "retry") && name == "N4" && retries == 0 {
					retries++
					w.WriteHeader(401)
					return
				}
				m, ok := metadata[name]
				if !ok {
					t.Errorf("unexpected %s", name)
					w.WriteHeader(404)
					return
				}
				json.NewEncoder(w).Encode(m)
			}))
			defer server.Close()
			transport := server.Client().Transport
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				clone := r.Clone(r.Context())
				clone.URL.Host = strings.TrimPrefix(server.URL, "https://")
				return transport.RoundTrip(clone)
			})}
			resolver := func(context.Context, string) (auth.Credentials, error) {
				return auth.Credentials{Token: "test", Instance: "https://sample.my.salesforce.com"}, nil
			}
			cfg := filepath.Join(dir, "config.json")
			raw, _ := json.Marshal(map[string]any{"orgAlias": "dev", "filterRelatedObjects": false, "objects": roots, "out": filepath.Dir(dest), "allChildren": mode != "flag" && mode != "default", "snapshots": true})
			if err := os.WriteFile(cfg, raw, 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"-config", cfg}
			if mode == "flag" {
				args = append(args, "-all-children")
			}
			if mode == "override" {
				args = append(args, "-all-children=false")
			}
			var out, errs strings.Builder
			code := runWithDeps(args, &out, &errs, resolver, resolver, client)
			expectedRequests := want
			expectedVersions := 1
			if strings.HasPrefix(mode, "retry") {
				expectedRequests = append([]string{"N1", "N2", "N3", "N4"}, want...)
				expectedVersions = 2
			}
			if !reflect.DeepEqual(requests, expectedRequests) || versions != expectedVersions {
				t.Fatalf("requests=%v want=%v versions=%d stderr=%s", requests, expectedRequests, versions, errs.String())
			}
			if mode == "failure" || mode == "retry failure" {
				if code != 1 || out.String() != "" || errs.String() != "sf-typings: invalid describe for N4\n" {
					t.Fatalf("%d %q %q", code, out.String(), errs.String())
				}
				assertUnchanged(t, filepath.Join(dest, "N1.d.ts"))
				entries, _ := os.ReadDir(dest)
				if len(entries) != 1 {
					t.Fatal("partial publication")
				}
				return
			}
			if code != 0 || errs.String() != "" || out.String() != fmt.Sprintf("Generated %d object(s).\n", len(want)) {
				t.Fatalf("%d %q %q", code, out.String(), errs.String())
			}
			entries, _ := os.ReadDir(dest)
			if len(entries) != 2*len(want) {
				t.Fatalf("files=%d", len(entries))
			}
			for _, name := range want {
				data, err := os.ReadFile(filepath.Join(dest, name+".describe.json"))
				if err != nil {
					t.Fatal(err)
				}
				var got any
				json.Unmarshal(data, &got)
				original, _ := json.Marshal(metadata[name])
				var expected any
				json.Unmarshal(original, &expected)
				if !reflect.DeepEqual(got, expected) {
					t.Fatal("snapshot changed")
				}
			}
			if mode == "flag" || mode == "config" || mode == "parent" || mode == "retry" || mode == "boundary union" {
				data, _ := os.ReadFile(filepath.Join(dest, "N5.d.ts"))
				if strings.Contains(string(data), "N6") || strings.Contains(string(data), "[]") {
					t.Fatalf("boundary child emitted: %s", data)
				}
				if mode == "boundary union" && !strings.Contains(string(data), "Children?: SFParent1 | null;") {
					t.Fatalf("parent union lost: %s", data)
				}
			}
		})
	}
}
