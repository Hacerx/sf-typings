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

// relevantDocMetadata selects editor-facing metadata without modifying the raw
// Describe maps used by snapshots or type generation.
func relevantDocMetadata(metadata map[string]any, object bool) map[string]any {
	selected := make(map[string]any)
	text := func(keys ...string) {
		for _, key := range keys {
			if value, ok := metadata[key].(string); ok && strings.TrimSpace(value) != "" {
				selected[key] = value
			}
		}
	}
	flags := func(keys ...string) {
		for _, key := range keys {
			if value, ok := metadata[key].(bool); ok {
				selected[key] = value
			}
		}
	}
	text("label", "description")
	flags("createable", "updateable")
	if object {
		text("labelPlural")
		flags("deletable", "queryable", "searchable")
		return selected
	}
	text("type", "inlineHelpText", "defaultValueFormula", "calculatedFormula", "relationshipName")
	if value, exists := metadata["defaultValue"]; exists && value != nil {
		selected["defaultValue"] = value
	}
	for _, key := range []string{"unique", "externalId"} {
		if metadata[key] == true {
			selected[key] = true
		}
	}
	switch metadata["type"] {
	case "string", "textarea", "email", "phone", "url", "encryptedstring", "combobox":
		if value, ok := metadata["length"].(json.Number); ok {
			if length, err := value.Float64(); err == nil && length > 0 {
				selected["length"] = value
			}
		}
	case "int", "long", "double", "currency", "percent":
		for _, key := range []string{"precision", "scale"} {
			if value, ok := metadata[key].(json.Number); ok {
				selected[key] = value
			}
		}
	case "reference":
		if values, ok := metadata["referenceTo"].([]any); ok && len(values) > 0 {
			if len(values) == 1 {
				selected["referenceTo"] = values[0]
			} else {
				selected["referenceTo"] = values
			}
		}
	case "picklist", "multipicklist":
		flags("restrictedPicklist")
		text("controllerName")
	}
	return selected
}

// relevantPicklistEntry retains useful option differences, not redundant values
// already visible in a literal union. Missing/unknown activity is never assumed true.
func relevantPicklistEntry(entry any) map[string]any {
	option, ok := entry.(map[string]any)
	if !ok {
		return nil
	}
	value, ok := option["value"].(string)
	if !ok {
		return nil
	}
	label, hasLabel := option["label"].(string)
	active, knownActivity := option["active"].(bool)
	validFor, _ := option["validFor"].(string)
	if hasLabel && label == value && knownActivity && active && option["defaultValue"] != true && validFor == "" {
		return nil
	}
	selected := map[string]any{"value": value}
	for _, key := range []string{"label", "active", "defaultValue", "validFor"} {
		if value, exists := option[key]; exists {
			selected[key] = value
		}
	}
	return selected
}

// writeDescribeDocs renders only relevant metadata; snapshots remain complete.
func writeDescribeDocs(text *strings.Builder, indent string, metadata map[string]any, object bool) {
	selected := relevantDocMetadata(metadata, object)
	keys := make([]string, 0, len(selected))
	for key := range selected {
		if key != "description" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	fmt.Fprintln(text, indent+"/**")
	line := func(value string) {
		fmt.Fprintln(text, indent+" * "+strings.ReplaceAll(value, "*/", `*\/`))
	}
	if value, ok := metadata["description"].(string); ok && strings.TrimSpace(value) != "" {
		line("@description " + docValue(value))
	}
	for _, key := range keys {
		value := selected[key]
		tag := metadataTag(key)
		if tag == "metadata" {
			line("@metadata " + docJSON(key) + " " + docJSON(value))
		} else {
			line("@" + tag + " " + docValue(value))
		}
	}
	if !object && (metadata["type"] == "picklist" || metadata["type"] == "multipicklist") {
		if entries, ok := metadata["picklistValues"].([]any); ok {
			for _, entry := range entries {
				if relevant := relevantPicklistEntry(entry); relevant != nil {
					line("@picklistValue " + docJSON(relevant))
				}
			}
		}
		line("@remarks Describe options are not record-type filtered; availability may vary by record type.")
		if restricted, ok := metadata["restrictedPicklist"].(bool); !ok || !restricted {
			line("@remarks Unrestricted picklists may accept values not listed here.")
		}
		if metadata["dependentPicklist"] == true || metadata["controllerName"] != nil && metadata["controllerName"] != "" {
			line("@remarks Dependent picklists require controller/validFor filtering.")
		}
		line("@remarks Known options may include inactive entries and do not guarantee runtime validity.")
	}
	fmt.Fprintln(text, indent+" */")
}
