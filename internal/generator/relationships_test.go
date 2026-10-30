package generator

import (
	"strings"
	"testing"
)

func TestParentRelationships(t *testing.T) {
	raw := []byte(`{"status":0,"result":{"name":"Contact","fields":[{"name":"ParentId","type":"reference","nillable":false,"relationshipName":"Parent","referenceTo":["Contact"]},{"name":"Customer__c","type":"reference","nillable":true,"relationshipName":"Customer__r","referenceTo":["Lead","Account","Lead"]}]}}`)
	files, err := Generate("Contact", raw, true)
	if err != nil {
		t.Fatal(err)
	}
	text := string(files[0].Data)
	for _, want := range []string{
		`import type { Account as SFParent1 } from "./Account";`,
		`import type { Lead as SFParent2 } from "./Lead";`,
		"Customer__c?: string | null;", "ParentId?: string;",
		"/** Parent relationship via Customer__c; available when queried. */",
		"Customer__r?: SFParent1 | SFParent2 | null;", "Parent?: Contact;",
		"@relationshipName Customer__r", "@referenceTo [\"Lead\",\"Account\",\"Lead\"]",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	if strings.Contains(text, `from "./Contact"`) {
		t.Fatal("self import")
	}
	if !strings.Contains(string(files[1].Data), `"referenceTo"`) {
		t.Fatal("snapshot lost metadata")
	}
}

func TestParentRelationshipValidation(t *testing.T) {
	for _, fields := range []string{
		`{"name":"Id","type":"reference","nillable":true,"relationshipName":"bad-name","referenceTo":[]}`,
		`{"name":"Id","type":"reference","nillable":true,"referenceTo":["../Bad"]}`,
		`{"name":"Id","type":"reference","nillable":true,"relationshipName":"Parent","referenceTo":["default"]}`,
		`{"name":"Id","type":"reference","nillable":true,"relationshipName":23}`,
		`{"name":"Id","type":"reference","nillable":true,"referenceTo":"Account"}`,
		`{"name":"Id","type":"reference","nillable":true,"relationshipName":"Parent\nInjected","referenceTo":["Account"]}`,
		`{"name":"Id","type":"reference","nillable":true,"relationshipName":"Parent","referenceTo":["Account\"; export interface Injected {} //"]}`,
		`{"name":"Id","type":"reference","nillable":true,"relationshipName":"Parent","referenceTo":[null]}`,
		`{"name":"Id","type":"reference","nillable":true,"relationshipName":"Id","referenceTo":["Account"]}`,
		`{"name":"A","type":"reference","nillable":true,"relationshipName":"Parent","referenceTo":["Account"]},{"name":"B","type":"reference","nillable":true,"relationshipName":"Parent","referenceTo":["Account"]}`,
	} {
		raw := []byte(`{"status":0,"result":{"name":"Contact","fields":[` + fields + `]}}`)
		if files, err := Generate("Contact", raw, false); err == nil || files != nil {
			t.Errorf("accepted %s", fields)
		}
	}
}

func TestParentRelationshipAliasesAndOrder(t *testing.T) {
	raw := []byte(`{"status":0,"result":{"name":"SFParent1","fields":[{"name":"SFParent2","type":"string","nillable":false},{"name":"Id","type":"reference","nillable":false,"relationshipName":"SFParent3","referenceTo":["Other","SFParent1"]}]}}`)
	files, targets, err := GenerateWithParents("SFParent1", raw, false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(files[0].Data)
	if !strings.Contains(text, `import type { Other as SFParent4 } from "./Other";`) || !strings.Contains(text, "SFParent3?: SFParent4 | SFParent1;") {
		t.Fatal(text)
	}
	if strings.Join(targets, ",") != "Other,SFParent1" {
		t.Fatal(targets)
	}
	reversed := []byte(`{"status":0,"result":{"name":"SFParent1","fields":[{"name":"Id","type":"reference","nillable":false,"relationshipName":"SFParent3","referenceTo":["Other","SFParent1"]},{"name":"SFParent2","type":"string","nillable":false}]}}`)
	again, err := Generate("SFParent1", reversed, false)
	if err != nil || string(again[0].Data) != text {
		t.Fatalf("non-deterministic %v", err)
	}
}

func TestParentRelationshipMissingMetadata(t *testing.T) {
	for _, extra := range []string{``, `,"relationshipName":"","referenceTo":["Account"]`, `,"relationshipName":null,"referenceTo":[]`, `,"relationshipName":"Parent","referenceTo":[]`, `,"referenceTo":["Account"]`} {
		files, targets, err := GenerateWithParents("Contact", []byte(`{"status":0,"result":{"name":"Contact","fields":[{"name":"ParentId","type":"reference","nillable":true`+extra+`}]}}`), false)
		if err != nil {
			t.Fatal(err)
		}
		if len(targets) != 0 || strings.Contains(string(files[0].Data), "import type") || strings.Contains(string(files[0].Data), "Parent?:") {
			t.Fatal(string(files[0].Data))
		}
	}
}
