package generator

import (
	"fmt"
	"sort"
)

type childRelationship struct {
	target string
	fields []string
}

// childRelationships uses only named, visible relationships. Ignored entries
// remain untouched in snapshots and never contribute imports or graph targets.
func childRelationships(value any, fields map[string]map[string]any) (map[string]childRelationship, error) {
	children := make(map[string]childRelationship)
	if value == nil {
		return children, nil
	}
	entries, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid child relationships")
	}
	for _, entry := range entries {
		relation, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid child relationship")
		}
		if relation["deprecatedAndHidden"] == true {
			continue
		}
		value := relation["relationshipName"]
		if value == nil || value == "" {
			continue
		}
		name, ok := value.(string)
		if !ok || !apiName.MatchString(name) {
			return nil, fmt.Errorf("invalid child relationship")
		}
		if hidden := relation["deprecatedAndHidden"]; hidden != nil {
			if _, ok := hidden.(bool); !ok {
				return nil, fmt.Errorf("invalid child relationship")
			}
		}
		target, ok := relation["childSObject"].(string)
		if !ok || !typeIdentifier(target) {
			return nil, fmt.Errorf("invalid child relationship")
		}
		field := ""
		if value := relation["field"]; value != nil {
			field, ok = value.(string)
			if !ok || !apiName.MatchString(field) {
				return nil, fmt.Errorf("invalid child relationship")
			}
		}
		if fields[name] != nil {
			return nil, fmt.Errorf("conflicting child relationship")
		}
		child, exists := children[name]
		if exists && child.target != target {
			return nil, fmt.Errorf("conflicting child relationship")
		}
		child.target = target
		if field != "" {
			i := sort.SearchStrings(child.fields, field)
			if i == len(child.fields) || child.fields[i] != field {
				child.fields = append(child.fields, field)
				sort.Strings(child.fields)
			}
		}
		children[name] = child
	}
	return children, nil
}
