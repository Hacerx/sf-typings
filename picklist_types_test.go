package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestPicklistTypes(t *testing.T) {
	options := `[{"value":"New","active":true},{"value":"Closed","active":false},{"value":"New"}]`
	for _, tc := range []struct {
		name, kind, restriction, options, want string
		nullable                               bool
	}{
		{"restricted", "picklist", `,"restrictedPicklist":true`, options, "'New' | 'Closed'", false},
		{"nullable", "picklist", `,"restrictedPicklist":true`, options, "'New' | 'Closed' | null", true},
		{"unrestricted", "picklist", `,"restrictedPicklist":false`, options, "'New' | 'Closed' | (string & {})", false},
		{"missing restriction", "picklist", "", options, "'New' | 'Closed' | (string & {}) | null", true},
		{"multipicklist", "multipicklist", `,"restrictedPicklist":true`, options, "string | null", true},
		{"formula result", "currency", "", options, "number", false},
		{"empty", "picklist", `,"restrictedPicklist":true`, `[]`, "string", false},
		{"null", "picklist", `,"restrictedPicklist":true`, `null`, "string", false},
		{"wrong shape", "picklist", `,"restrictedPicklist":true`, `{"value":"New"}`, "string", false},
		{"unusable", "picklist", `,"restrictedPicklist":true`, `[null,42,{}, {"value":false}]`, "string", false},
		{"mixed", "picklist", `,"restrictedPicklist":true`, `[null,{"value":""},{"value":42},{"value":"New","future":{"x":false}}]`, "'' | 'New'", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"name":"Account","fields":[{"name":"Choice","type":%q,"nillable":%t%s,"picklistValues":%s}]}`, tc.kind, tc.nullable, tc.restriction, tc.options)
			got := string(docsGenerate(t, body)[0].data)
			requireDocs(t, got, "Choice?: "+tc.want+";")
			if tc.name == "mixed" {
				requireDocs(t, got, "@picklistValue null", `@picklistValue {"value":42}`, `@picklistValue {"future":{"x":false},"value":"New"}`)
			}
			if tc.name == "wrong shape" {
				requireDocs(t, got, `@picklistValues {"value":"New"}`)
			}
			var entries []any
			if json.Unmarshal([]byte(tc.options), &entries) == nil && len(entries) > 0 {
				if strings.Count(got, " * @picklistValue ") != len(entries) {
					t.Fatalf("lost complete option metadata: %s", got)
				}
			}
		})
	}
}

func TestPicklistLiteralBoundaries(t *testing.T) {
	values := []string{"O'Brien", `a\b`, "*/", "line\r\n@type {never}", "\u2028\u2029", "", "a\x00b"}
	options := make([]map[string]any, 0, len(values))
	for _, value := range values {
		options = append(options, map[string]any{"value": value, "active": false})
	}
	body, err := json.Marshal(map[string]any{"name": "Account", "fields": []any{map[string]any{
		"name": "Choice", "type": "picklist", "nillable": true, "restrictedPicklist": true, "picklistValues": options,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	got := string(docsGenerate(t, string(body))[0].data)
	requireDocs(t, got, `Choice?: 'O\'Brien' | 'a\\b' | '*/' | 'line\r\n@type {never}' | '\u2028\u2029' | '' | 'a\u0000b' | null;`)
	if strings.ContainsAny(got, "\r\u2028\u2029\x00") || strings.Count(got, "*/") != 3 {
		t.Fatalf("unsafe literal/comment boundaries: %s", got)
	}
}

func TestDescribeTagCollisions(t *testing.T) {
	metadata := map[string]any{"name": "Account", "fields": []any{}, "type": "object", "sfType": "original",
		"param": "{never} injected", "returns": false, "enum": []any{"x"}, "template": "T", "property": "{never} bad",
		"defaultvalue": "reserved alias", "defaultValue": false,
		"deprecated": "not a deprecation", "constructor": true, "__proto__": "preserved", "bad-key": "x",
		"bad\n@type": "*/\r\n@returns", "label": "readable \"quoted\" text", "empty": "",
		"inlineHelpText": "text @deprecated @type {never}\u0085@hidden",
		"future":         map[string]any{"type": "unchanged"},
	}
	body, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	got := string(docsGenerate(t, string(body))[0].data)
	requireDocs(t, got, "@sfType object", `@metadata "sfType" "original"`,
		`@metadata "param" "{never} injected"`, `@metadata "returns" false`, `@metadata "enum" ["x"]`,
		`@metadata "template" "T"`, `@metadata "deprecated" "not a deprecation"`,
		`@metadata "name" "Account"`, `@metadata "property" "{never} bad"`, `@metadata "defaultvalue" "reserved alias"`, `@defaultValue false`,
		`@metadata "constructor" true`, `@metadata "__proto__" "preserved"`, `@metadata "bad-key" "x"`,
		`@metadata "bad\n\u0040type" "*\/\r\n\u0040returns"`, `@label readable "quoted" text`, `@empty ""`, `@future {"type":"unchanged"}`,
		`@inlineHelpText text \u0040deprecated \u0040type {never}\u0085\u0040hidden`)
	for _, line := range strings.Split(got, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
		if strings.HasPrefix(line, "@") {
			tag := strings.Fields(line)[0]
			if tag != "@metadata" && tag != "@sfType" && tag != "@defaultValue" && tag != "@label" && tag != "@empty" && tag != "@future" && tag != "@inlineHelpText" {
				t.Fatalf("semantic tag escaped fallback: %q", line)
			}
		}
	}
}
