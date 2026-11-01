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
	selected := make(map[string]bool)
	wildcard := false
	for _, selector := range selectors {
		if strings.Contains(selector, "*") {
			wildcard = true
		} else {
			selected[selector] = true
		}
	}
	if wildcard {
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
			if config.MatchesObject(selectors, object.Name) {
				selected[object.Name] = true
			}
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("object patterns matched no objects")
	}
	var names []string
	for name := range selected {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
