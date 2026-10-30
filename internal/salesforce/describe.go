package salesforce

import (
	"container/heap"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"sf-typings/internal/auth"
	"sf-typings/internal/jsonutil"
)

var restVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)

// TrustedInstance accepts only an HTTPS Salesforce origin, never arbitrary URLs.
func TrustedInstance(instance string) (*url.URL, bool) {
	u, err := url.Parse(instance)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.HasSuffix(host, ".") {
		return nil, false
	}
	trusted := false
	for _, suffix := range []string{".my.salesforce.com", ".salesforce.com", ".force.com"} {
		if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
			trusted = true
		}
	}
	if !trusted {
		return nil, false
	}
	return u, true
}

type restHTTPError struct{ status int }

func (e *restHTTPError) Error() string { return fmt.Sprintf("REST HTTP %d", e.status) }

func restJSON(ctx context.Context, client *http.Client, base *url.URL, path, token string) ([]byte, error) {
	u := *base
	u.Path = path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("invalid REST request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("REST network request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &restHTTPError{status: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return nil, fmt.Errorf("invalid REST response")
	}
	return data, nil
}

// IsUnauthorized identifies a REST 401 without exposing response bodies.
func IsUnauthorized(err error) bool {
	var response *restHTTPError
	return errors.As(err, &response) && response.status == http.StatusUnauthorized
}

// Describe visits each complete Describe response in request order. The caller
// owns generation and publication; callback failures stop further requests.
func Describe(ctx context.Context, names []string, creds auth.Credentials, httpClient *http.Client, visit func(string, []byte) error) error {
	return DescribeGraph(ctx, names, creds, httpClient, func(name string, raw []byte) ([]string, error) {
		return nil, visit(name, raw)
	})
}

var objectIdentifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// DescribeGraph retains breadth-first request order for unweighted edges.
func DescribeGraph(ctx context.Context, names []string, creds auth.Credentials, httpClient *http.Client, visit func(string, []byte) ([]string, error)) error {
	return DescribeGraphLevels(ctx, names, creds, httpClient, func(node GraphNode, raw []byte) ([]GraphNode, error) {
		targets, err := visit(node.Name, raw)
		var edges []GraphNode
		for _, name := range targets {
			edges = append(edges, GraphNode{name, node.Level + 1})
		}
		return edges, err
	})
}

// GraphNode carries an absolute discovery level. Roots always start at one.
// Visitors may return same-level or higher-level edges, never decreasing ones.
type GraphNode struct {
	Name  string
	Level int
}

type graphEntry struct {
	GraphNode
	order int
}
type graphQueue []graphEntry

func (q graphQueue) Len() int { return len(q) }
func (q graphQueue) Less(i, j int) bool {
	return q[i].Level < q[j].Level || q[i].Level == q[j].Level && q[i].order < q[j].order
}
func (q graphQueue) Swap(i, j int)   { q[i], q[j] = q[j], q[i] }
func (q *graphQueue) Push(value any) { *q = append(*q, value.(graphEntry)) }
func (q *graphQueue) Pop() any {
	last := len(*q) - 1
	value := (*q)[last]
	*q = (*q)[:last]
	return value
}

// DescribeGraphLevels visits each object once, at its lowest reachable level.
// Stable lowest-level-first scheduling finalizes only after all possible shorter
// paths, including zero-cost hops. All traversal state is local to this attempt.
// The caller owns edge semantics; transport validates each complete next batch.
func DescribeGraphLevels(ctx context.Context, names []string, creds auth.Credentials, httpClient *http.Client, visit func(GraphNode, []byte) ([]GraphNode, error)) error {
	queue := &graphQueue{}
	best := make(map[string]int)
	order := 0
	enqueue := func(node GraphNode) {
		if level, exists := best[node.Name]; exists && level <= node.Level {
			return
		}
		best[node.Name] = node.Level
		heap.Push(queue, graphEntry{node, order})
		order++
	}
	for _, name := range names {
		if !objectIdentifier.MatchString(name) {
			return fmt.Errorf("invalid object name")
		}
		enqueue(GraphNode{name, 1})
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	client := *httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	base, ok := TrustedInstance(creds.Instance)
	if !ok {
		return fmt.Errorf("untrusted org instance")
	}
	versionsRaw, err := restJSON(ctx, &client, base, "/services/data/", creds.Token)
	if err != nil {
		return fmt.Errorf("REST version lookup: %w", err)
	}
	var versions []struct {
		Version string `json:"version"`
	}
	if jsonutil.Decode(versionsRaw, &versions, false) != nil {
		return fmt.Errorf("invalid REST versions")
	}
	version, major, minor := "", "", ""
	for _, item := range versions {
		if !restVersion.MatchString(item.Version) {
			continue
		}
		parts := strings.SplitN(item.Version, ".", 2)
		for i := range parts {
			parts[i] = strings.TrimLeft(parts[i], "0")
			if parts[i] == "" {
				parts[i] = "0"
			}
		}
		// Compare decimal integers without overflow or floating-point rounding.
		greater := version == "" || len(parts[0]) > len(major) || (len(parts[0]) == len(major) && parts[0] > major) ||
			(parts[0] == major && (len(parts[1]) > len(minor) || (len(parts[1]) == len(minor) && parts[1] > minor)))
		if greater {
			version, major, minor = item.Version, parts[0], parts[1]
		}
	}
	if version == "" {
		return fmt.Errorf("invalid REST versions")
	}
	seen := make(map[string]bool)
	for queue.Len() > 0 {
		node := heap.Pop(queue).(graphEntry).GraphNode
		name := node.Name
		if seen[name] || best[name] != node.Level {
			continue
		}
		seen[name] = true
		raw, err := restJSON(ctx, &client, base, "/services/data/v"+version+"/sobjects/"+name+"/describe", creds.Token)
		if err != nil {
			return fmt.Errorf("REST Describe for %s: %w", name, err)
		}
		wrapped, err := json.Marshal(struct {
			Status int             `json:"status"`
			Result json.RawMessage `json:"result"`
		}{0, raw})
		if err != nil {
			return fmt.Errorf("invalid REST response")
		}
		edges, err := visit(node, wrapped)
		if err != nil {
			return err
		}
		// Validate the entire returned batch before requesting any target.
		for _, edge := range edges {
			if !objectIdentifier.MatchString(edge.Name) {
				return fmt.Errorf("invalid object name")
			}
			if edge.Level < node.Level {
				return fmt.Errorf("invalid graph level")
			}
		}
		for _, edge := range edges {
			enqueue(edge)
		}
	}
	return nil
}
