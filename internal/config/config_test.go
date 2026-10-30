package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoad(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{"invalid JSON", `{`, "invalid config JSON"},
		{"unknown key", `{"apexSources":["classes"],"extra":true}`, "invalid config JSON"},
		{"trailing JSON", `{"apexSources":["classes"]} {}`, "invalid config JSON"},
		{"empty", `{}`, "objects or apexSources must be nonempty"},
		{"alias", `{"orgAlias":"../dev","objects":["Account"]}`, "invalid org alias"},
		{"name", `{"orgAlias":"dev","objects":["../Account"]}`, "invalid object API name"},
		{"duplicate", `{"orgAlias":"dev","objects":["Account","Account"]}`, "duplicate object API name"},
		{"source", `{"apexSources":[""]}`, "invalid Apex source path"},
		{"Apex only", `{"apexSources":["classes"]}`, ""},
		{"sorted objects", `{"orgAlias":"dev","objects":["Contact","Account"]}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tc.raw), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := Load(path)
			if tc.want != "" {
				if err == nil || err.Error() != tc.want {
					t.Fatalf("error=%v want=%q", err, tc.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "sorted objects" && !reflect.DeepEqual(got.Objects, []string{"Account", "Contact"}) {
				t.Fatalf("objects=%v", got.Objects)
			}
		})
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil || err.Error() != "cannot read config" {
		t.Fatalf("missing config: %v", err)
	}
}
