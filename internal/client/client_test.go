package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// platform serves discovery at its root and routes /api to api.
func platform(t *testing.T, api http.HandlerFunc) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == WellKnownPath {
			if r.Header.Get("x-tenzir-api-key") != "" {
				t.Error("discovery sent the credential")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"apiUrl": srv.URL + "/api"})
			return
		}
		api(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestErrorDecoding(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		want    Error
		missing bool
	}{
		{"tag only", 404, `{"_tag":"NotFound"}`, Error{Status: 404, Tag: "NotFound"}, true},
		{"tag and message", 409, `{"_tag":"NotEmpty","message":"the workspace holds deployments"}`,
			Error{Status: 409, Tag: "NotEmpty", Message: "the workspace holds deployments"}, false},
		{"not json", 502, "Bad Gateway\n", Error{Status: 502, Message: "Bad Gateway"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := platform(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			c, err := New(context.Background(), Config{URL: srv.URL, APIKey: "tnzu_x"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.ReadWorkspace(context.Background(), "org", "ws")
			var apiErr *Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("got %v, want *Error", err)
			}
			if apiErr.Status != tc.want.Status || apiErr.Tag != tc.want.Tag || apiErr.Message != tc.want.Message {
				t.Errorf("got %+v, want %+v", *apiErr, tc.want)
			}
			if IsNotFound(err) != tc.missing {
				t.Errorf("IsNotFound = %v", !tc.missing)
			}
		})
	}
}

func TestRequestsCarryOneCredentialAndEscapedPaths(t *testing.T) {
	var gotPath, gotKey, gotAuth string
	srv := platform(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		gotKey = r.Header.Get("x-tenzir-api-key")
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{}`))
	})
	c, err := New(context.Background(), Config{URL: srv.URL + "/", Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadWorkspace(context.Background(), "my org", "a/b"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/organizations/my%20org/workspaces/a%2Fb" {
		t.Errorf("path %q", gotPath)
	}
	if gotKey != "" || gotAuth != "Bearer tok" {
		t.Errorf("credential headers %q, %q", gotKey, gotAuth)
	}
	if _, err := New(context.Background(), Config{URL: srv.URL, Token: "a", APIKey: "b"}); err == nil {
		t.Error("accepted both an API key and a token")
	}
}
