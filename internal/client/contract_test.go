package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
)

var placeholder = regexp.MustCompile(`\{[^}]*\}`)

// TestContract checks every registered operation against OpenAPI from a
// local file or, during acceptance tests, a running platform.
func TestContract(t *testing.T) {
	var body io.ReadCloser
	prefix := "/api"
	if file := os.Getenv("TENZIR_OPENAPI_FILE"); file != "" {
		f, err := os.Open(file)
		if err != nil {
			t.Fatal(err)
		}
		body = f
	} else {
		url := os.Getenv("TENZIR_PLATFORM_URL")
		if os.Getenv("TF_ACC") == "" || url == "" {
			t.Skip("needs TENZIR_OPENAPI_FILE, or TF_ACC and TENZIR_PLATFORM_URL")
		}
		c, err := New(context.Background(), Config{URL: url, APIKey: "unused"})
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.Get(c.APIURL().String() + "/openapi.json")
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			t.Fatalf("OpenAPI request: %s", resp.Status)
		}
		body = resp.Body
		prefix = c.APIURL().Path
	}
	defer body.Close()
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.NewDecoder(body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	published := map[string]bool{}
	for p, methods := range doc.Paths {
		for method := range methods {
			published[strings.ToUpper(method)+" "+placeholder.ReplaceAllString(p, "{}")] = true
		}
	}
	for _, o := range Operations {
		key := o.Method + " " + placeholder.ReplaceAllString(prefix+o.Path, "{}")
		if !published[key] {
			t.Errorf("the platform does not publish %s", o)
		}
	}
	t.Logf("checked %d registered operations", len(Operations))
}
