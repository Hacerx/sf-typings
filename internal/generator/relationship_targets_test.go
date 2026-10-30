package generator

import (
	"reflect"
	"strings"
	"testing"
)

func TestRelationshipTargetSeparation(t *testing.T) {
	raw := []byte(`{"status":0,"result":{"name":"Root","fields":[{"name":"ParentId","type":"reference","nillable":true,"relationshipName":"Related","referenceTo":["Parent","Shared"]}],"childRelationships":[{"relationshipName":"Related","childSObject":"Child"},{"relationshipName":"Others","childSObject":"Shared"}]}}`)
	for _, include := range []bool{false, true} {
		files, targets, err := GenerateWithRelationshipTargets("Root", raw, false, include)
		if err != nil || !reflect.DeepEqual(targets.Parents, []string{"Parent", "Shared"}) {
			t.Fatalf("%+v %v", targets, err)
		}
		if include {
			if !reflect.DeepEqual(targets.Children, []string{"Child", "Shared"}) {
				t.Fatal(targets)
			}
		} else if len(targets.Children) != 0 || strings.Contains(string(files[0].Data), "Child") || !strings.Contains(string(files[0].Data), "Related?: SFParent1 | SFParent2 | null;") {
			t.Fatalf("targets=%+v file=%s", targets, files[0].Data)
		}
		_, combined, err := GenerateWithRelationships("Root", raw, false, include)
		want := []string{"Parent", "Shared"}
		if include {
			want = []string{"Child", "Parent", "Shared"}
		}
		if err != nil || !reflect.DeepEqual(combined, want) {
			t.Fatalf("%v %v", combined, err)
		}
	}
}
