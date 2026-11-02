package salesforce

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Hacerx/sf-typings/internal/auth"
)

func TestDescribeGraphLevels(t *testing.T) {
	for _, bad := range []string{"", "name", "negative", "decreasing"} {
		t.Run(bad, func(t *testing.T) {
			var requests []string
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/services/data/" {
					io.WriteString(w, `[{"version":"60.0"}]`)
					return
				}
				name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/services/data/v60.0/sobjects/"), "/describe")
				requests = append(requests, name)
				io.WriteString(w, `{}`)
			}))
			defer server.Close()
			transport := server.Client().Transport
			client := &http.Client{Transport: depthTransport(func(r *http.Request) (*http.Response, error) {
				clone := r.Clone(r.Context())
				clone.URL.Host = strings.TrimPrefix(server.URL, "https://")
				return transport.RoundTrip(clone)
			})}
			levels := map[string]int{}
			err := DescribeGraphLevels(context.Background(), []string{"Root"}, auth.Credentials{Token: "test", Instance: "https://sample.my.salesforce.com"}, client, func(node GraphNode, raw []byte) ([]GraphNode, error) {
				levels[node.Name] = node.Level
				if node.Name == "Root" {
					batch := []GraphNode{{"Deep", 4}, {"Bridge", 1}}
					switch bad {
					case "name":
						batch = append(batch, GraphNode{"../bad", 1})
					case "negative":
						batch = append(batch, GraphNode{"Bad", -1})
					case "decreasing":
						batch = append(batch, GraphNode{"Bad", 0})
					}
					return batch, nil
				}
				if node.Name == "Bridge" {
					return []GraphNode{{"Deep", 1}}, nil
				}
				if node.Name == "Deep" {
					return []GraphNode{{"Root", 1}, {"Deep", 1}}, nil
				}
				return nil, nil
			})
			if bad != "" {
				if err == nil || !reflect.DeepEqual(requests, []string{"Root"}) {
					t.Fatalf("requests=%v err=%v", requests, err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(requests, []string{"Root", "Bridge", "Deep"}) || levels["Deep"] != 1 {
				t.Fatalf("requests=%v levels=%v err=%v", requests, levels, err)
			}
		})
	}
}

type depthTransport func(*http.Request) (*http.Response, error)

func (f depthTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
