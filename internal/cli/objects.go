package cli

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"sf-typings/internal/auth"
	"sf-typings/internal/config"
	"sf-typings/internal/generator"
	"sf-typings/internal/salesforce"
)

func objectOutputs(ctx context.Context, alias string, names []string, snapshots bool, local, fallback auth.Resolver, httpClient *http.Client) ([]generator.File, error) {
	return objectOutputsWithOptions(ctx, alias, names, snapshots, false, local, fallback, httpClient)
}

func objectOutputsWithOptions(ctx context.Context, alias string, names []string, snapshots, allChildren bool, local, fallback auth.Resolver, httpClient *http.Client) ([]generator.File, error) {
	return objectOutputsWithTimeout(ctx, alias, names, snapshots, allChildren, config.DefaultTimeout, local, fallback, httpClient)
}

func objectOutputsWithTimeout(ctx context.Context, alias string, names []string, snapshots, allChildren bool, budget time.Duration, local, fallback auth.Resolver, httpClient *http.Client) ([]generator.File, error) {
	ctx, cancel := context.WithTimeout(ctx, budget)
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
	files, err := restOutputsWithOptions(ctx, names, snapshots, allChildren, creds, httpClient)
	if !usingCLI && salesforce.IsUnauthorized(err) {
		// Keep only the validated local instance URL; discard the stale bearer.
		instance := creds.Instance
		creds = auth.Credentials{}
		creds, err = fallback(ctx, alias)
		if err != nil || creds.Token == "" {
			return nil, fmt.Errorf("org token unavailable")
		}
		creds.Instance = instance
		return restOutputsWithOptions(ctx, names, snapshots, allChildren, creds, httpClient)
	}
	return files, err
}

// Collect in memory on each attempt; a failed or stale-token attempt is never published.
func restOutputs(ctx context.Context, names []string, snapshots bool, creds auth.Credentials, client *http.Client) ([]generator.File, error) {
	return restOutputsWithOptions(ctx, names, snapshots, false, creds, client)
}

func restOutputsWithOptions(ctx context.Context, names []string, snapshots, allChildren bool, creds auth.Credentials, client *http.Client) ([]generator.File, error) {
	// Scope is configured membership, independent of queue/discovery order.
	roots := make(map[string]bool, len(names))
	for _, name := range names {
		roots[name] = true
	}
	var files []generator.File
	err := salesforce.DescribeGraphLevels(ctx, names, creds, client, func(node salesforce.GraphNode, raw []byte) ([]salesforce.GraphNode, error) {
		includeChildren := roots[node.Name] || allChildren && node.Level < 5
		generated, targets, err := generator.GenerateWithRelationshipTargets(node.Name, raw, snapshots, includeChildren)
		if err != nil {
			return nil, err
		}
		files = append(files, generated...)
		var edges []salesforce.GraphNode
		for _, name := range targets.Parents {
			edges = append(edges, salesforce.GraphNode{Name: name, Level: node.Level})
		}
		for _, name := range targets.Children {
			edges = append(edges, salesforce.GraphNode{Name: name, Level: node.Level + 1})
		}
		return edges, nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
