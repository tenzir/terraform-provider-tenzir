// Package client is a small hand-written client for the Tenzir Platform REST
// API, which the platform declares in platform/services/shared/api/groups.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Config selects the platform and the credential to call it with.
type Config struct {
	// URL is the platform address that serves /.well-known/tenzir-platform.
	URL string
	// APIKey is a tnzu_ key, sent as x-tenzir-api-key.
	APIKey string
	// Token is an OIDC bearer token, sent as Authorization: Bearer.
	Token string
	// TokenFile names a file that holds the bearer token. It is read again
	// for every request, so that whoever writes it can replace a token
	// before it expires.
	TokenFile string
	// UserAgent identifies the caller.
	UserAgent string
	// HTTPClient defaults to a client with a timeout.
	HTTPClient *http.Client
}

// Client calls the platform API at the address that discovery published.
type Client struct {
	apiURL    *url.URL
	platform  Platform
	apiKey    string
	token     string
	tokenFile string
	userAgent string
	http      *http.Client
}

// New discovers where the platform serves its API and returns a client for it.
func New(ctx context.Context, cfg Config) (*Client, error) {
	set := 0
	for _, credential := range []string{cfg.APIKey, cfg.Token, cfg.TokenFile} {
		if credential != "" {
			set++
		}
	}
	if set > 1 {
		return nil, fmt.Errorf("more than one of an API key, a token and a token file are set; the platform accepts only one")
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	c := &Client{
		apiKey:    cfg.APIKey,
		token:     cfg.Token,
		tokenFile: cfg.TokenFile,
		userAgent: cfg.UserAgent,
		http:      hc,
	}
	platform, err := c.discover(ctx, cfg.URL)
	if err != nil {
		return nil, err
	}
	apiURL, err := url.Parse(strings.TrimRight(platform.APIURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("the platform published an invalid API URL %q: %w", platform.APIURL, err)
	}
	c.apiURL = apiURL
	c.platform = *platform
	return c, nil
}

// Platform returns what discovery published.
func (c *Client) Platform() Platform {
	return c.platform
}

// APIURL returns the base URL that every operation path is relative to.
func (c *Client) APIURL() *url.URL {
	return c.apiURL
}

// Error is a failed API call. Tag is the Effect error `_tag`, such as
// NotFound, Unauthorized or SlugTaken, and is empty for a body that carries
// none.
type Error struct {
	Operation string
	Status    int
	Tag       string
	Message   string
	body      []byte
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d %s", e.Operation, e.Status, http.StatusText(e.Status))
	if e.Tag != "" {
		fmt.Fprintf(&b, " (%s)", e.Tag)
	}
	if e.Message != "" {
		fmt.Fprintf(&b, ": %s", e.Message)
	}
	return b.String()
}

// IsNotFound reports whether err is an API error with status 404.
func IsNotFound(err error) bool {
	return HasStatus(err, http.StatusNotFound)
}

// HasStatus reports whether err is an API error with the given status.
func HasStatus(err error, status int) bool {
	apiErr, ok := err.(*Error)
	return ok && apiErr.Status == status
}

// decodeError turns a non-2xx response into an *Error.
func decodeError(operation string, status int, body []byte) *Error {
	e := &Error{Operation: operation, Status: status, body: body}
	var tagged struct {
		Tag     string `json:"_tag"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &tagged) == nil {
		e.Tag = tagged.Tag
		e.Message = tagged.Message
	} else {
		e.Message = strings.TrimSpace(string(body))
		if len(e.Message) > 500 {
			e.Message = e.Message[:500] + "…"
		}
	}
	return e
}

// Operation is one endpoint of the API: a method and a path template whose
// {placeholders} are filled in order. Every operation the client uses is
// registered in Operations, which the contract test checks against the
// platform's OpenAPI document.
type Operation struct {
	Method string
	Path   string
}

// Operations lists every operation the client calls.
var Operations []Operation

func op(method, path string) Operation {
	o := Operation{Method: method, Path: path}
	Operations = append(Operations, o)
	return o
}

func (o Operation) String() string {
	return o.Method + " " + o.Path
}

// expand fills the placeholders of the path template with escaped arguments.
func (o Operation) expand(args ...string) string {
	var b strings.Builder
	rest := o.Path
	for _, arg := range args {
		start := strings.IndexByte(rest, '{')
		end := strings.IndexByte(rest, '}')
		if start < 0 || end < start {
			panic(fmt.Sprintf("%s: too many path arguments", o))
		}
		b.WriteString(rest[:start])
		b.WriteString(url.PathEscape(arg))
		rest = rest[end+1:]
	}
	if strings.IndexByte(rest, '{') >= 0 {
		panic(fmt.Sprintf("%s: too few path arguments", o))
	}
	b.WriteString(rest)
	return b.String()
}

// call sends one request to the API. A nil body sends none, and a nil out
// discards the response.
func (c *Client) call(ctx context.Context, o Operation, args []string, query url.Values, body, out any) error {
	target := c.apiURL.String() + o.expand(args...)
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	return c.send(ctx, o.String(), o.Method, target, body, out, true)
}

func (c *Client) send(ctx context.Context, operation, method, target string, body, out any, authenticate bool) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("%s: encoding the request: %w", operation, err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	if authenticate {
		switch {
		case c.apiKey != "":
			req.Header.Set("x-tenzir-api-key", c.apiKey)
		case c.token != "":
			req.Header.Set("Authorization", "Bearer "+c.token)
		case c.tokenFile != "":
			token, err := os.ReadFile(c.tokenFile)
			if err != nil {
				return fmt.Errorf("%s: reading the token file: %w", operation, err)
			}
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%s: reading the response: %w", operation, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return decodeError(operation, resp.StatusCode, payload)
	}
	if out == nil || len(payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("%s: decoding the response: %w", operation, err)
	}
	return nil
}
