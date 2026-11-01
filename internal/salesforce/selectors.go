package salesforce

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"sf-typings/internal/config"
	"sf-typings/internal/jsonutil"
)

func expandSelectors(ctx context.Context, client *http.Client, base *url.URL, version, token string, selectors []string) ([]string, error) {
	return expandApexSelectors(ctx, client, base, version, token, selectors, nil, nil)
}

func expandApexSelectors(ctx context.Context, client *http.Client, base *url.URL, version, token string, selectors, candidates []string, resolved map[string]string) ([]string, error) {
	selected := make(map[string]bool)
	wildcard := false
	for _, selector := range selectors {
		if strings.Contains(selector, "*") {
			wildcard = true
		} else {
			selected[selector] = true
		}
	}
	inferred := make(map[string]bool)
	if wildcard || len(candidates) > 0 {
		raw, err := restJSON(ctx, client, base, "/services/data/v"+version+"/sobjects/", token)
		if err != nil {
			return nil, fmt.Errorf("REST object catalog: %w", err)
		}
		var catalog struct {
			Objects []struct {
				Name string `json:"name"`
			} `json:"sobjects"`
		}
		if jsonutil.Decode(raw, &catalog, false) != nil || catalog.Objects == nil {
			return nil, fmt.Errorf("invalid REST object catalog")
		}
		for _, object := range catalog.Objects {
			if !objectIdentifier.MatchString(object.Name) {
				return nil, fmt.Errorf("invalid REST object catalog")
			}
			for _, candidate := range candidates {
				if strings.EqualFold(candidate, object.Name) {
					resolved[strings.ToLower(candidate)] = object.Name
					inferred[object.Name] = true
				}
			}
			if config.MatchesObject(selectors, object.Name) {
				selected[object.Name] = true
			}
		}
	}
	if len(selected) == 0 && len(selectors) > 0 {
		return nil, fmt.Errorf("object patterns matched no objects")
	}
	for name := range inferred {
		selected[name] = true
	}
	var names []string
	for name := range selected {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
