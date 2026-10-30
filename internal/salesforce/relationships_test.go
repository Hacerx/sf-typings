package salesforce

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"sf-typings/internal/auth"
)

type graphTransport func(*http.Request) (*http.Response, error)

func (f graphTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDescribeGraphQueue(t *testing.T) {
	var paths, visits []string
	client := &http.Client{Transport: graphTransport(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		body := `{"name":"unused","fields":[]}`
		if r.URL.Path == "/services/data/" {
			body = `[{"version":"60.0"}]`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	graph := map[string][]string{"A": {"A", "B", "C", "B"}, "B": {"D"}, "C": {"D", "A"}, "D": {"B"}}
	err := DescribeGraph(context.Background(), []string{"A", "A"}, auth.Credentials{Instance: "https://sample.my.salesforce.com", Token: "fake"}, client, func(name string, raw []byte) ([]string, error) {
		visits = append(visits, name)
		return graph[name], nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(visits, []string{"A", "B", "C", "D"}) || len(paths) != 5 || paths[0] != "/services/data/" {
		t.Fatalf("visits=%v paths=%v", visits, paths)
	}
}

func TestDescribeGraphRejectsUnsafeBatch(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: graphTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{}`
		if calls == 1 {
			body = `[{"version":"60.0"}]`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	creds := auth.Credentials{Instance: "https://sample.my.salesforce.com", Token: "fake"}
	err := DescribeGraph(context.Background(), []string{"A"}, creds, client, func(string, []byte) ([]string, error) { return []string{"B", "../C"}, nil })
	if err == nil || calls != 2 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	calls = 0
	err = DescribeGraph(context.Background(), []string{"../A"}, creds, client, func(string, []byte) ([]string, error) { t.Fatal("unexpected visit"); return nil, nil })
	if err == nil || calls != 0 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
