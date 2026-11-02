package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"sf-typings/internal/config"
	"sf-typings/internal/jsonutil"
)

const tsconfigUsage = "Usage: sf-typings -init-tsconfig [-config config.json] [-out generated]\n"
const tsconfigHelp = "TypeScript setup: run -init-tsconfig from the SFDX project root to create tsconfig.json or enrich an existing regular JSONC config with chosen out declarations, preserving source selection and comments. Prints Created, Updated, or Unchanged; unsafe/unsupported configs fail without changes. Explicit root include preserves opaque string/array extends; otherwise supports single project-local JSON extends chains with explicit include/files, not package inheritance. No references-only builds. New configs include packageDirectories paths. Never follows target symlinks. Only -init-tsconfig/-config/-out/-help are accepted. No generation, authentication, installs, or deployment. Install TypeScript and @salesforce/lightning-types yourself: npm install --save-dev typescript @salesforce/lightning-types. Check with npx tsc --noEmit; compile separately before deployment. See examples/sfdx-typescript/README.md. -init-tsconfig is a bootstrap flag, not a JSON option.\n"

// Bootstrap is deliberately separate from generation: config is validated, but
// source paths and credentials are never resolved.
func initTSConfig(flags *flag.FlagSet, configPath, out string, stdout, stderr io.Writer) int {
	invalid := flags.NArg() != 0
	hasConfig, hasOut := false, false
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "config":
			hasConfig = true
		case "out":
			hasOut = true
		case "init-tsconfig", "help":
		default:
			invalid = true
		}
	})
	if invalid {
		fmt.Fprint(stderr, "sf-typings: invalid arguments\n"+tsconfigUsage)
		return 1
	}
	if flags.Lookup("help").Value.String() == "true" {
		return Run([]string{"-help"}, stdout, stderr)
	}
	var err error
	if hasConfig {
		var c config.Config
		c, err = config.Load(configPath)
		if !hasOut {
			out = c.Out
		}
	}
	action := "Created"
	if err == nil {
		if out == "" {
			err = fmt.Errorf("invalid output directory")
		} else if info, inspectErr := os.Lstat("tsconfig.json"); inspectErr == nil {
			if !info.Mode().IsRegular() {
				err = fmt.Errorf("tsconfig.json already exists")
			} else {
				action, err = updateTSConfig(out)
			}
		} else if !os.IsNotExist(inspectErr) {
			err = fmt.Errorf("cannot inspect tsconfig.json")
		} else {
			err = createTSConfig(out)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "sf-typings: %s\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s tsconfig.json.\n", action)
	return 0
}

func createTSConfig(out string) error {
	if out == "" {
		return fmt.Errorf("invalid output directory")
	}
	if _, err := os.Lstat("tsconfig.json"); err == nil {
		return fmt.Errorf("tsconfig.json already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("cannot inspect tsconfig.json")
	}
	data, err := os.ReadFile("sfdx-project.json")
	if err != nil {
		return fmt.Errorf("cannot read sfdx-project.json")
	}
	var project struct {
		PackageDirectories []struct {
			Path string `json:"path"`
		} `json:"packageDirectories"`
	}
	// SFDX owns other project fields; unlike our runtime config they are allowed.
	if jsonutil.Decode(data, &project, false) != nil {
		return fmt.Errorf("invalid sfdx-project.json")
	}
	if len(project.PackageDirectories) == 0 {
		return fmt.Errorf("invalid package directory path")
	}
	includes := []string{}
	seen := make(map[string]bool)
	for _, pkg := range project.PackageDirectories {
		if !localPackageDirectory(pkg.Path) {
			return fmt.Errorf("invalid package directory path")
		}
		name := filepath.ToSlash(filepath.Clean(pkg.Path)) + "/**/*.ts"
		if !seen[name] {
			includes = append(includes, name)
			seen[name] = true
		}
	}
	includes = append(includes, filepath.ToSlash(filepath.Clean(out))+"/**/*.d.ts")
	template := struct {
		CompilerOptions map[string]any `json:"compilerOptions"`
		Include         []string       `json:"include"`
		Exclude         []string       `json:"exclude"`
	}{
		CompilerOptions: map[string]any{
			"target": "ESNext", "module": "ESNext", "moduleResolution": "Bundler",
			"strict": true, "noEmit": true, "types": []string{"@salesforce/lightning-types"},
		},
		Include: includes,
		Exclude: []string{"node_modules"},
	}
	data, err = json.MarshalIndent(template, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot encode tsconfig.json")
	}
	// O_EXCL closes the target-check/create race and rejects dangling symlinks.
	f, err := os.OpenFile("tsconfig.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if os.IsExist(err) {
		return fmt.Errorf("tsconfig.json already exists")
	}
	if err != nil {
		return fmt.Errorf("cannot create tsconfig.json")
	}
	_, writeErr := f.Write(append(data, '\n'))
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		// Do not remove a path another process could have replaced after creation.
		return fmt.Errorf("cannot write tsconfig.json")
	}
	return nil
}

// Reject traversal, glob syntax and links in every existing package component.
// Inspect directories only, never package source contents or org data.
func localPackageDirectory(path string) bool {
	if !filepath.IsLocal(path) || strings.ContainsAny(path, "\\:*?[]\x00") {
		return false
	}
	current := ""
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." || strings.TrimSpace(part) != part {
			return false
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}
