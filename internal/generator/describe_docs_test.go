package generator

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"sf-typings/internal/jsonutil"
)

const docsDescribe = `{"name":"Account","label":"Accounts","createable":false,"future":{"z":null,"a":900719925474099312345},"fields":[
{"name":"Status","type":"picklist","nillable":true,"label":"Status label","inlineHelpText":"Choose a status","restrictedPicklist":false,"dependentPicklist":true,"controllerName":"Kind","picklistValues":[{"value":"Z","label":"Last","active":true,"defaultValue":false,"validFor":"AA=="},{"value":"A","label":"Old","active":false,"defaultValue":true},{"value":"?","label":"Unspecified"}]},
{"name":"Amount","type":"currency","nillable":false,"length":0,"precision":18,"scale":2,"createable":false,"updateable":true,"calculated":true,"calculatedFormula":"1 + 2","defaultValue":null,"referenceTo":["Account"],"relationshipName":"Parent","future":{"ratio":1.234567890123456789,"flag":false}}
]}`

func docsGenerate(t *testing.T, body string) []File {
	t.Helper()
	files, err := Generate("Account", []byte(`{"status":0,"result":`+body+`}`), true)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func requireDocs(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in declaration:\n%s", want, got)
		}
	}
}

func TestDescribeDocsDescription(t *testing.T) {
	for _, object := range []bool{true, false} {
		for _, tc := range []struct {
			name string
			raw  string
			want string
		}{
			{"priority", `{"description":"  Preferred  ","inlineHelpText":"Help","label":"Label"}`, "  Preferred  "},
			{"no-help-fallback", `{"description":" \t\n","inlineHelpText":"Help","label":"Label"}`, ""},
			{"no-label-fallback", `{"description":42,"inlineHelpText":false,"label":"Label"}`, ""},
			{"missing-description", `{"inlineHelpText":"Help","label":"Label"}`, ""},
			{"null", `{"description":null,"inlineHelpText":"Help","label":"Label"}`, ""},
			{"boolean", `{"description":true,"label":"Label"}`, ""},
			{"array", `{"description":["text"],"label":"Label"}`, ""},
			{"absent", `{}`, ""},
			{"blank", `{"description":" \t\n","inlineHelpText":"","label":" "}`, ""},
			{"nonstring", `{"description":{"text":"not a string"},"inlineHelpText":[],"label":null}`, ""},
			{"hostile", `{"description":"*/\r\n@deprecated\u0085\u2028\u2029","label":"Label"}`, `*\/\r\n\u0040deprecated\u0085\u2028\u2029`},
		} {
			t.Run(fmt.Sprintf("object=%t/%s", object, tc.name), func(t *testing.T) {
				var metadata map[string]any
				if err := jsonutil.Decode([]byte(tc.raw), &metadata, false); err != nil {
					t.Fatal(err)
				}
				var text strings.Builder
				writeDescribeDocs(&text, "", metadata, object)
				got := text.String()
				if tc.want == "" {
					if strings.Contains(got, "@description ") {
						t.Fatalf("synthesized description: %s", got)
					}
				} else if !strings.HasPrefix(got, "/**\n * @description "+tc.want+"\n") || strings.Count(got, "@description ") != 1 {
					t.Errorf("expected one leading description %q: %s", tc.want, got)
				}
				if strings.Contains(got, `@metadata "description"`) {
					t.Fatalf("duplicate description: %s", got)
				}
				if strings.Count(got, "*/") != 1 || strings.ContainsAny(got, "\r\u0085\u2028\u2029") {
					t.Fatalf("unsafe description: %s", got)
				}
			})
		}
	}
}

func TestDescribeDocsMetadata(t *testing.T) {
	files := docsGenerate(t, docsDescribe)
	got := string(files[0].Data)
	requireDocs(t, got, " * @label Accounts", " * @createable false",
		`@label Status label`, `@inlineHelpText Choose a status`, "@precision 18", "@scale 2",
		"@updateable true", `@calculatedFormula 1 + 2`, `@sfType currency`,
		"Amount?: number;", "Status?: 'Z' | 'A' | '?' | (string & {}) | null;")
	objectDoc := strings.Split(got, "export interface Account")[0]
	if strings.Contains(objectDoc, "@fields") || strings.Contains(objectDoc, "@future") {
		t.Fatalf("object metadata not separated/sorted: %s", objectDoc)
	}
	if strings.Index(got, "Amount?:") > strings.Index(got, "Status?:") {
		t.Fatal("field order changed")
	}
	var original any
	if err := jsonutil.Decode([]byte(docsDescribe), &original, false); err != nil {
		t.Fatal(err)
	}
	wantSnapshot, _ := json.MarshalIndent(original, "", "  ")
	if string(files[1].Data) != string(wantSnapshot)+"\n" {
		t.Fatal("metadata documentation changed the snapshot")
	}
	if again := docsGenerate(t, docsDescribe); string(again[0].Data) != got {
		t.Fatal("nondeterministic documentation")
	}
}

func TestDescribeDocsPicklists(t *testing.T) {
	got := string(docsGenerate(t, docsDescribe)[0].Data)
	requireDocs(t, got,
		`@picklistValue {"active":true,"defaultValue":false,"label":"Last","validFor":"AA==","value":"Z"}`,
		`@picklistValue {"active":false,"defaultValue":true,"label":"Old","value":"A"}`,
		`@picklistValue {"label":"Unspecified","value":"?"}`,
		"Unrestricted picklists may accept values not listed here.",
		"Dependent picklists require controller/validFor filtering.",
		"Describe options are not record-type filtered; availability may vary by record type.")
	if strings.Index(got, `"value":"Z"`) > strings.Index(got, `"value":"A"`) {
		t.Fatal("picklist input order changed")
	}
	for _, kind := range []string{"picklist", "multipicklist"} {
		body := fmt.Sprintf(`{"name":"Account","fields":[{"name":"Choice","type":%q,"nillable":false,"restrictedPicklist":true,"picklistValues":[]}]}`, kind)
		decl := string(docsGenerate(t, body)[0].Data)
		requireDocs(t, decl, "Choice?: string;", "@restrictedPicklist true", "@remarks Describe options are not record-type filtered")
		if strings.Contains(decl, "@picklistValues") {
			t.Fatalf("empty options documented: %s", decl)
		}
		if strings.Contains(decl, "Unrestricted picklists") || strings.Contains(decl, "Dependent picklists require") {
			t.Fatalf("incorrect context warning: %s", decl)
		}
	}
}

func TestDescribeDocsEscaping(t *testing.T) {
	body := `{"name":"Account","description":"*/\r\n@deprecated\u2028@hidden\u2029*/","label":"*/\r\n@deprecated\u2028@hidden\u2029*/","future":{"*/\n@tag":["*/",null]},"fields":[{"name":"Name","type":"picklist","nillable":false,"description":"*/\r@tag\n* @other","inlineHelpText":"*/\r@tag\n* @other","picklistValues":[{"value":"*/","label":"\n@tag"}]}]}`
	got := string(docsGenerate(t, body)[0].Data)
	if strings.Count(got, "*/") != 3 || strings.ContainsAny(got, "\r\u2028\u2029") {
		t.Fatalf("unsafe comment boundary: %s", got)
	}
	allowed := map[string]bool{"@description": true, "@metadata": true, "@label": true, "@future": true, "@sfType": true, "@nillable": true, "@inlineHelpText": true, "@picklistValue": true, "@remarks": true}
	for _, line := range strings.Split(got, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
		if strings.HasPrefix(line, "@") && !allowed[strings.Fields(line)[0]] {
			t.Fatalf("unexpected JSDoc tag: %q", line)
		}
	}
	requireDocs(t, got, `@description *\/\r\n\u0040deprecated\u2028\u0040hidden\u2029*\/`, `@description *\/\r\u0040tag\n* \u0040other`, `@label *\/\r\n\u0040deprecated`, `@picklistValue {`)
	requireDocs(t, got, `*\/`, `\r\n\u0040deprecated`, `\u2028\u0040hidden`, `\u2029`, "Name?: '*/' | (string & {});")
}

func TestDescribeDocsValidationAndTypes(t *testing.T) {
	for _, fields := range []string{
		`null`, `{}`, `[null]`, `[42]`,
		`[{"name":"Bad","type":"string","nillable":null}]`,
		`[{"name":"Bad","type":42,"nillable":false}]`,
		`[{"name":"Bad*/","type":"string","nillable":false}]`,
		`[{"name":"A","type":"string","nillable":false},{"name":"A","type":"boolean","nillable":true}]`,
	} {
		_, err := Generate("Account", []byte(`{"status":0,"result":{"name":"Account","fields":`+fields+`}}`), true)
		if err == nil || err.Error() != "invalid describe for Account" {
			t.Errorf("accepted invalid fields %s: %v", fields, err)
		}
	}
	for kind, want := range map[string]string{"reference": "string", "boolean": "boolean", "long": "number", "datetime": "string", "futureType": "unknown"} {
		body := fmt.Sprintf(`{"name":"Account","fields":[{"name":"Value","type":%q,"nillable":true,"future":null,"picklistValues":null}]}`, kind)
		requireDocs(t, string(docsGenerate(t, body)[0].Data), "Value?: "+want+" | null;")
	}
}

func TestDescribeDocsConcise(t *testing.T) {
	for _, tc := range []struct {
		name, raw  string
		object     bool
		want, omit []string
	}{
		{"object", `{"label":"Account","labelPlural":"Accounts","description":"Object","createable":false,"updateable":true,"deletable":false,"queryable":true,"searchable":false,"urls":{"x":"y"},"childRelationships":[],"future":42,"name":"Account"}`, true,
			[]string{"@description Object", "@label Account", "@labelPlural Accounts", "@createable false", "@updateable true", "@deletable false", "@queryable true", "@searchable false"}, []string{"@urls", "@childRelationships", "@future", "@metadata"}},
		{"ordinary", `{"label":"Name","type":"string","createable":false,"updateable":true,"length":80,"precision":0,"scale":0,"nillable":true,"name":"Name","calculated":false,"unique":false,"externalId":false,"inlineHelpText":" ","future":42,"picklistValues":[]}`, false,
			[]string{"@label Name", "@sfType string", "@createable false", "@updateable true", "@length 80"}, []string{"@precision", "@scale", "@nillable", "@metadata", "@calculated", "@unique", "@externalId", "@inlineHelpText", "@future", "@picklist"}},
		{"numeric", `{"type":"double","length":20,"precision":18,"scale":0,"unique":true,"externalId":true,"defaultValue":0,"defaultValueFormula":" TODAY() ","calculatedFormula":"1 + 2"}`, false,
			[]string{"@precision 18", "@scale 0", "@unique true", "@externalId true", "@defaultValue 0", "@defaultValueFormula  TODAY() ", "@calculatedFormula 1 + 2"}, []string{"@length", "@createable", "@updateable"}},
		{"false default", `{"type":"boolean","defaultValue":false}`, false, []string{"@defaultValue false"}, nil},
		{"empty default", `{"type":"string","length":0,"defaultValue":""}`, false, []string{`@defaultValue ""`}, []string{"@length"}},
		{"null default", `{"type":"string","defaultValue":null,"defaultValueFormula":" ","calculatedFormula":"","referenceTo":["Account"],"relationshipName":" "}`, false, nil, []string{"@defaultValue", "@calculatedFormula", "@referenceTo", "@relationshipName"}},
		{"lookup", `{"type":"reference","referenceTo":["Account"],"relationshipName":"Parent"}`, false, []string{"@referenceTo Account", "@relationshipName Parent"}, nil},
		{"polymorphic", `{"type":"reference","referenceTo":["Account","Contact"]}`, false, []string{`@referenceTo ["Account","Contact"]`}, nil},
		{"empty lookup", `{"type":"reference","referenceTo":[]}`, false, nil, []string{"@referenceTo"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var metadata map[string]any
			if err := jsonutil.Decode([]byte(tc.raw), &metadata, false); err != nil {
				t.Fatal(err)
			}
			var text strings.Builder
			writeDescribeDocs(&text, "", metadata, tc.object)
			got := text.String()
			requireDocs(t, got, tc.want...)
			for _, tag := range tc.omit {
				if strings.Contains(got, tag) {
					t.Errorf("irrelevant %s: %s", tag, got)
				}
			}
		})
	}
}

func TestDescribeDocsMeaningfulPicklists(t *testing.T) {
	for _, kind := range []string{"picklist", "multipicklist"} {
		body := fmt.Sprintf(`{"name":"Account","fields":[{"name":"Choice","type":%q,"nillable":true,"restrictedPicklist":false,"controllerName":"Kind","picklistValues":[{"value":"Ordinary","label":"Ordinary","active":true,"defaultValue":false},{"value":"Implicit","label":"Implicit","active":true},{"value":"Translated","label":"Traducción","active":true},{"value":"Old","label":"Old","active":false},{"value":"Default","label":"Default","active":true,"defaultValue":true},{"value":"Dependent","label":"Dependent","active":true,"validFor":"AA=="},{"value":"Unknown","label":"Unknown"},{"value":"Null","label":"Null","active":null}]}]}`, kind)
		got := string(docsGenerate(t, body)[0].Data)
		if strings.Count(got, " * @picklistValue ") != 6 {
			t.Fatalf("wrong relevant option count: %s", got)
		}
		for _, value := range []string{"Translated", "Old", "Default", "Dependent", "Unknown", "Null"} {
			requireDocs(t, got, `"value":"`+value+`"`)
		}
		for _, value := range []string{"Ordinary", "Implicit"} {
			if strings.Contains(got, `"value":"`+value+`"`) {
				t.Errorf("redundant option: %s", got)
			}
		}
		requireDocs(t, got, "@restrictedPicklist false", "@controllerName Kind", "@remarks Describe options", "@remarks Unrestricted", "@remarks Dependent")
		if kind == "multipicklist" {
			requireDocs(t, got, "Choice?: string | null;")
		} else {
			requireDocs(t, got, "Choice?: 'Ordinary' | 'Implicit' | 'Translated'")
		}
	}
}
