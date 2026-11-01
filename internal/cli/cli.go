package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"

	"sf-typings/internal/auth"
	"sf-typings/internal/config"
	"sf-typings/internal/generator"
)

// Run executes the public CLI with explicit output streams and returns its exit status.
func Run(args []string, stdout, stderr io.Writer) int {
	return runWithDeps(args, stdout, stderr, auth.Local, auth.Token, nil)
}

func runWithDeps(args []string, stdout, stderr io.Writer, local, fallback auth.Resolver, client *http.Client) int {
	flags := flag.NewFlagSet("sf-typings", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	cfg := flags.String("config", "", "JSON configuration path")
	out := flags.String("out", "", "output directory")
	snapshots := flags.Bool("snapshots", false, "also save complete per-object Describe JSON")
	allChildren := flags.Bool("all-children", false, "include recursive children through level 5 (configured root is level 1)")
	filterRelated := flags.Bool("filter-related-objects", true, "require related objects to match object selectors")
	timeout := flags.String("timeout", config.DefaultTimeout.String(), "overall auth/Describe budget: positive Go duration (e.g. 5m), default 35s")
	help := flags.Bool("help", false, "show usage")
	usage := "Usage: sf-typings -config config.json [-out generated] [-snapshots] [-all-children] [-timeout 5m] [-filter-related-objects=false]\n"
	if err := flags.Parse(args); err != nil {
		fmt.Fprint(stderr, "sf-typings: invalid arguments\n"+usage)
		return 1
	}
	if *help {
		fmt.Fprint(stdout, usage+"Config: {\"orgAlias\":\"dev\",\"objects\":[\"Account\"],\"apexSources\":[\"classes\"],\"out\":\"generated\",\"snapshots\":false,\"allChildren\":false,\"filterRelatedObjects\":true,\"timeout\":\"35s\"}\nExplicit -out/-snapshots/-all-children/-timeout/-filter-related-objects override config, including empty out (invalid) and false. A nonempty output directory is required; -config/-help are bootstrap flags only.\nObjects use REST Describe. If local org URL is missing, sf org display supplies only the URL; sf org auth show-access-token supplies the fallback token. A local-token HTTP 401 triggers one token-only REST retry. Apex-only runs use local .cls files. Never retrieves records.\nObjects accept exact API names and case-sensitive '*' patterns (zero or more characters), e.g. AC_* or *__c; wildcard roots are expanded from the org catalog and deduplicated. No matching objects fails with: object patterns matched no objects. Related objects must match the selectors by default (JSON filterRelatedObjects: true); -filter-related-objects=false or JSON false restores unrestricted relationships. Explicit flags override JSON. Excluded relationships/imports are omitted, lookup IDs and complete snapshots remain intact. Configured objects are roots: allowed transitive parents are automatically described; self/mutual cycles are fetched once per object per attempt. Parent properties use type-only imports and polymorphic unions, are optional for query projections, and allow null when the ID is nillable. Snapshots cover every discovered object. Named visible child relationships are optional Apex-style T[] properties, not REST envelopes. By default only configured roots emit/discover children; -all-children enables recursive children through level 5: configured roots count as level 1, child edges add 1, and parent edges cost 0 without resetting the level. Level-5 objects emit/discover no children; transitive parents remain enabled. Shared objects use their shallowest level, and configured roots always start at 1. This bounds depth, not sibling count or breadth. It limits graph discovery, not nesting through recursive/shared interfaces. The graph shares one overall budget across auth, all Describe nodes and the single token retry: -timeout or JSON timeout sets a positive Go duration (e.g. 5m), default 35s. Invalid durations fail with: invalid timeout (use a positive Go duration, e.g. 5m). Per-request and auth CLI caps remain 15s. Permissions, timeout, or malformed metadata failures publish no new outputs.\n")
		return 0
	}
	if *cfg == "" || flags.NArg() != 0 {
		fmt.Fprint(stderr, "sf-typings: invalid arguments\n"+usage)
		return 1
	}
	c, err := config.Load(*cfg)
	budget := config.DefaultTimeout
	if err == nil {
		// Visit records presence, not truthiness: explicit false and empty
		// strings must override the corresponding configuration values.
		flags.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "out":
				c.Out = *out
			case "snapshots":
				c.Snapshots = *snapshots
			case "all-children":
				c.AllChildren = *allChildren
			case "timeout":
				c.Timeout = *timeout
			case "filter-related-objects":
				c.FilterRelatedObjects = *filterRelated
			}
		})
		budget, err = config.ParseTimeout(c.Timeout)
		if err == nil && c.Out == "" {
			fmt.Fprint(stderr, "sf-typings: invalid arguments\n"+usage)
			return 1
		}
	}
	apexCount, objectCount := 0, 0
	if err == nil {
		var files []generator.File
		if len(c.Objects) > 0 {
			files, err = objectOutputsSelected(context.Background(), c.OrgAlias, c.Objects, c.Snapshots, c.AllChildren, c.FilterRelatedObjects, budget, local, fallback, client)
			for _, file := range files {
				if strings.HasSuffix(file.Name, ".d.ts") {
					objectCount++
				}
			}
		}
		var apexFiles []generator.File
		if err == nil {
			apexFiles, err = generator.ApexOutputs(c.ApexSources)
			apexCount = len(apexFiles)
			files = append(files, apexFiles...)
		}
		if err == nil {
			seen := make(map[string]bool)
			for _, f := range files {
				if seen[strings.ToLower(f.Name)] {
					err = fmt.Errorf("duplicate output name")
					break
				}
				seen[strings.ToLower(f.Name)] = true
			}
		}
		if err == nil {
			err = save(c.Out, files)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "sf-typings: %s\n", err)
		return 1
	}
	if len(c.ApexSources) == 0 {
		fmt.Fprintf(stdout, "Generated %d object(s).\n", objectCount)
	} else {
		fmt.Fprintf(stdout, "Generated %d object(s), %d Apex class(es).\n", objectCount, apexCount)
	}
	return 0
}
