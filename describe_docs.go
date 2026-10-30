package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// docJSON retains json.Number precision and sorts nested map keys. JSON quoting
// keeps CR/LF, Unicode line separators and apparent @tags on a single doc line.
// Escape terminators AFTER serialization, including those in nested keys/values.
func docJSON(value any) string {
	encoded, _ := json.Marshal(value) // Values come exclusively from decoded JSON.
	return strings.ReplaceAll(string(encoded), "*/", `*\/`)
}

func docKey(key string) string {
	if apiName.MatchString(key) {
		return key
	}
	return docJSON(key)
}

// writeDescribeDocs documents the full raw metadata, not a fixed subset of the
// Describe schema. Object fields belong on their properties, not the interface.
func writeDescribeDocs(text *strings.Builder, indent string, metadata map[string]any, object bool) {
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		if !object || key != "fields" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	fmt.Fprintln(text, indent+"/**")
	line := func(value string) { fmt.Fprintln(text, indent+" * "+value) }
	for _, key := range keys {
		value := metadata[key]
		if !object && key == "picklistValues" {
			if entries, ok := value.([]any); ok && len(entries) > 0 {
				for i, entry := range entries {
					activity := "activity unspecified"
					if option, ok := entry.(map[string]any); ok {
						if active, ok := option["active"].(bool); ok {
							if active {
								activity = "active known option"
							} else {
								activity = "inactive option"
							}
						}
					}
					line(fmt.Sprintf("picklistValues[%d] (%s): %s", i, activity, docJSON(entry)))
				}
				continue
			}
		}
		line(docKey(key) + ": " + docJSON(value))
	}
	if !object && (metadata["type"] == "picklist" || metadata["type"] == "multipicklist") {
		line("Describe options are not record-type filtered; availability may vary by record type.")
		if restricted, ok := metadata["restrictedPicklist"].(bool); !ok || !restricted {
			line("Unrestricted picklists may accept values not listed here. Restriction applies only when restrictedPicklist=true.")
		}
		if metadata["dependentPicklist"] == true || metadata["controllerName"] != nil && metadata["controllerName"] != "" {
			line("Dependent picklists require controller/validFor filtering.")
		}
		line("Options are documentation, not a literal union or a guarantee of runtime validity.")
	}
	fmt.Fprintln(text, indent+" */")
}
