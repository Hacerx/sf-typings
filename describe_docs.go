package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// docJSON retains json.Number precision and sorts nested map keys. Escape
// terminators and apparent tags only after serialization, including nested keys
// and values. Even inline @tags can be meaningful to JSDoc consumers.
func docJSON(value any) string {
	var encoded strings.Builder
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value) // Values come exclusively from decoded JSON.
	return strings.NewReplacer("*/", `*\/`, "\u0085", `\u0085`, "@", `\u0040`).Replace(strings.TrimSuffix(encoded.String(), "\n"))
}

// Standard JSDoc/TypeScript tags must not give Salesforce metadata semantics.
// Reserve our own synthetic tags too, so unknown keys cannot impersonate them.
var semanticDocTags = func() map[string]bool {
	tags := make(map[string]bool)
	for _, tag := range strings.Fields(`
		abstract access alias async augments author borrows callback class classdesc
		constant const constructs constructor copyright default defaultvalue deprecated
		description desc enum event example exports extends external file fileoverview
		overview fires emits function func method generator global hideconstructor ignore
		implements inherits inheritdoc inner instance interface kind lends license listens
		member var memberof memberof! mixes mixin module name namespace override overload
		package param arg argument property prop private protected public readonly requires
		return returns satisfies see since static summary template this throws exception
		todo tutorial type typedef variation version yields yield optional internal
		noinheritdoc jsx jsxfrag jsximportsource jsxruntime import
		sfType metadata picklistValue __proto__ prototype`) {
		tags[strings.ToLower(tag)] = true
	}
	return tags
}()

func metadataTag(key string) string {
	if key == "type" {
		return "sfType"
	}
	// Salesforce's camel-case defaultValue is not JSDoc's @defaultvalue alias.
	if !apiName.MatchString(key) || (key != "defaultValue" && semanticDocTags[strings.ToLower(key)]) {
		return "metadata"
	}
	return key
}

func docValue(value any) string {
	encoded := docJSON(value)
	if s, ok := value.(string); ok && s != "" {
		// Keep strings readable while visibly escaping all physical separators,
		// control characters and backslashes. Only remove JSON's quote escapes.
		return strings.ReplaceAll(encoded[1:len(encoded)-1], `\"`, `"`)
	}
	return encoded
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
	line := func(value string) {
		fmt.Fprintln(text, indent+" * "+strings.ReplaceAll(value, "*/", `*\/`))
	}
	for _, key := range keys {
		value := metadata[key]
		if !object && key == "picklistValues" {
			if entries, ok := value.([]any); ok && len(entries) > 0 {
				for _, entry := range entries {
					line("@picklistValue " + docJSON(entry))
				}
				continue
			}
		}
		tag := metadataTag(key)
		if tag == "metadata" {
			line("@metadata " + docJSON(key) + " " + docJSON(value))
		} else {
			line("@" + tag + " " + docValue(value))
		}
	}
	if !object && (metadata["type"] == "picklist" || metadata["type"] == "multipicklist") {
		line("Describe options are not record-type filtered; availability may vary by record type.")
		if restricted, ok := metadata["restrictedPicklist"].(bool); !ok || !restricted {
			line("Unrestricted picklists may accept values not listed here. Restriction applies only when restrictedPicklist=true.")
		}
		if metadata["dependentPicklist"] == true || metadata["controllerName"] != nil && metadata["controllerName"] != "" {
			line("Dependent picklists require controller/validFor filtering.")
		}
		line("Known options include inactive entries and do not guarantee runtime validity.")
	}
	fmt.Fprintln(text, indent+" */")
}
