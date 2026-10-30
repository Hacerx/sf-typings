package config

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"sf-typings/internal/jsonutil"
)

// Config selects object metadata and local Apex sources for a single run.
type Config struct {
	OrgAlias    string   `json:"orgAlias"`
	Objects     []string `json:"objects"`
	ApexSources []string `json:"apexSources"`
	Out         string   `json:"out"`
	Snapshots   bool     `json:"snapshots"`
	AllChildren bool     `json:"allChildren"`
}

var apiName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
var aliasName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// Load decodes one strict configuration and validates it before any org access.
func Load(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("cannot read config")
	}
	if err := jsonutil.Decode(b, &c, true); err != nil {
		return c, fmt.Errorf("invalid config JSON")
	}
	// JSON null is not a string or boolean option. encoding/json otherwise
	// accepts it silently for these scalar Go fields.
	var values map[string]any
	if jsonutil.Decode(b, &values, false) != nil {
		return c, fmt.Errorf("invalid config JSON")
	}
	for key, value := range values {
		switch strings.ToLower(key) {
		case "out", "snapshots", "allchildren":
			if value == nil {
				return c, fmt.Errorf("invalid config JSON")
			}
		}
	}
	if len(c.Objects) == 0 && len(c.ApexSources) == 0 {
		return c, fmt.Errorf("objects or apexSources must be nonempty")
	}
	if len(c.Objects) > 0 && !aliasName.MatchString(c.OrgAlias) {
		return c, fmt.Errorf("invalid org alias")
	}
	for _, source := range c.ApexSources {
		if source == "" {
			return c, fmt.Errorf("invalid Apex source path")
		}
	}
	seen := make(map[string]bool)
	for _, name := range c.Objects {
		if !apiName.MatchString(name) {
			return c, fmt.Errorf("invalid object API name")
		}
		if seen[name] {
			return c, fmt.Errorf("duplicate object API name")
		}
		seen[name] = true
	}
	sort.Strings(c.Objects)
	return c, nil
}
