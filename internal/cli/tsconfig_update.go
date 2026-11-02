package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var errTSUpdate = fmt.Errorf("cannot safely update tsconfig.json (invalid or unsupported config)")

// jsoncNode keeps byte offsets into the original document. Only an insertion is
// serialized; comments, whitespace, trailing commas and unrelated keys survive.
type jsoncNode struct {
	start, end, insert int
	kind               byte
	value              any
	fields             map[string]*jsoncNode
	items              []*jsoncNode
}

type jsoncParser struct {
	data []byte
	pos  int
}

func (p *jsoncParser) space() error {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\r', '\n':
			p.pos++
		case '/':
			if p.pos+1 >= len(p.data) {
				return errTSUpdate
			}
			switch p.data[p.pos+1] {
			case '/':
				p.pos += 2
				for p.pos < len(p.data) && p.data[p.pos] != '\n' {
					p.pos++
				}
			case '*':
				end := bytes.Index(p.data[p.pos+2:], []byte("*/"))
				if end < 0 {
					return errTSUpdate
				}
				p.pos += end + 4
			default:
				return errTSUpdate
			}
		default:
			return nil
		}
	}
	return nil
}

func (p *jsoncParser) node(depth int) (*jsoncNode, error) {
	if depth > 128 || p.space() != nil || p.pos >= len(p.data) {
		return nil, errTSUpdate
	}
	n := &jsoncNode{start: p.pos, kind: p.data[p.pos]}
	switch n.kind {
	case '{', '[':
		p.pos++
		n.insert = p.pos
		close := byte(']')
		if n.kind == '{' {
			close = '}'
			n.fields = map[string]*jsoncNode{}
		}
		for {
			if p.space() != nil || p.pos >= len(p.data) {
				return nil, errTSUpdate
			}
			if p.data[p.pos] == close {
				p.pos++
				break
			}
			key := ""
			if n.kind == '{' {
				k, err := p.node(depth + 1)
				if err != nil || k.kind != '"' {
					return nil, errTSUpdate
				}
				key = k.value.(string)
				if _, exists := n.fields[key]; exists {
					return nil, errTSUpdate
				}
				if p.space() != nil || p.pos >= len(p.data) || p.data[p.pos] != ':' {
					return nil, errTSUpdate
				}
				p.pos++
			}
			v, err := p.node(depth + 1)
			if err != nil {
				return nil, err
			}
			if n.kind == '{' {
				n.fields[key] = v
			} else {
				n.items = append(n.items, v)
			}
			n.insert = v.end
			if p.space() != nil || p.pos >= len(p.data) {
				return nil, errTSUpdate
			}
			if p.data[p.pos] == close {
				p.pos++
				break
			}
			if p.data[p.pos] != ',' {
				return nil, errTSUpdate
			}
			p.pos++
		}
	case '"':
		p.pos++
		for p.pos < len(p.data) && p.data[p.pos] != '"' {
			if p.data[p.pos] == '\\' {
				p.pos++
			}
			p.pos++
		}
		if p.pos >= len(p.data) {
			return nil, errTSUpdate
		}
		p.pos++
		if json.Unmarshal(p.data[n.start:p.pos], &n.value) != nil {
			return nil, errTSUpdate
		}
	default:
		for p.pos < len(p.data) && !strings.ContainsRune(" \t\r\n,]}/", rune(p.data[p.pos])) {
			p.pos++
		}
		if !json.Valid(p.data[n.start:p.pos]) {
			return nil, errTSUpdate
		}
	}
	n.end = p.pos
	return n, nil
}

func parseTSConfig(data []byte) (*jsoncNode, error) {
	p := jsoncParser{data: data}
	n, err := p.node(0)
	if err != nil || p.space() != nil || p.pos != len(data) || n.kind != '{' {
		return nil, errTSUpdate
	}
	for _, key := range []string{"include", "files", "exclude"} {
		if v := n.fields[key]; v != nil {
			if v.kind != '[' {
				return nil, errTSUpdate
			}
			for _, item := range v.items {
				if item.kind != '"' {
					return nil, errTSUpdate
				}
			}
		}
	}
	if v := n.fields["compilerOptions"]; v != nil && v.kind != '{' {
		return nil, errTSUpdate
	}
	if v := n.fields["references"]; v != nil && v.kind != '[' {
		return nil, errTSUpdate
	}
	if v := n.fields["extends"]; v != nil && v.kind != '"' {
		if v.kind != '[' {
			return nil, errTSUpdate
		}
		for _, item := range v.items {
			if item.kind != '"' {
				return nil, errTSUpdate
			}
		}
	}
	return n, nil
}

// Inspect each component, then compare the opened descriptor before reading.
// A swapped symlink is never read or used as a write destination.
func readLocalTSConfig(path string) ([]byte, os.FileInfo, error) {
	if !filepath.IsLocal(path) || filepath.Ext(path) != ".json" {
		return nil, nil, errTSUpdate
	}
	current := ""
	var info os.FileInfo
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		var err error
		info, err = os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return nil, nil, errTSUpdate
		}
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errTSUpdate
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, errTSUpdate
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, nil, errTSUpdate
	}
	data, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 {
		return nil, nil, errTSUpdate
	}
	return data, info, nil
}

type tsSourceSelection struct {
	include              []string
	hasInclude, hasFiles bool
}

// Single project-local JSON extends chains only. TypeScript retains the origin
// of inherited globs; materializing them locally must explicitly rebase them.
func tsSelection(n *jsoncNode, path string, seen map[string]bool) (tsSourceSelection, error) {
	var result tsSourceSelection
	// A root include already overrides every inherited include. Appending to
	// it leaves inherited files/options/excludes untouched, so extends can stay
	// opaque (including package names and arrays), with no inherited reads.
	if inc := n.fields["include"]; path == "tsconfig.json" && inc != nil {
		result.hasInclude = true
		for _, item := range inc.items {
			result.include = append(result.include, item.value.(string))
		}
		return result, nil
	}
	if len(seen) >= 16 || seen[path] {
		return result, errTSUpdate
	}
	seen[path] = true
	if base := n.fields["extends"]; base != nil {
		if base.kind != '"' {
			return result, errTSUpdate
		}
		name := base.value.(string)
		if !strings.HasPrefix(name, "./") && !strings.HasPrefix(name, "../") {
			return result, errTSUpdate
		}
		if strings.ContainsAny(name, "\\:$\x00") {
			return result, errTSUpdate
		}
		name = filepath.Clean(filepath.Join(filepath.Dir(path), filepath.FromSlash(name)))
		if filepath.Ext(name) == "" {
			name += ".json"
		}
		data, _, err := readLocalTSConfig(name)
		if err != nil {
			return result, err
		}
		parent, err := parseTSConfig(data)
		if err != nil {
			return result, err
		}
		result, err = tsSelection(parent, name, seen)
		if err != nil {
			return result, err
		}
	}
	if files := n.fields["files"]; files != nil {
		result.hasFiles = true
	}
	if inc := n.fields["include"]; inc != nil {
		result.hasInclude = true
		result.include = nil
		for _, item := range inc.items {
			pattern := item.value.(string)
			if path != "tsconfig.json" {
				// Rebase only a literal prefix; cleaning across a wildcard could
				// change its meaning. Parent paths may reach the project root,
				// but never escape it (including SFDX's ../force-app includes).
				if filepath.IsAbs(pattern) || strings.ContainsAny(pattern, "\\:$\x00") {
					return result, errTSUpdate
				}
				parts := strings.Split(pattern, "/")
				literal := len(parts)
				for i, part := range parts {
					if strings.ContainsAny(part, "*?[") && literal == len(parts) {
						literal = i
					}
					if i >= literal && part == ".." {
						return result, errTSUpdate
					}
				}
				prefix := filepath.Join(filepath.Dir(path), filepath.FromSlash(strings.Join(parts[:literal], "/")))
				if !filepath.IsLocal(prefix) {
					return result, errTSUpdate
				}
				pattern = filepath.ToSlash(prefix)
				if literal < len(parts) {
					pattern += "/" + strings.Join(parts[literal:], "/")
				}
				pattern = strings.TrimPrefix(pattern, "./")
			}
			result.include = append(result.include, pattern)
		}
	}
	// Solution/build-only configs have deliberately empty source selection.
	if n.fields["references"] != nil && !result.hasInclude {
		return result, errTSUpdate
	}
	return result, nil
}

func updateTSConfig(out string) (string, error) {
	data, info, err := readLocalTSConfig("tsconfig.json")
	if err != nil {
		return "", err
	}
	root, err := parseTSConfig(data)
	if err != nil {
		return "", err
	}
	selection, err := tsSelection(root, "tsconfig.json", map[string]bool{})
	if err != nil {
		return "", err
	}
	pattern := filepath.ToSlash(filepath.Clean(out)) + "/**/*.d.ts"
	for _, item := range selection.include {
		if strings.TrimPrefix(item, "./") == pattern {
			return "Unchanged", nil
		}
	}
	var pos int
	var text string
	if inc := root.fields["include"]; inc != nil {
		encoded, _ := json.Marshal(pattern)
		pos = inc.insert
		text = string(encoded)
		if len(inc.items) > 0 {
			text = ", " + text
		}
	} else {
		if !selection.hasInclude && !selection.hasFiles {
			// An inherited implicit default may depend on compiler options at its
			// origin. Do not invent source selection for that unsupported boundary.
			if root.fields["extends"] != nil {
				return "", errTSUpdate
			}
			selection.include = []string{"**/*"}
		}
		selection.include = append(selection.include, pattern)
		encoded, _ := json.Marshal(selection.include)
		pos = root.insert
		text = `"include": ` + string(encoded)
		if len(root.fields) > 0 {
			text = ", " + text
		}
	}
	candidate := append([]byte{}, data[:pos]...)
	candidate = append(candidate, []byte(text)...)
	candidate = append(candidate, data[pos:]...)
	if _, err := parseTSConfig(candidate); err != nil {
		return "", err
	}
	// Stage alongside the target for atomic rename, preserving the user's mode.
	// Deferred cleanup also covers write/close/rename failures.
	f, err := os.CreateTemp(".", ".tsconfig-*.tmp")
	if err != nil {
		return "", errTSUpdate
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(candidate); err == nil {
		err = f.Chmod(info.Mode())
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return "", errTSUpdate
	}
	current, err := os.Lstat("tsconfig.json")
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(info, current) {
		return "", errTSUpdate
	}
	// Detect concurrent in-place edits as well as path replacement.
	latest, _, err := readLocalTSConfig("tsconfig.json")
	if err != nil || !bytes.Equal(data, latest) {
		return "", errTSUpdate
	}
	if os.Rename(f.Name(), "tsconfig.json") != nil {
		return "", errTSUpdate
	}
	return "Updated", nil
}
