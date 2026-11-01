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
	"time"

	"sf-typings/internal/auth"
)

func timeoutConfig(t *testing.T, options map[string]any) (string, string) {
	t.Helper()
	root := t.TempDir()
	dest := filepath.Join(root, "generated")
	if err := os.Mkdir(dest, 0700); err != nil {
		t.Fatal(err)
	}
	prior := filepath.Join(dest, "Account.d.ts")
	if err := os.WriteFile(prior, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{"orgAlias": "dev", "filterRelatedObjects": false, "objects": []string{"Account"}, "out": dest}
	for k, v := range options {
		cfg[k] = v
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path, prior
}

func TestTimeoutResolution(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options map[string]any
		flags   []string
		want    time.Duration
	}{
		{name: "default", want: 35 * time.Second},
		{name: "config", options: map[string]any{"timeout": "2m"}, want: 2 * time.Minute},
		{name: "flag", flags: []string{"-timeout", "5m"}, want: 5 * time.Minute},
		{name: "override", options: map[string]any{"timeout": "2m"}, flags: []string{"-timeout", "5m"}, want: 5 * time.Minute},
		{name: "case folded config", options: map[string]any{"TiMeOuT": "1m30s"}, want: 90 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, _ := timeoutConfig(t, tc.options)
			calls := 0
			resolver := func(ctx context.Context, _ string) (auth.Credentials, error) {
				calls++
				deadline, ok := ctx.Deadline()
				remaining := time.Until(deadline)
				if !ok || remaining > tc.want || remaining < tc.want-time.Second {
					t.Errorf("deadline remaining=%v want=%v", remaining, tc.want)
				}
				return auth.Credentials{}, fmt.Errorf("stop before HTTP")
			}
			// A trusted instance with no token exercises fallback without invoking sf.
			local := func(ctx context.Context, alias string) (auth.Credentials, error) {
				resolver(ctx, alias)
				return auth.Credentials{Instance: "https://unit.my.salesforce.com"}, nil
			}
			var out, errs strings.Builder
			code := runWithDeps(append([]string{"-config", path}, tc.flags...), &out, &errs, local, resolver, nil)
			if code != 1 || calls != 2 || out.String() != "" || errs.String() != "sf-typings: org token unavailable\n" {
				t.Fatalf("exit=%d calls=%d stdout=%q stderr=%q", code, calls, out.String(), errs.String())
			}
		})
	}
}

func TestInvalidTimeoutPreservesOutput(t *testing.T) {
	const invalid = "sf-typings: invalid timeout (use a positive Go duration, e.g. 5m)\n"
	for _, value := range []string{"", "0", "0s", "-1s", "5", "oops", "999999999999999999999h"} {
		for _, source := range []string{"config", "flag"} {
			t.Run(source+"/"+value, func(t *testing.T) {
				options := map[string]any{}
				var flags []string
				if source == "config" {
					options["timeout"] = value
				} else {
					flags = []string{"-timeout", value}
				}
				checkInvalidTimeout(t, options, flags, invalid)
			})
		}
	}
	for _, value := range []any{nil, 15, true, []string{"5m"}, map[string]any{"duration": "5m"}} {
		for _, key := range []string{"timeout", "TiMeOuT"} {
			t.Run(fmt.Sprint(key, value), func(t *testing.T) {
				checkInvalidTimeout(t, map[string]any{key: value}, nil, "sf-typings: invalid config JSON\n")
			})
		}
	}
}

func checkInvalidTimeout(t *testing.T, options map[string]any, flags []string, want string) {
	t.Helper()
	path, prior := timeoutConfig(t, options)
	calls := 0
	resolver := func(context.Context, string) (auth.Credentials, error) { calls++; return auth.Credentials{}, nil }
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected HTTP"); return nil, nil })}
	var out, errs strings.Builder
	code := runWithDeps(append([]string{"-config", path}, flags...), &out, &errs, resolver, resolver, client)
	if code != 1 || calls != 0 || out.String() != "" || errs.String() != want {
		t.Fatalf("exit=%d auth=%d stdout=%q stderr=%q", code, calls, out.String(), errs.String())
	}
	assertUnchanged(t, prior)
	entries, err := os.ReadDir(filepath.Dir(prior))
	if err != nil || len(entries) != 1 {
		t.Fatalf("outputs=%v err=%v", entries, err)
	}
}

func TestTimeoutSharedAcrossGraphAndRetry(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(fmt.Sprint("retry=", retry), func(t *testing.T) {
			path, prior := timeoutConfig(t, map[string]any{"timeout": "100ms"})
			var deadline time.Time
			check := func(ctx context.Context) {
				t.Helper()
				got, ok := ctx.Deadline()
				if !ok || !got.Equal(deadline) {
					t.Fatalf("deadline=%v want=%v", got, deadline)
				}
			}
			local := func(ctx context.Context, _ string) (auth.Credentials, error) {
				var ok bool
				deadline, ok = ctx.Deadline()
				if !ok || time.Until(deadline) > 100*time.Millisecond {
					t.Fatal("missing custom budget")
				}
				return auth.Credentials{Instance: "https://unit.my.salesforce.com", Token: "local"}, nil
			}
			fallbacks := 0
			fallback := func(ctx context.Context, _ string) (auth.Credentials, error) {
				check(ctx)
				fallbacks++
				return auth.Credentials{Token: "fresh"}, nil
			}
			requests := 0
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				check(req.Context())
				requests++
				status := 200
				body := `[{"version":"60.0"}]`
				if strings.HasSuffix(req.URL.Path, "/Account/describe") {
					if retry && req.Header.Get("Authorization") == "Bearer local" {
						status = 401
						body = `{}`
					} else {
						body = `{"name":"Account","fields":[],"childRelationships":[{"relationshipName":"Contacts","childSObject":"Contact"}]}`
					}
				}
				if strings.HasSuffix(req.URL.Path, "/Contact/describe") {
					<-req.Context().Done()
					return nil, req.Context().Err()
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			var out, errs strings.Builder
			code := runWithDeps([]string{"-config", path}, &out, &errs, local, fallback, client)
			wantRequests, wantFallbacks := 3, 0
			if retry {
				wantRequests, wantFallbacks = 5, 1
			}
			if code != 1 || out.String() != "" || errs.String() != "sf-typings: REST Describe for Contact: REST network request failed\n" || requests != wantRequests || fallbacks != wantFallbacks {
				t.Fatalf("exit=%d stdout=%q stderr=%q requests=%d fallbacks=%d", code, out.String(), errs.String(), requests, fallbacks)
			}
			assertUnchanged(t, prior)
		})
	}
}
