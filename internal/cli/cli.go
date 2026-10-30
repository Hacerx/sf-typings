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
	help := flags.Bool("help", false, "show usage")
	usage := "Usage: sf-typings -config config.json -out generated [-snapshots]\n"
	if err := flags.Parse(args); err != nil {
		fmt.Fprint(stderr, "sf-typings: invalid arguments\n"+usage)
		return 1
	}
	if *help {
		fmt.Fprint(stdout, usage+"Config: {\"orgAlias\":\"dev\",\"objects\":[\"Account\"],\"apexSources\":[\"classes\"]}\nObjects use REST Describe. If local org URL is missing, sf org display supplies only the URL; sf org auth show-access-token supplies the fallback token. A local-token HTTP 401 triggers one token-only REST retry. Apex-only runs use local .cls files. Never retrieves records.\nConfigured objects are roots: transitive parents are automatically described, including unconfigured targets; self/mutual cycles are fetched once per object per attempt. Parent properties use type-only imports and polymorphic unions, are optional for query projections, and allow null when the ID is nillable. Snapshots cover every discovered object. No child relationships are generated or fetched. The graph shares a 35-second overall budget; permissions, timeout, or malformed metadata failures publish no new outputs.\n")
		return 0
	}
	if *cfg == "" || *out == "" || flags.NArg() != 0 {
		fmt.Fprint(stderr, "sf-typings: invalid arguments\n"+usage)
		return 1
	}
	c, err := config.Load(*cfg)
	apexCount, objectCount := 0, 0
	if err == nil {
		var files []generator.File
		if len(c.Objects) > 0 {
			files, err = objectOutputs(context.Background(), c.OrgAlias, c.Objects, *snapshots, local, fallback, client)
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
			err = save(*out, files)
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
