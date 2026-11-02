package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTSConfigUpdate(t *testing.T) {
	for _, tc := range []struct {
		name, input, base, config string
		flags                     []string
		want                      string
	}{
		{name: "include JSONC", input: "{\n // keep\n \"compilerOptions\": {\"strict\": true,},\n \"include\": [\"src/**/*.ts\", /* keep */],\n \"exclude\": [\"ignored\"], \"files\": [\"entry.ts\"], \"references\": [], \"custom\": 42,\n}\n", want: "[\"src/**/*.ts\", \"sf-typings/**/*.d.ts\", /* keep */]"},
		{name: "opaque package extends", input: `{"extends":"@company/config","compilerOptions":{"strict":true},"include":["src"],"files":["entry.ts"],"exclude":["ignored"],"custom":42}`, want: `"include":["src", "sf-typings/**/*.d.ts"]`},
		{name: "opaque array extends", input: `{"extends":["@company/config","../unread.json"],"include":["src"],"references":[],"custom":42}`, want: `"include":["src", "sf-typings/**/*.d.ts"]`},
		{name: "implicit", input: `{"compilerOptions":{"strict":true}}`, want: `"include": ["**/*","sf-typings/**/*.d.ts"]`},
		{name: "files", input: `{"files":["entry.ts"]}`, want: `"include": ["sf-typings/**/*.d.ts"]`},
		{name: "empty include", input: `{"include":[]}`, want: `"include":["sf-typings/**/*.d.ts"]`},
		{name: "inherit", input: `{"extends":"./base/config.json","compilerOptions":{"strict":true}}`, base: `{"include":["src/**/*.ts"],"exclude":["ignored"]}`, want: `"include": ["base/src/**/*.ts","sf-typings/**/*.d.ts"]`},
		{name: "configured", input: `{"include":["src"]}`, config: `{"apexSources":["missing.cls"],"out":"custom"}`, flags: []string{"-config", "config.json"}, want: `"include":["src", "custom/**/*.d.ts"]`},
		{name: "override", input: `{"include":["src"]}`, config: `{"apexSources":["missing.cls"],"out":"custom"}`, flags: []string{"-config", "config.json", "-out", "chosen"}, want: `"include":["src", "chosen/**/*.d.ts"]`},
		{name: "SFDX parent-relative include", input: `{"extends":"./base/config.json"}`, base: `{"include":["../force-app/**/*.ts"]}`, want: `"include": ["force-app/**/*.ts","sf-typings/**/*.d.ts"]`},
		{name: "already present relative", input: `{"include":["./sf-typings/**/*.d.ts"]}`, want: ""},
		{name: "already present", input: "{ // unchanged\n\"include\":[\"src\",\"sf-typings/**/*.d.ts\",],}", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeLayoutFile(t, filepath.Join(root, "tsconfig.json"), tc.input)
			if tc.base != "" {
				writeLayoutFile(t, filepath.Join(root, "base/config.json"), tc.base)
			}
			if tc.config != "" {
				writeLayoutFile(t, filepath.Join(root, "config.json"), tc.config)
			}
			path := filepath.Join(root, "tsconfig.json")
			info, _ := os.Stat(path)
			before := initSnapshot(t, root)
			args := append([]string{"-init-tsconfig"}, tc.flags...)
			out, errs, code := initRun(t, root, args...)
			message := "Updated tsconfig.json.\n"
			if tc.want == "" {
				message = "Unchanged tsconfig.json.\n"
			}
			if code != 0 || out != message || errs != "" {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, out, errs)
			}
			data, _ := os.ReadFile(path)
			if tc.want == "" && string(data) != tc.input {
				t.Fatal("no-op changed bytes")
			}
			if !strings.Contains(string(data), tc.want) {
				t.Fatalf("config=%s want %s", data, tc.want)
			}
			if tc.name == "include JSONC" && string(data) != strings.Replace(tc.input, `["src/**/*.ts", /* keep */]`, tc.want, 1) {
				t.Fatal("changed original content outside include insertion")
			}
			if strings.HasPrefix(tc.name, "opaque ") && string(data) != strings.Replace(tc.input, `"include":["src"]`, tc.want, 1) {
				t.Fatal("opaque inheritance or unrelated fields changed")
			}
			after := initSnapshot(t, root)
			delete(before, "tsconfig.json")
			delete(after, "tsconfig.json")
			if !reflect.DeepEqual(before, after) {
				t.Fatal("unrelated changes")
			}
			updated, _ := os.Stat(path)
			if info.Mode() != updated.Mode() {
				t.Fatal("mode changed")
			}
			before = initSnapshot(t, root)
			out, errs, code = initRun(t, root, args...)
			if code != 0 || out != "Unchanged tsconfig.json.\n" || errs != "" || !reflect.DeepEqual(before, initSnapshot(t, root)) {
				t.Fatalf("repeat exit=%d stdout=%q stderr=%q", code, out, errs)
			}
		})
	}
}

func TestTSConfigUpdateRejected(t *testing.T) {
	for _, tc := range []struct{ name, input, base string }{
		{"malformed", `{`, ""}, {"nonobject", `[]`, ""}, {"null", `null`, ""},
		{"include type", `{"include":"src"}`, ""}, {"include null", `{"include":null}`, ""},
		{"include element", `{"include":[1]}`, ""}, {"duplicate", `{"include":[],"include":["src"]}`, ""},
		{"nested duplicate", `{"compilerOptions":{"strict":true,"strict":false}}`, ""},
		{"opaque extends null", `{"include":[],"extends":null}`, ""},
		{"opaque extends array element", `{"include":[],"extends":["@company/config",42]}`, ""},
		{"files type", `{"files":false}`, ""}, {"exclude type", `{"exclude":{}}`, ""},
		{"references only", `{"files":[],"references":[{"path":"./other"}]}`, ""},
		{"external extends", `{"extends":"../private.json"}`, ""},
		{"package extends", `{"extends":"@company/config"}`, ""},
		{"multiple extends", `{"extends":["./base/config.json"]}`, ""},
		{"missing base", `{"extends":"./missing.json"}`, ""},
		{"implicit inherited", `{"extends":"./base/config.json"}`, `{"compilerOptions":{"strict":true}}`},
		{"unsafe inherited include", `{"extends":"./base/config.json"}`, `{"include":["../../outside/**/*.ts"]}`},
		{"wildcard traversal", `{"extends":"./base/config.json"}`, `{"include":["**/../src"]}`},
		{"malformed base", `{"extends":"./base/config.json"}`, `{`},
		{"cycle", `{"extends":"./base/config.json"}`, `{"extends":"../tsconfig.json"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeLayoutFile(t, filepath.Join(root, "tsconfig.json"), tc.input)
			if tc.base != "" {
				writeLayoutFile(t, filepath.Join(root, "base/config.json"), tc.base)
			}
			before := initSnapshot(t, root)
			out, errs, code := initRun(t, root, "-init-tsconfig")
			if code != 1 || out != "" || errs != "sf-typings: cannot safely update tsconfig.json (invalid or unsupported config)\n" {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, out, errs)
			}
			if !reflect.DeepEqual(before, initSnapshot(t, root)) {
				t.Fatal("failure changed tree")
			}
		})
	}
}
