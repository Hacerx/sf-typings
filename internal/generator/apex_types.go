package generator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/octoberswimmer/apexfmt/parser"
)

// apexClass retains parser nodes so type resolution never reparses source text.
type apexClass struct {
	name, owner, symbol string
	parent              *apexClass
	node                parser.IClassDeclarationContext
	fields              []string
	reachable           bool
}

type apexTypes struct {
	classes    map[string]*apexClass
	ordered    []*apexClass
	candidates map[string]bool
	objects    map[string]string
}

func apexModifiers(mods []parser.IModifierContext) (enabled, accessible, static, transient bool) {
	for _, mod := range mods {
		if a := mod.Annotation(); a != nil && strings.EqualFold(a.QualifiedName().GetText(), "AuraEnabled") {
			enabled = true
		}
		accessible = accessible || mod.PUBLIC() != nil || mod.GLOBAL() != nil
		static = static || mod.STATIC() != nil
		transient = transient || mod.TRANSIENT() != nil
	}
	return
}

func (r *apexTypes) add(node parser.IClassDeclarationContext, parent *apexClass, owner string) (*apexClass, error) {
	name := node.Id().GetText()
	if !apiName.MatchString(name) {
		return nil, fmt.Errorf("invalid Apex source")
	}
	if parent != nil {
		name = parent.name + "." + name
	}
	key := strings.ToLower(name)
	if r.classes[key] != nil {
		return nil, fmt.Errorf("invalid Apex source")
	}
	c := &apexClass{name: name, owner: owner, parent: parent, node: node}
	r.classes[key] = c
	for _, decl := range node.ClassBody().AllClassBodyDeclaration() {
		if member := decl.MemberDeclaration(); member != nil && member.ClassDeclaration() != nil {
			if _, err := r.add(member.ClassDeclaration(), c, owner); err != nil {
				return nil, err
			}
		}
	}
	return c, nil
}

func (r *apexTypes) index() {
	for _, c := range r.classes {
		r.ordered = append(r.ordered, c)
	}
	sort.Slice(r.ordered, func(i, j int) bool { return r.ordered[i].name < r.ordered[j].name })
	for i, c := range r.ordered {
		c.symbol = fmt.Sprintf("Type%d", i+1)
	}
}

func (r *apexTypes) local(name string, scope *apexClass) *apexClass {
	for c := scope; c != nil; c = c.parent {
		if found := r.classes[strings.ToLower(c.name+"."+name)]; found != nil {
			return found
		}
	}
	return r.classes[strings.ToLower(name)]
}

func apexClassType(c *apexClass) string {
	return fmt.Sprintf("import('@sf-typings/apex/%s').%s", c.owner, c.symbol)
}

// reference resolves local wrappers before catalog-confirmed sObjects.
func (r *apexTypes) reference(name string, scope *apexClass) (string, error) {
	c := r.local(name, scope)
	if c == nil {
		if strings.HasPrefix(strings.ToLower(name), "schema.") {
			name = name[len("schema."):]
		}
		if apiName.MatchString(name) && !strings.EqualFold(name, "SObject") && !strings.EqualFold(name, "Object") {
			r.candidates[name] = true
			if object := r.objects[strings.ToLower(name)]; object != "" {
				return fmt.Sprintf("import('./%s').%s", object, object), nil
			}
		}
		return "unknown", nil
	}
	if c.node.TypeRef() != nil {
		return "unknown", nil // Inheritance needs an explicit serialized-member model.
	}
	if !c.reachable {
		c.reachable = true // Mark before following fields: local/cross-file cycles terminate.
		seen := make(map[string]bool)
		for _, decl := range c.node.ClassBody().AllClassBodyDeclaration() {
			member := decl.MemberDeclaration()
			if member == nil {
				continue
			}
			enabled, accessible, static, transient := apexModifiers(decl.AllModifier())
			if !enabled || !accessible || static || transient {
				continue
			}
			var names []string
			var typ parser.ITypeRefContext
			if field := member.FieldDeclaration(); field != nil {
				typ = field.TypeRef()
				for _, v := range field.VariableDeclarators().AllVariableDeclarator() {
					names = append(names, v.Id().GetText())
				}
			} else if prop := member.PropertyDeclaration(); prop != nil {
				readable := false
				for _, block := range prop.AllPropertyBlock() {
					if block.Getter() == nil {
						continue
					}
					readable = true
					for _, mod := range block.AllModifier() {
						if mod.PRIVATE() != nil || mod.PROTECTED() != nil {
							readable = false
						}
					}
				}
				if !readable {
					continue
				}
				typ = prop.TypeRef()
				names = append(names, prop.Id().GetText())
			} else {
				continue
			}
			value, err := r.resolve(typ, c)
			if err != nil {
				return "", err
			}
			for _, name := range names {
				key := strings.ToLower(name)
				if !apiName.MatchString(name) || seen[key] {
					return "", fmt.Errorf("invalid Apex source")
				}
				seen[key] = true
				c.fields = append(c.fields, fmt.Sprintf("    %q: %s;\n", name, value))
			}
		}
	}
	return apexClassType(c), nil
}

func (r *apexTypes) resolve(t parser.ITypeRefContext, scope *apexClass) (string, error) {
	if t == nil {
		return "unknown", nil
	}
	names := t.AllTypeName()
	if len(names) == 0 {
		return "unknown", nil
	}
	var result string
	if len(names) == 1 && (names[0].LIST() != nil || names[0].MAP() != nil) {
		n := names[0]
		args := n.TypeArguments()
		var types []parser.ITypeRefContext
		if args != nil && args.TypeList() != nil {
			types = args.TypeList().AllTypeRef()
		}
		result = "unknown"
		if n.LIST() != nil && len(types) == 1 {
			item, err := r.resolve(types[0], scope)
			if err != nil {
				return "", err
			}
			result = item + "[]"
		} else if n.MAP() != nil && len(types) == 2 {
			key, err := r.resolve(types[0], scope)
			if err != nil {
				return "", err
			}
			value, err := r.resolve(types[1], scope)
			if err != nil {
				return "", err
			}
			// JSON objects stringify Apex keys; numeric keys also support numeric indexing.
			if key == "number" {
				result = "{ [key: number]: " + value + " }"
			} else if key == "string" || key == "boolean" {
				result = "{ [key: string]: " + value + " }"
			}
		}
	} else {
		var parts []string
		for _, n := range names {
			if n.TypeArguments() != nil || n.Id() == nil {
				return "unknown", nil
			}
			parts = append(parts, n.Id().GetText())
		}
		name := strings.Join(parts, ".")
		// Lexically scoped local classes take precedence over external references.
		if r.local(name, scope) != nil {
			var err error
			result, err = r.reference(name, scope)
			if err != nil {
				return "", err
			}
		} else {
			switch strings.ToLower(name) {
			case "void":
				result = "void"
			case "string", "id", "date", "datetime", "time":
				result = "string"
			case "integer", "long", "double", "decimal":
				result = "number"
			case "boolean":
				result = "boolean"
			default:
				var err error
				result, err = r.reference(name, scope)
				if err != nil {
					return "", err
				}
			}
		}
	}
	if arrays := t.ArraySubscripts(); arrays != nil {
		result += strings.Repeat("[]", len(arrays.AllLBRACK()))
	}
	return result, nil
}

func (r *apexTypes) declarations(owner string) string {
	var text strings.Builder
	for _, c := range r.ordered {
		if !c.reachable || c.owner != owner {
			continue
		}
		if text.Len() == 0 {
			fmt.Fprintf(&text, "declare module '@sf-typings/apex/%s' {\n", owner)
		}
		fmt.Fprintf(&text, "  export interface %s {\n", c.symbol)
		for _, field := range c.fields {
			text.WriteString(field)
		}
		fmt.Fprintln(&text, "  }")
	}
	if text.Len() > 0 {
		fmt.Fprintln(&text, "}")
	}
	return text.String()
}
