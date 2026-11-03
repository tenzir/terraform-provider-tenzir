package client

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
)

var placeholder = regexp.MustCompile(`\{[^}]*\}`)

// TestContract checks every operation the client calls against the OpenAPI
// document of a running platform, to catch renames on the platform side.
func TestContract(t *testing.T) {
	url := os.Getenv("TENZIR_PLATFORM_URL")
	if os.Getenv("TF_ACC") == "" || url == "" {
		t.Skip("needs TF_ACC and TENZIR_PLATFORM_URL")
	}
	c, err := New(context.Background(), Config{URL: url, APIKey: "unused"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(c.APIURL().String() + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	published := map[string]bool{}
	for p, methods := range doc.Paths {
		for method := range methods {
			published[strings.ToUpper(method)+" "+placeholder.ReplaceAllString(p, "{}")] = true
		}
	}
	prefix := c.APIURL().Path
	for _, o := range Operations {
		key := o.Method + " " + placeholder.ReplaceAllString(prefix+o.Path, "{}")
		if !published[key] {
			t.Errorf("the platform does not publish %s", o)
		}
	}
}
