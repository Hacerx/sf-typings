package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/Hacerx/sf-typings/internal/auth"
	"github.com/Hacerx/sf-typings/internal/config"
	"github.com/Hacerx/sf-typings/internal/generator"
)

// Run executes the public CLI with explicit output streams and returns its exit status.
func Run(args []string, stdout, stderr io.Writer) int {
	return runWithDeps(args, stdout, stderr, auth.Local, auth.Token, nil)
}

func runWithDeps(args []string, stdout, stderr io.Writer, local, fallback auth.Resolver, client *http.Client) int {
	flags := flag.NewFlagSet("sf-typings", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	cfg := flags.String("config", "", "JSON configuration path")
	out := flags.String("out", config.DefaultOut, "output directory (default sf-typings)")
	only := flags.String("only", "all", "update all, apex (and required sObjects), or sobjects")
	snapshots := flags.Bool("snapshots", false, "also save complete per-object Describe JSON")
	allChildren := flags.Bool("all-children", false, "include recursive children through level 5 (configured root is level 1)")
	filterRelated := flags.Bool("filter-related-objects", true, "require related objects to match object selectors")
	timeout := flags.String("timeout", config.DefaultTimeout.String(), "overall auth/Describe budget: positive Go duration (e.g. 5m), default 35s")
	initConfig := flags.Bool("init-tsconfig", false, "create an initial root tsconfig.json without overwriting")
	help := flags.Bool("help", false, "show usage")
	usage := "Usage: sf-typings -config config.json [-out generated] [-only all|apex|sobjects] [-snapshots] [-all-children] [-timeout 5m] [-filter-related-objects=false]\n"
	if err := flags.Parse(args); err != nil {
		fmt.Fprint(stderr, "sf-typings: invalid arguments\n"+usage)
		return 1
	}
	if *initConfig {
		return initTSConfig(flags, *cfg, *out, stdout, stderr)
	}
	if *help {
		fmt.Fprint(stdout, usage+"Config: {\"orgAlias\":\"dev\",\"objects\":[\"Account\"],\"apexSources\":[\"classes\"],\"out\":\"generated\",\"only\":\"all\",\"snapshots\":false,\"allChildren\":false,\"filterRelatedObjects\":true,\"timeout\":\"35s\"}\nExplicit -out/-only/-snapshots/-all-children/-timeout/-filter-related-objects override config, including empty out (invalid) and false. Omitted out defaults to sf-typings in the CLI working directory; run from your project root. JSON out or -out replaces that root directly, without appending sf-typings. Explicit empty out is invalid; -config/-help are bootstrap flags only.\nOutput: out/apex/*.d.ts and out/sobjects/*.d.ts (plus optional *.describe.json). -only all (default) updates both; apex refreshes inferred sObjects but ignores configured object roots; sobjects ignores Apex sources and preserves Apex files. Selected mode requires applicable input. Empty/invalid only fails with: invalid only (use all, apex, or sobjects); JSON null/non-string is invalid config JSON and invalid config cannot be repaired by flags. Counts include emitted objects (including Apex dependencies) and Apex classes only when selected. Legacy flat files are not moved/deleted: manually remove them to avoid duplicate ambient declarations. Objects use REST Describe. If local org URL is missing, sf org display supplies only the URL; sf org auth show-access-token supplies the fallback token. A local-token HTTP 401 triggers one token-only REST retry. Apex-only primitive/local-wrapper runs use local .cls files without org access. Apex methods support nested List/arrays, Map keys serialized as strings/numbers, and reachable local AuraEnabled wrapper members; external candidates use the org catalog and automatic Describe (including Schema-qualified names), requiring a valid orgAlias or failing with: invalid org alias. Unknown catalog names, SObject and Object remain unknown. Inferred roots do not broaden configured relationship selectors or default child emission. Apex is validated before network access; metadata failure publishes no outputs. Never retrieves records.\nObjects accept exact API names and case-sensitive '*' patterns (zero or more characters), e.g. AC_* or *__c; wildcard roots are expanded from the org catalog and deduplicated. No matching objects fails with: object patterns matched no objects. Related objects must match the selectors by default (JSON filterRelatedObjects: true); -filter-related-objects=false or JSON false restores unrestricted relationships. Explicit flags override JSON. Excluded relationships/imports are omitted, lookup IDs and complete snapshots remain intact. Configured objects are roots: allowed transitive parents are automatically described; self/mutual cycles are fetched once per object per attempt. Parent properties use type-only imports and polymorphic unions, are optional for query projections, and allow null when the ID is nillable. Snapshots cover every discovered object. Named visible child relationships are optional Apex-style T[] properties, not REST envelopes. By default only configured roots emit/discover children; -all-children enables recursive children through level 5: configured roots count as level 1, child edges add 1, and parent edges cost 0 without resetting the level. Level-5 objects emit/discover no children; transitive parents remain enabled. Shared objects use their shallowest level, and configured roots always start at 1. This bounds depth, not sibling count or breadth. It limits graph discovery, not nesting through recursive/shared interfaces. The graph shares one overall budget across auth, all Describe nodes and the single token retry: -timeout or JSON timeout sets a positive Go duration (e.g. 5m), default 35s. Invalid durations fail with: invalid timeout (use a positive Go duration, e.g. 5m). Per-request and auth CLI caps remain 15s. Permissions, timeout, or malformed metadata failures publish no new outputs.\n")
		fmt.Fprint(stdout, tsconfigUsage+tsconfigHelp)
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
			case "only":
				c.Only = *only
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
		err = config.ValidateOnly(c.Only)
		if err == nil {
			budget, err = config.ParseTimeout(c.Timeout)
		}
		if err == nil && c.Only == "apex" && len(c.ApexSources) == 0 {
			err = fmt.Errorf("apexSources must be nonempty for only apex")
		}
		if err == nil && c.Only == "sobjects" && len(c.Objects) == 0 {
			err = fmt.Errorf("objects must be nonempty for only sobjects")
		}
		if err == nil && c.Out == "" {
			fmt.Fprint(stderr, "sf-typings: invalid arguments\n"+usage)
			return 1
		}
	}
	apexCount, objectCount := 0, 0
	if err == nil {
		var files []generator.File
		var plan *generator.ApexPlan
		sources, objects := c.ApexSources, c.Objects
		if c.Only == "sobjects" {
			sources = nil
		}
		if c.Only == "apex" {
			objects = nil
		}
		plan, err = generator.AnalyzeApex(sources)
		var candidates []string
		if err == nil {
			candidates = plan.Candidates()
		}
		resolved := make(map[string]string)
		if err == nil && len(candidates) > 0 && !config.ValidOrgAlias(c.OrgAlias) {
			err = fmt.Errorf("invalid org alias")
		}
		if err == nil && (len(objects) > 0 || len(candidates) > 0) {
			files, err = objectOutputsApex(context.Background(), c.OrgAlias, objects, candidates, resolved, c.Snapshots, c.AllChildren, c.FilterRelatedObjects, budget, local, fallback, client)
			for i := range files {
				if strings.HasSuffix(files[i].Name, ".d.ts") {
					objectCount++
				}
				files[i].Name = filepath.Join("sobjects", files[i].Name)
			}
		}
		var apexFiles []generator.File
		if err == nil {
			apexFiles, err = plan.Outputs(resolved)
			apexCount = len(apexFiles)
			for i := range apexFiles {
				apexFiles[i].Name = filepath.Join("apex", apexFiles[i].Name)
			}
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
	if c.Only == "sobjects" || len(c.ApexSources) == 0 {
		fmt.Fprintf(stdout, "Generated %d object(s).\n", objectCount)
	} else {
		fmt.Fprintf(stdout, "Generated %d object(s), %d Apex class(es).\n", objectCount, apexCount)
	}
	return 0
}
