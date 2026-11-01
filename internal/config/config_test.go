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
		{"filter default", `{"orgAlias":"dev","objects":["AC_*","*__c"]}`, ""},
		{"filter false", `{"apexSources":["classes"],"filterRelatedObjects":false}`, ""},
		{"null filter", `{"apexSources":["classes"],"filterRelatedObjects":null}`, "invalid config JSON"},
		{"mixed-case null filter", `{"apexSources":["classes"],"FilterRelatedObjects":null}`, "invalid config JSON"},
		{"wrong filter type", `{"apexSources":["classes"],"filterRelatedObjects":"false"}`, "invalid config JSON"},
		{"timeout", `{"apexSources":["classes"],"timeout":"5m"}`, ""},
		{"wrong timeout type", `{"apexSources":["classes"],"timeout":35}`, "invalid config JSON"},
		{"null timeout", `{"apexSources":["classes"],"timeout":null}`, "invalid config JSON"},
		{"mixed-case null timeout", `{"apexSources":["classes"],"TiMeOuT":null}`, "invalid config JSON"},
		{"empty timeout", `{"apexSources":["classes"],"timeout":""}`, "invalid timeout (use a positive Go duration, e.g. 5m)"},
		{"zero timeout", `{"apexSources":["classes"],"timeout":"0"}`, "invalid timeout (use a positive Go duration, e.g. 5m)"},
		{"negative timeout", `{"apexSources":["classes"],"timeout":"-1s"}`, "invalid timeout (use a positive Go duration, e.g. 5m)"},
		{"overflow timeout", `{"apexSources":["classes"],"timeout":"999999999999999999999h"}`, "invalid timeout (use a positive Go duration, e.g. 5m)"},
		{"execution options", `{"apexSources":["classes"],"out":"generated","snapshots":true,"allChildren":true}`, ""},
		{"wrong output type", `{"apexSources":["classes"],"out":false}`, "invalid config JSON"},
		{"wrong snapshots type", `{"apexSources":["classes"],"snapshots":"true"}`, "invalid config JSON"},
		{"wrong allChildren type", `{"apexSources":["classes"],"allChildren":1}`, "invalid config JSON"},
		{"null output", `{"apexSources":["classes"],"out":null}`, "invalid config JSON"},
		{"null snapshots", `{"apexSources":["classes"],"snapshots":null}`, "invalid config JSON"},
		{"null allChildren", `{"apexSources":["classes"],"allChildren":null}`, "invalid config JSON"},
		{"uppercase null output", `{"apexSources":["classes"],"OUT":null}`, "invalid config JSON"},
		{"mixed-case null snapshots", `{"apexSources":["classes"],"SnapShots":null}`, "invalid config JSON"},
		{"uppercase null allChildren", `{"apexSources":["classes"],"ALLCHILDREN":null}`, "invalid config JSON"},
		{"case-insensitive valid options", `{"apexSources":["classes"],"OUT":"generated","SNAPSHOTS":false,"ALLCHILDREN":true}`, ""},
		{"bootstrap config", `{"apexSources":["classes"],"config":"other.json"}`, "invalid config JSON"},
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
			if got.FilterRelatedObjects != (tc.name != "filter false") {
				t.Fatalf("filterRelatedObjects=%v", got.FilterRelatedObjects)
			}
			wantTimeout := "35s"
			if tc.name == "timeout" {
				wantTimeout = "5m"
			}
			if got.Timeout != wantTimeout {
				t.Fatalf("timeout=%q want=%q", got.Timeout, wantTimeout)
			}
			if tc.name == "execution options" && (got.Out != "generated" || !got.Snapshots || !got.AllChildren) {
				t.Fatalf("options=%+v", got)
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
