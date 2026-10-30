package main

import (
	"encoding/json"
	"strings"
)

// tsLiteral uses JSON's control/Unicode escaping, but TypeScript's single-quoted
// delimiter. Unlike comment rendering it must not introduce a non-TS escape for
// */: the slash is safe inside a string literal outside a comment.
func tsLiteral(value string) string {
	encoded, _ := json.Marshal(value)
	body := strings.ReplaceAll(string(encoded[1:len(encoded)-1]), `\"`, `"`)
	return "'" + strings.ReplaceAll(body, "'", `\'`) + "'"
}

func fieldType(field map[string]any) string {
	kind, _ := field["type"].(string)
	if kind != "picklist" {
		return tsType(kind)
	}
	entries, _ := field["picklistValues"].([]any)
	seen := make(map[string]bool)
	var literals []string
	for _, entry := range entries {
		option, _ := entry.(map[string]any)
		value, ok := option["value"].(string)
		if ok && !seen[value] {
			seen[value] = true
			literals = append(literals, tsLiteral(value))
		}
	}
	if len(literals) == 0 {
		return "string"
	}
	if field["restrictedPicklist"] != true {
		literals = append(literals, "(string & {})")
	}
	return strings.Join(literals, " | ")
}
