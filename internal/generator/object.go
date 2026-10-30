package generator

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"sf-typings/internal/jsonutil"
)

// File is a generated artifact, held in memory until all inputs succeed.
type File struct {
	Name string
	Data []byte
}

var apiName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

type envelope struct {
	Status *int            `json:"status"`
	Result json.RawMessage `json:"result"`
}

func tsType(kind string) string {
	switch kind {
	case "string", "textarea", "picklist", "multipicklist", "id", "reference", "phone", "email", "url", "encryptedstring", "combobox", "base64":
		return "string"
	case "boolean":
		return "boolean"
	case "int", "double", "currency", "percent", "long":
		return "number"
	case "date", "datetime", "time":
		return "string"
	default:
		return "unknown"
	}
}

// Generate validates a complete Describe envelope and renders its declaration
// and optional snapshot without changing the decoded metadata.
func Generate(name string, raw []byte, snapshots bool) ([]File, error) {
	return generate(name, raw, snapshots, false, nil)
}

// GenerateWithParents returns validated, sorted, unique parent targets alongside
// the artifacts. Only relationships actually emitted participate in discovery.
func GenerateWithParents(name string, raw []byte, snapshots bool) ([]File, []string, error) {
	return GenerateWithRelationships(name, raw, snapshots, false)
}

// GenerateWithRelationships optionally includes Apex-style child collections and
// returns sorted, unique targets for all emitted relationships. Callers control
// child scope per object; Generate and GenerateWithParents remain parent-only.
func GenerateWithRelationships(name string, raw []byte, snapshots, includeChildren bool) ([]File, []string, error) {
	files, targets, err := GenerateWithRelationshipTargets(name, raw, snapshots, includeChildren)
	if err != nil {
		return nil, nil, err
	}
	all := make(map[string]bool)
	for _, name := range append(targets.Parents, targets.Children...) {
		all[name] = true
	}
	var combined []string
	for name := range all {
		combined = append(combined, name)
	}
	sort.Strings(combined)
	return files, combined, nil
}

// RelationshipTargets separates emitted parent and child edges for caller policy.
// Each list is sorted and unique; a target may occur in both lists.
type RelationshipTargets struct {
	Parents  []string
	Children []string
}

// GenerateWithRelationshipTargets renders once and reports only emitted edges.
func GenerateWithRelationshipTargets(name string, raw []byte, snapshots, includeChildren bool) ([]File, RelationshipTargets, error) {
	var targets RelationshipTargets
	files, err := generate(name, raw, snapshots, includeChildren, &targets)
	if err != nil {
		return nil, RelationshipTargets{}, err
	}
	return files, targets, nil
}

func generate(name string, raw []byte, snapshots, includeChildren bool, targets *RelationshipTargets) ([]File, error) {
	invalid := fmt.Errorf("invalid describe for %s", name)
	if !typeIdentifier(name) {
		return nil, invalid
	}
	var env envelope
	if jsonutil.Decode(raw, &env, false) != nil || env.Status == nil || *env.Status != 0 || len(env.Result) == 0 {
		return nil, invalid
	}
	var metadata map[string]any
	if jsonutil.Decode(env.Result, &metadata, false) != nil || metadata["name"] != name {
		return nil, invalid
	}
	fields, ok := metadata["fields"].([]any)
	if !ok {
		return nil, invalid
	}
	byName := make(map[string]map[string]any)
	var names []string
	for _, value := range fields {
		f, ok := value.(map[string]any)
		fieldName, _ := f["name"].(string)
		kind, _ := f["type"].(string)
		_, nullableOK := f["nillable"].(bool)
		if !ok || !apiName.MatchString(fieldName) || kind == "" || !nullableOK || byName[fieldName] != nil {
			return nil, invalid
		}
		byName[fieldName] = f
		names = append(names, fieldName)
	}
	parents, parentTargets, err := parentRelationships(byName)
	if err != nil {
		return nil, invalid
	}
	var children map[string]childRelationship
	if includeChildren {
		children, err = childRelationships(metadata["childRelationships"], byName)
		if err != nil {
			return nil, invalid
		}
	}
	allTargets := make(map[string]bool)
	for _, target := range parentTargets {
		allTargets[target] = true
	}
	for _, child := range children {
		allTargets[child.target] = true
	}
	var relationshipTargets []string
	for target := range allTargets {
		relationshipTargets = append(relationshipTargets, target)
	}
	sort.Strings(relationshipTargets)
	if targets != nil {
		targets.Parents = parentTargets
		childTargets := make(map[string]bool)
		for _, child := range children {
			childTargets[child.target] = true
		}
		for target := range childTargets {
			targets.Children = append(targets.Children, target)
		}
		sort.Strings(targets.Children)
	}
	occupied := map[string]bool{name: true}
	for _, field := range names {
		occupied[field] = true
	}
	for relationship := range parents {
		occupied[relationship] = true
		names = append(names, relationship)
	}
	for relationship := range children {
		if _, parentExists := parents[relationship]; !parentExists {
			names = append(names, relationship)
		}
		occupied[relationship] = true
	}
	sort.Strings(names)
	var text strings.Builder
	fmt.Fprintln(&text, "// Generated by sf-typings. Do not edit.")
	aliases := map[string]string{name: name}
	nextAlias := 1
	for _, target := range relationshipTargets {
		if target == name {
			continue
		}
		alias := fmt.Sprintf("SFParent%d", nextAlias)
		for occupied[alias] {
			nextAlias++
			alias = fmt.Sprintf("SFParent%d", nextAlias)
		}
		nextAlias++
		occupied[alias] = true
		aliases[target] = alias
		fmt.Fprintf(&text, "import type { %s as %s } from %q;\n", target, alias, "./"+target)
	}
	writeDescribeDocs(&text, "", metadata, true)
	fmt.Fprintf(&text, "export interface %s {\n", name)
	for _, fieldName := range names {
		if parent, ok := parents[fieldName]; ok {
			var types []string
			for _, target := range parent.targets {
				types = append(types, aliases[target])
			}
			if child, ok := children[fieldName]; ok {
				types = append(types, aliases[child.target]+"[]")
				fmt.Fprintf(&text, "  /**\n   * Parent relationship via %s; available when queried.\n", parent.field)
				if len(child.fields) > 0 {
					fmt.Fprintf(&text, "   * Child relationship via %s; available when queried.\n", strings.Join(child.fields, ", "))
				} else {
					fmt.Fprintln(&text, "   * Child relationship; available when queried.")
				}
				fmt.Fprintln(&text, "   */")
			} else {
				fmt.Fprintf(&text, "  /** Parent relationship via %s; available when queried. */\n", parent.field)
			}
			if parent.nullable {
				types = append(types, "null")
			}
			fmt.Fprintf(&text, "  %s?: %s;\n", fieldName, strings.Join(types, " | "))
			continue
		}
		if child, ok := children[fieldName]; ok {
			if len(child.fields) > 0 {
				fmt.Fprintf(&text, "  /** Child relationship via %s; available when queried. */\n", strings.Join(child.fields, ", "))
			} else {
				fmt.Fprintln(&text, "  /** Child relationship; available when queried. */")
			}
			fmt.Fprintf(&text, "  %s?: %s[];\n", fieldName, aliases[child.target])
			continue
		}
		f := byName[fieldName]
		writeDescribeDocs(&text, "  ", f, false)
		nullable := ""
		if f["nillable"] == true {
			nullable = " | null"
		}
		fmt.Fprintf(&text, "  %s?: %s%s;\n", fieldName, fieldType(f), nullable)
	}
	fmt.Fprintln(&text, "}")
	files := []File{{name + ".d.ts", []byte(text.String())}}
	if snapshots {
		var value any
		if jsonutil.Decode(env.Result, &value, false) != nil {
			return nil, invalid
		}
		b, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return nil, invalid
		}
		files = append(files, File{name + ".describe.json", append(b, '\n')})
	}
	return files, nil
}
