package salesforce

import (
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

// DescribeGraph performs sequential breadth-first discovery. The visitor owns
// relationship validation and returns only parents that it can generate. Each
// attempt has its own queue/visited state and discovers the REST version once.
func DescribeGraph(ctx context.Context, names []string, creds auth.Credentials, httpClient *http.Client, visit func(string, []byte) ([]string, error)) error {
	queue := append([]string(nil), names...)
	for _, name := range queue {
		if !objectIdentifier.MatchString(name) {
			return fmt.Errorf("invalid object name")
		}
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
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
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
		parents, err := visit(name, wrapped)
		if err != nil {
			return err
		}
		// Validate the entire returned batch before requesting any target.
		for _, parent := range parents {
			if !objectIdentifier.MatchString(parent) {
				return fmt.Errorf("invalid object name")
			}
		}
		queue = append(queue, parents...)
	}
	return nil
}
