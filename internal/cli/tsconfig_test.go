package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"sf-typings/internal/auth"
)

const initUsage = "Usage: sf-typings -init-tsconfig [-config config.json] [-out generated]\n"
const initHelp = "TypeScript setup: run -init-tsconfig from the SFDX project root to create tsconfig.json or enrich an existing regular JSONC config with chosen out declarations, preserving source selection and comments. Prints Created, Updated, or Unchanged; unsafe/unsupported configs fail without changes. Explicit root include preserves opaque string/array extends; otherwise supports single project-local JSON extends chains with explicit include/files, not package inheritance. No references-only builds. New configs include packageDirectories paths. Never follows target symlinks. Only -init-tsconfig/-config/-out/-help are accepted. No generation, authentication, installs, or deployment. Install TypeScript and @salesforce/lightning-types yourself: npm install --save-dev typescript @salesforce/lightning-types. Check with npx tsc --noEmit; compile separately before deployment. See examples/sfdx-typescript/README.md. -init-tsconfig is a bootstrap flag, not a JSON option.\n"

func TestInitTSConfigChild(t *testing.T) {
	if os.Getenv("SF_TYPINGS_INIT_CHILD") != "1" {
		return
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			args = args[i+1:]
			break
		}
	}
	calls, requests := 0, 0
	resolver := func(context.Context, string) (auth.Credentials, error) {
		calls++
		return auth.Credentials{}, nil
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, fmt.Errorf("unexpected network")
	})}
	code := runWithDeps(args, os.Stdout, os.Stderr, resolver, resolver, client)
	if calls != 0 || requests != 0 {
		fmt.Fprintf(os.Stderr, "auth=%d network=%d\n", calls, requests)
		os.Exit(99)
	}
	os.Exit(code)
}

func initRun(t *testing.T, root string, args ...string) (string, string, int) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, append([]string{"-test.run=^TestInitTSConfigChild$", "--"}, args...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SF_TYPINGS_INIT_CHILD=1", "HOME="+root, "USERPROFILE="+root)
	var out, errs strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errs
	code := 0
	if err := cmd.Run(); err != nil {
		e, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatal(err)
		}
		code = e.ExitCode()
	}
	return out.String(), errs.String(), code
}

// Snapshot bytes and entry kinds without following links outside the fixture.
func initSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		switch {
		case entry.Type()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			result[rel] = "link:" + link
		case entry.IsDir():
			result[rel] = "dir"
		default:
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			result[rel] = "file:" + string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestInitTSConfig(t *testing.T) {
	for _, tc := range []struct {
		name, cfg, out string
		flags          []string
	}{
		{name: "standalone default", out: "sf-typings"},
		{name: "explicit output", out: "chosen", flags: []string{"-out", "chosen"}},
		{name: "configured output", cfg: `{"apexSources":["unreadable.cls"],"out":"custom"}`, out: "custom", flags: []string{"-config", "config.json"}},
		{name: "override configured output", cfg: `{"objects":["Account"],"orgAlias":"dev","out":"custom"}`, out: "chosen", flags: []string{"-config", "config.json", "-out", "chosen"}},
		{name: "override empty output", cfg: `{"apexSources":["missing.cls"],"out":""}`, out: "chosen", flags: []string{"-config", "config.json", "-out", "chosen"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeLayoutFile(t, filepath.Join(root, "sfdx-project.json"), `{"packageDirectories":[{"path":"force-app","default":true},{"path":"packages/extra"},{"path":"force-app"}],"sourceApiVersion":"64.0"}`)
			for _, p := range []string{"force-app", "packages/extra"} {
				if err := os.MkdirAll(filepath.Join(root, p), 0700); err != nil {
					t.Fatal(err)
				}
			}
			writeLayoutFile(t, filepath.Join(root, "package.json"), "original")
			if tc.cfg != "" {
				writeLayoutFile(t, filepath.Join(root, "config.json"), tc.cfg)
			}
			before := initSnapshot(t, root)
			out, errs, code := initRun(t, root, append([]string{"-init-tsconfig"}, tc.flags...)...)
			if code != 0 || out != "Created tsconfig.json.\n" || errs != "" {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, out, errs)
			}
			data, err := os.ReadFile(filepath.Join(root, "tsconfig.json"))
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]any{
				"compilerOptions": map[string]any{
					"target": "ESNext", "module": "ESNext", "moduleResolution": "Bundler",
					"strict": true, "noEmit": true, "types": []any{"@salesforce/lightning-types"},
				},
				"include": []any{"force-app/**/*.ts", "packages/extra/**/*.ts", tc.out + "/**/*.d.ts"},
				"exclude": []any{"node_modules"},
			}
			var got any
			if err := json.Unmarshal(data, &got); err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("template=%s err=%v", data, err)
			}
			after := initSnapshot(t, root)
			delete(after, "tsconfig.json")
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("unexpected changes: before=%v after=%v", before, after)
			}
			// Second requests preserve even the exact generated formatting.
			before = initSnapshot(t, root)
			out, errs, code = initRun(t, root, append([]string{"-init-tsconfig"}, tc.flags...)...)
			if code != 0 || out != "Unchanged tsconfig.json.\n" || errs != "" || !reflect.DeepEqual(before, initSnapshot(t, root)) {
				t.Fatalf("repeat: exit=%d stdout=%q stderr=%q", code, out, errs)
			}
		})
	}
}

func TestInitTSConfigHelpAndGenerationPreserved(t *testing.T) {
	root := t.TempDir()
	before := initSnapshot(t, root)
	out, errs, code := initRun(t, root, "-init-tsconfig", "-help")
	plainOut, plainErrs, plainCode := initRun(t, root, "-help")
	if code != 0 || errs != "" || out != plainOut || plainErrs != "" || plainCode != 0 || !strings.Contains(out, initUsage+initHelp) {
		t.Fatalf("help: exit=%d stdout=%q stderr=%q", code, out, errs)
	}
	if !reflect.DeepEqual(before, initSnapshot(t, root)) {
		t.Fatal("help changed filesystem")
	}
	writeLayoutFile(t, filepath.Join(root, "Orders.cls"), "public class Orders { @AuraEnabled public static String fetch() { return null; } }")
	writeLayoutFile(t, filepath.Join(root, "config.json"), `{"apexSources":["Orders.cls"]}`)
	before = initSnapshot(t, root)
	out, errs, code = initRun(t, root, "-config", "config.json")
	if code != 0 || out != "Generated 0 object(s), 1 Apex class(es).\n" || errs != "" {
		t.Fatalf("generate: exit=%d stdout=%q stderr=%q", code, out, errs)
	}
	before["sf-typings"] = "dir"
	before[filepath.Join("sf-typings", "apex")] = "dir"
	before[filepath.Join("sf-typings", "apex", "Orders.d.ts")] = "file:// Generated by sf-typings. Do not edit.\ndeclare module '@salesforce/apex/Orders.fetch' {\n  export default function fetch(): Promise<string>;\n}\n"
	if !reflect.DeepEqual(before, initSnapshot(t, root)) {
		t.Fatal("regular generation changed bytes or files unexpectedly")
	}
}

func TestInitTSConfigRejected(t *testing.T) {
	for _, tc := range []struct {
		name, project, cfg, target, message string
		flags                               []string
	}{
		{name: "missing project", message: "cannot read sfdx-project.json"},
		{name: "malformed project", project: `{`, message: "invalid sfdx-project.json"},
		{name: "trailing JSON", project: `{} {}`, message: "invalid sfdx-project.json"},
		{name: "no packages", project: `{}`, message: "invalid package directory path"},
		{name: "null packages", project: `{"packageDirectories":null}`, message: "invalid package directory path"},
		{name: "missing path", project: `{"packageDirectories":[{}]}`, message: "invalid package directory path"},
		{name: "null path", project: `{"packageDirectories":[{"path":null}]}`, message: "invalid package directory path"},
		{name: "wrong path type", project: `{"packageDirectories":[{"path":42}]}`, message: "invalid sfdx-project.json"},
		{name: "traversal", project: `{"packageDirectories":[{"path":"../outside"}]}`, message: "invalid package directory path"},
		{name: "absolute", project: `{"packageDirectories":[{"path":"/outside"}]}`, message: "invalid package directory path"},
		{name: "windows path", project: `{"packageDirectories":[{"path":"C:\\outside"}]}`, message: "invalid package directory path"},
		{name: "glob", project: `{"packageDirectories":[{"path":"force-*"}]}`, message: "invalid package directory path"},
		{name: "missing directory", project: `{"packageDirectories":[{"path":"missing"}]}`, message: "invalid package directory path"},
		{name: "package symlink", target: "package-link", message: "invalid package directory path"},
		{name: "existing regular", target: "file", message: "cannot safely update tsconfig.json (invalid or unsupported config)"},
		{name: "existing directory", target: "dir", message: "tsconfig.json already exists"},
		{name: "existing symlink", target: "link", message: "tsconfig.json already exists"},
		{name: "existing dangling symlink", target: "dangling", message: "tsconfig.json already exists"},
		{name: "bad config", cfg: `{"apexSources":["missing.cls"],"unknown":true}`, flags: []string{"-config", "config.json"}, message: "invalid config JSON"},
		{name: "bootstrap JSON rejected", cfg: `{"apexSources":["missing.cls"],"init-tsconfig":true}`, flags: []string{"-config", "config.json"}, message: "invalid config JSON"},
		{name: "invalid config cannot be repaired", cfg: `{"apexSources":["missing.cls"],"out":null}`, flags: []string{"-config", "config.json", "-out", "chosen"}, message: "invalid config JSON"},
		{name: "empty config flag", flags: []string{"-config", ""}, message: "cannot read config"},
		{name: "empty out", flags: []string{"-out", ""}, message: "invalid output directory"},
		{name: "configured empty out", cfg: `{"apexSources":["missing.cls"],"out":""}`, flags: []string{"-config", "config.json"}, message: "invalid output directory"},
		{name: "conflicting flag", flags: []string{"-only", "apex"}, message: "invalid arguments"},
		{name: "explicit false conflicts", flags: []string{"-snapshots=false"}, message: "invalid arguments"},
		{name: "timeout conflicts", flags: []string{"-timeout", "5m"}, message: "invalid arguments"},
		{name: "children conflicts", flags: []string{"-all-children"}, message: "invalid arguments"},
		{name: "filter conflicts", flags: []string{"-filter-related-objects=false"}, message: "invalid arguments"},
		{name: "positional conflicts", flags: []string{"extra"}, message: "invalid arguments"},
		{name: "help does not hide conflicts", flags: []string{"-help", "-only", "apex"}, message: "invalid arguments"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			project := tc.project
			if project == "" && tc.name != "missing project" {
				project = `{"packageDirectories":[{"path":"force-app"}]}`
			}
			if project != "" {
				writeLayoutFile(t, filepath.Join(root, "sfdx-project.json"), project)
			}
			if err := os.Mkdir(filepath.Join(root, "force-app"), 0700); err != nil {
				t.Fatal(err)
			}
			writeLayoutFile(t, filepath.Join(root, "package.json"), "original")
			if tc.cfg != "" {
				writeLayoutFile(t, filepath.Join(root, "config.json"), tc.cfg)
			}
			target := filepath.Join(root, "tsconfig.json")
			switch tc.target {
			case "file":
				writeLayoutFile(t, target, "original\x00\r\n")
			case "dir":
				writeLayoutFile(t, filepath.Join(target, "sentinel"), "original")
			case "link", "dangling", "package-link":
				dest := "missing"
				if tc.target == "link" {
					dest = "original.json"
					writeLayoutFile(t, filepath.Join(root, dest), "original")
				}
				if tc.target == "package-link" {
					dest = "force-app"
					target = filepath.Join(root, "linked")
					writeLayoutFile(t, filepath.Join(root, "sfdx-project.json"), `{"packageDirectories":[{"path":"linked"}]}`)
				}
				if err := os.Symlink(dest, target); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			before := initSnapshot(t, root)
			out, errs, code := initRun(t, root, append([]string{"-init-tsconfig"}, tc.flags...)...)
			want := "sf-typings: " + tc.message + "\n"
			if tc.message == "invalid arguments" {
				want += initUsage
			}
			if code != 1 || out != "" || errs != want {
				t.Fatalf("exit=%d stdout=%q stderr=%q want=%q", code, out, errs, want)
			}
			if !reflect.DeepEqual(before, initSnapshot(t, root)) {
				t.Fatal("rejected request changed filesystem")
			}
		})
	}
}
