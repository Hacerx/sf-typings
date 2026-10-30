package generator

import (
	"fmt"
	"sort"
	"strings"
)

// Type identifiers must also be legal TypeScript binding names.
func typeIdentifier(name string) bool {
	if !apiName.MatchString(name) {
		return false
	}
	for _, word := range strings.Fields(`break case catch class const continue debugger default delete do else enum export extends false finally for function if import in instanceof new null return super switch this throw true try typeof var void while with implements interface let package private protected public static yield await abstract as asserts any boolean constructor declare get infer intrinsic is keyof module namespace never number object readonly require set string symbol type undefined unique unknown from global of override out satisfies using`) {
		if name == word {
			return false
		}
	}
	return true
}

type parentRelationship struct {
	field    string
	targets  []string
	nullable bool
}

// parentRelationships validates all supplied relationship identifiers, even when
// incomplete metadata means no relationship can be generated. It never mutates
// Describe metadata, so snapshots retain the complete original response.
func parentRelationships(fields map[string]map[string]any) (map[string]parentRelationship, []string, error) {
	parents := make(map[string]parentRelationship)
	all := make(map[string]bool)
	for field, f := range fields {
		relationship := ""
		if value := f["relationshipName"]; value != nil {
			var ok bool
			relationship, ok = value.(string)
			if !ok || relationship != "" && !apiName.MatchString(relationship) {
				return nil, nil, fmt.Errorf("invalid relationship")
			}
		}
		targets := make(map[string]bool)
		if value := f["referenceTo"]; value != nil {
			values, ok := value.([]any)
			if !ok {
				return nil, nil, fmt.Errorf("invalid relationship")
			}
			for _, value := range values {
				name, ok := value.(string)
				if !ok || !typeIdentifier(name) {
					return nil, nil, fmt.Errorf("invalid relationship")
				}
				targets[name] = true
			}
		}
		if f["type"] != "reference" || relationship == "" || len(targets) == 0 {
			continue
		}
		if fields[relationship] != nil {
			return nil, nil, fmt.Errorf("conflicting relationship")
		}
		if _, exists := parents[relationship]; exists {
			return nil, nil, fmt.Errorf("conflicting relationship")
		}
		var names []string
		for name := range targets {
			names = append(names, name)
			all[name] = true
		}
		sort.Strings(names)
		parents[relationship] = parentRelationship{field, names, f["nillable"] == true}
	}
	var targets []string
	for name := range all {
		targets = append(targets, name)
	}
	sort.Strings(targets)
	return parents, targets, nil
}
