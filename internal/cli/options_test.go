package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hacerx/sf-typings/internal/auth"
)

func TestExecutionOptions(t *testing.T) {
	for _, tc := range []struct {
		name      string
		flags     []string
		config    map[string]any
		wantCode  int
		wantError string
		wantCLI   bool
	}{
		{name: "config output", config: map[string]any{}},
		{name: "explicit false", flags: []string{"-snapshots=false", "-all-children=false"}, config: map[string]any{"snapshots": true, "allChildren": true}},
		{name: "output override", flags: []string{"-out", "CLI"}, config: map[string]any{}, wantCLI: true},
		{name: "empty output override", flags: []string{"-out", ""}, config: map[string]any{}, wantCode: 1, wantError: "arguments"},
		{name: "wrong bool", config: map[string]any{"snapshots": "true"}, wantCode: 1, wantError: "sf-typings: invalid config JSON\n"},
		{name: "null option", config: map[string]any{"allChildren": nil}, wantCode: 1, wantError: "sf-typings: invalid config JSON\n"},
		{name: "bootstrap rejected", config: map[string]any{"help": true}, wantCode: 1, wantError: "sf-typings: invalid config JSON\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "Orders.cls")
			if err := os.WriteFile(source, []byte("public class Orders { @AuraEnabled public static void go() {} }"), 0600); err != nil {
				t.Fatal(err)
			}
			cfg := map[string]any{"out": filepath.Join(root, "config-out"), "apexSources": []string{source}}
			for k, v := range tc.config {
				cfg[k] = v
			}
			raw, _ := json.Marshal(cfg)
			path := filepath.Join(root, "config.json")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"-config", path}
			for _, v := range tc.flags {
				if v == "CLI" {
					v = filepath.Join(root, "cli-out")
				}
				args = append(args, v)
			}
			calls := 0
			resolver := func(context.Context, string) (auth.Credentials, error) { calls++; return auth.Credentials{}, nil }
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected request"); return nil, nil })}
			var out, errs strings.Builder
			code := runWithDeps(args, &out, &errs, resolver, resolver, client)
			if code != tc.wantCode || calls != 0 {
				t.Fatalf("exit=%d auth=%d stderr=%q", code, calls, errs.String())
			}
			if tc.wantCode != 0 {
				if out.String() != "" || (tc.wantError == "arguments" && !strings.HasPrefix(errs.String(), "sf-typings: invalid arguments\nUsage:")) || (tc.wantError != "arguments" && errs.String() != tc.wantError) {
					t.Fatalf("stdout=%q stderr=%q", out.String(), errs.String())
				}
				for _, dir := range []string{"config-out", "cli-out"} {
					if _, err := os.Stat(filepath.Join(root, dir)); !os.IsNotExist(err) {
						t.Fatalf("unexpected output %s", dir)
					}
				}
				return
			}
			if out.String() != "Generated 0 object(s), 1 Apex class(es).\n" || errs.String() != "" {
				t.Fatalf("stdout=%q stderr=%q", out.String(), errs.String())
			}
			dir := "config-out"
			if tc.wantCLI {
				dir = "cli-out"
			}
			if _, err := os.Stat(filepath.Join(root, dir, "apex", "Orders.d.ts")); err != nil {
				t.Fatal(err)
			}
		})
	}
}
