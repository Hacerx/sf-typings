package generator

import "fmt"

type childRelationship struct {
	target string
	field  string
}

// childRelationships uses only named, visible relationships. Ignored entries
// remain untouched in snapshots and never contribute imports or graph targets.
func childRelationships(value any, fields map[string]map[string]any, parents map[string]parentRelationship) (map[string]childRelationship, error) {
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
		_, parentExists := parents[name]
		if fields[name] != nil || parentExists {
			return nil, fmt.Errorf("conflicting child relationship")
		}
		if _, exists := children[name]; exists {
			return nil, fmt.Errorf("conflicting child relationship")
		}
		children[name] = childRelationship{target, field}
	}
	return children, nil
}
