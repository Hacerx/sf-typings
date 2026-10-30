package cli

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"sf-typings/internal/auth"
	"sf-typings/internal/generator"
	"sf-typings/internal/salesforce"
)

func objectOutputs(ctx context.Context, alias string, names []string, snapshots bool, local, fallback auth.Resolver, httpClient *http.Client) ([]generator.File, error) {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	creds, err := local(ctx, alias)
	needInstance := err != nil || creds.Instance == ""
	if needInstance {
		// Do not reuse a token from an incomplete local org record.
		creds = auth.Credentials{}
		creds.Instance, err = auth.InstanceURL(ctx, alias)
		if err != nil {
			return nil, fmt.Errorf("org instance unavailable")
		}
	}
	if _, ok := salesforce.TrustedInstance(creds.Instance); !ok {
		return nil, fmt.Errorf("untrusted org instance")
	}
	usingCLI := needInstance || creds.Token == ""
	if usingCLI {
		instance := creds.Instance
		creds, err = fallback(ctx, alias)
		creds.Instance = instance
	}
	if err != nil || creds.Token == "" {
		return nil, fmt.Errorf("org token unavailable")
	}
	files, err := restOutputs(ctx, names, snapshots, creds, httpClient)
	if !usingCLI && salesforce.IsUnauthorized(err) {
		// Keep only the validated local instance URL; discard the stale bearer.
		instance := creds.Instance
		creds = auth.Credentials{}
		creds, err = fallback(ctx, alias)
		if err != nil || creds.Token == "" {
			return nil, fmt.Errorf("org token unavailable")
		}
		creds.Instance = instance
		return restOutputs(ctx, names, snapshots, creds, httpClient)
	}
	return files, err
}

// Collect in memory on each attempt; a failed or stale-token attempt is never published.
func restOutputs(ctx context.Context, names []string, snapshots bool, creds auth.Credentials, client *http.Client) ([]generator.File, error) {
	var files []generator.File
	err := salesforce.DescribeGraph(ctx, names, creds, client, func(name string, raw []byte) ([]string, error) {
		generated, parents, err := generator.GenerateWithParents(name, raw, snapshots)
		if err == nil {
			files = append(files, generated...)
		}
		return parents, err
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
