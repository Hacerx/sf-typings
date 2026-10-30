package generator

import (
	"encoding/json"
	"strings"
	"testing"
)

func childDescribe(children string) []byte {
	return []byte(`{"status":0,"result":{"name":"Account","fields":[{"name":"ParentId","type":"reference","nillable":true,"relationshipName":"Parent","referenceTo":["Contact"]}],"childRelationships":` + children + `}}`)
}

func TestChildRelationships(t *testing.T) {
	raw := childDescribe(`[{"relationshipName":"Contacts","childSObject":"Contact","field":"AccountId"},{"relationshipName":"ChildAccounts","childSObject":"Account"},{"relationshipName":null,"childSObject":"bad/target"},{"relationshipName":"Ignored","childSObject":"bad/target","deprecatedAndHidden":true}]`)
	files, targets, err := GenerateWithRelationships("Account", raw, true, true)
	if err != nil {
		t.Fatal(err)
	}
	text := string(files[0].Data)
	for _, want := range []string{`import type { Contact as SFParent1 } from "./Contact";`, "Contacts?: SFParent1[];", "ChildAccounts?: Account[];", "Parent?: SFParent1 | null;", "ParentId?: string | null;", "Child relationship via AccountId; available when queried."} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
	if strings.Count(text, "import type") != 1 || strings.Contains(text, "Ignored?") || strings.Contains(text, "records") || strings.Contains(text, "[] | null") || strings.Join(targets, ",") != "Account,Contact" {
		t.Fatalf("targets=%v text=%s", targets, text)
	}
	var original, snapshot map[string]any
	var envelope struct{ Result map[string]any }
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	original = envelope.Result
	if err := json.Unmarshal(files[1].Data, &snapshot); err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(original)
	b, _ := json.Marshal(snapshot)
	if string(a) != string(b) {
		t.Fatal("snapshot changed")
	}
	// Existing public APIs retain their parent-only behavior.
	for _, generate := range []func(string, []byte, bool) ([]File, error){Generate, func(n string, r []byte, s bool) ([]File, error) { f, _, e := GenerateWithParents(n, r, s); return f, e }} {
		f, e := generate("Account", raw, false)
		if e != nil || strings.Contains(string(f[0].Data), "Contacts?") {
			t.Fatalf("parent API: %v %v", f, e)
		}
	}
}

func TestChildAliasReuseAndSorting(t *testing.T) {
	first := childDescribe(`[{"relationshipName":"SFParent1","childSObject":"Contact"},{"relationshipName":"Other","childSObject":"Contact"}]`)
	second := childDescribe(`[{"relationshipName":"Other","childSObject":"Contact"},{"relationshipName":"SFParent1","childSObject":"Contact"}]`)
	var previous string
	for _, raw := range [][]byte{first, second} {
		files, targets, err := GenerateWithRelationships("Account", raw, false, true)
		if err != nil {
			t.Fatal(err)
		}
		text := string(files[0].Data)
		if strings.Join(targets, ",") != "Contact" || strings.Count(text, "import type") != 1 || !strings.Contains(text, "SFParent1?: SFParent2[];") || !strings.Contains(text, "Other?: SFParent2[];") || !strings.Contains(text, "Parent?: SFParent2 | null;") {
			t.Fatalf("targets=%v declaration=%s", targets, text)
		}
		if previous != "" && previous != text {
			t.Fatal("relationship order changed declaration")
		}
		previous = text
	}
}

func TestChildValidation(t *testing.T) {
	for _, children := range []string{
		`{}`, `[null]`, `[42]`, `[{"relationshipName":42}]`, `[{"relationshipName":{}}]`,
		`[{"relationshipName":"Contacts"}]`, `[{"relationshipName":"Contacts","childSObject":"../Contact"}]`,
		`[{"relationshipName":"Contacts","childSObject":"class"}]`,
		`[{"relationshipName":"X*/ injected","childSObject":"Contact"}]`,
		`[{"relationshipName":"Contacts","childSObject":"Contact","field":"X*/ injected"}]`,
		`[{"relationshipName":"Contacts","childSObject":"Contact","deprecatedAndHidden":"true"}]`,
		`[{"relationshipName":"ParentId","childSObject":"Contact"}]`, `[{"relationshipName":"Parent","childSObject":"Contact"}]`,
		`[{"relationshipName":"Contacts","childSObject":"Contact"},{"relationshipName":"Contacts","childSObject":"Contact"}]`,
	} {
		t.Run(children, func(t *testing.T) {
			files, targets, err := GenerateWithRelationships("Account", childDescribe(children), true, true)
			if err == nil || err.Error() != "invalid describe for Account" || files != nil || targets != nil {
				t.Fatalf("files=%v targets=%v err=%v", files, targets, err)
			}
		})
	}
	for _, children := range []string{`null`, `[]`, `[{"relationshipName":"","childSObject":false}]`, `[{"childSObject":"Bad/path"}]`, `[{"relationshipName":"Ignored","deprecatedAndHidden":true}]`} {
		if _, _, err := GenerateWithRelationships("Account", childDescribe(children), false, true); err != nil {
			t.Fatalf("%s: %v", children, err)
		}
	}
	// Child metadata on objects outside the configured scope is neither used nor followed.
	if _, _, err := GenerateWithRelationships("Account", childDescribe(`{"unused":"malformed"}`), false, false); err != nil {
		t.Fatal(err)
	}
}
