package config

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"sf-typings/internal/jsonutil"
)

// Config selects object metadata and local Apex sources for a single run.
type Config struct {
	OrgAlias             string   `json:"orgAlias"`
	Objects              []string `json:"objects"`
	ApexSources          []string `json:"apexSources"`
	Out                  string   `json:"out"`
	Only                 string   `json:"only"`
	Snapshots            bool     `json:"snapshots"`
	AllChildren          bool     `json:"allChildren"`
	FilterRelatedObjects bool     `json:"filterRelatedObjects"`
	Timeout              string   `json:"timeout"`
}

// DefaultOut is the output root relative to the CLI working directory.
const DefaultOut = "sf-typings"

// DefaultTimeout is the shared authentication and Describe graph budget.
const DefaultTimeout = 35 * time.Second

// ParseTimeout accepts only positive Go durations, without exposing input in errors.
func ParseTimeout(value string) (time.Duration, error) {
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid timeout (use a positive Go duration, e.g. 5m)")
	}
	return d, nil
}

var apiName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
var aliasName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// ValidOrgAlias validates an alias before inferred metadata access too.
func ValidOrgAlias(alias string) bool { return aliasName.MatchString(alias) }

// ValidateOnly accepts the category selector without normalizing explicit input.
func ValidateOnly(value string) error {
	switch value {
	case "all", "apex", "sobjects":
		return nil
	default:
		return fmt.Errorf("invalid only (use all, apex, or sobjects)")
	}
}

// Load decodes one strict configuration and validates it before any org access.
func Load(path string) (Config, error) {
	c := Config{Out: DefaultOut, Timeout: DefaultTimeout.String(), FilterRelatedObjects: true, Only: "all"}
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
		case "out", "only", "snapshots", "allchildren", "timeout", "filterrelatedobjects":
			if value == nil {
				return c, fmt.Errorf("invalid config JSON")
			}
		}
	}
	if err := ValidateOnly(c.Only); err != nil {
		return c, err
	}
	if _, err := ParseTimeout(c.Timeout); err != nil {
		return c, err
	}
	if len(c.Objects) == 0 && len(c.ApexSources) == 0 {
		return c, fmt.Errorf("objects or apexSources must be nonempty")
	}
	if len(c.Objects) > 0 && !ValidOrgAlias(c.OrgAlias) {
		return c, fmt.Errorf("invalid org alias")
	}
	for _, source := range c.ApexSources {
		if source == "" {
			return c, fmt.Errorf("invalid Apex source path")
		}
	}
	seen := make(map[string]bool)
	for _, name := range c.Objects {
		if !ValidObjectSelector(name) {
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
