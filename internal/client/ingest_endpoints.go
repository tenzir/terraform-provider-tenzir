package client

import "context"

var (
	opCreateIngestEndpoint = op("POST", "/organizations/{org}/ingest-endpoints")
	opReadIngestEndpoint   = op("GET", "/organizations/{org}/ingest-endpoints/{endpointId}")
	opReadIngestToken      = op("GET", "/organizations/{org}/ingest-endpoints/{endpointId}/token")
	opUpdateIngestEndpoint = op("POST", "/organizations/{org}/ingest-endpoints/{endpointId}/update")
	opDeleteIngestEndpoint = op("DELETE", "/organizations/{org}/ingest-endpoints/{endpointId}")
)

// IngestEndpoint is a URL that turns every request into one event.
type IngestEndpoint struct {
	ID            string     `json:"id"`
	OrgID         string     `json:"orgId"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	ReadableBy    ReadableBy `json:"readableBy"`
	URL           *string    `json:"url,omitempty"`
	TokenPrefix   string     `json:"tokenPrefix"`
	TokenIssuedAt string     `json:"tokenIssuedAt"`
	CreatedAt     string     `json:"createdAt"`
	CreatedBy     *string    `json:"createdBy,omitempty"`
}

// NewIngestEndpoint is what creating an endpoint takes. Without readers,
// every deployment of the organization may read it.
type NewIngestEndpoint struct {
	Name        string      `json:"name"`
	Description *string     `json:"description,omitempty"`
	ReadableBy  *ReadableBy `json:"readableBy,omitempty"`
}

// IngestEndpointPatch changes the fields it sets.
type IngestEndpointPatch struct {
	Description *string     `json:"description,omitempty"`
	ReadableBy  *ReadableBy `json:"readableBy,omitempty"`
}

// CreateIngestEndpoint creates an endpoint and returns it with its token.
func (c *Client) CreateIngestEndpoint(ctx context.Context, org string, ep NewIngestEndpoint) (*IngestEndpoint, string, error) {
	var out struct {
		Endpoint IngestEndpoint `json:"endpoint"`
		Token    string         `json:"token"`
	}
	if err := c.call(ctx, opCreateIngestEndpoint, []string{org}, nil, ep, &out); err != nil {
		return nil, "", err
	}
	return &out.Endpoint, out.Token, nil
}

// ReadIngestEndpoint reads an endpoint by id.
func (c *Client) ReadIngestEndpoint(ctx context.Context, org, id string) (*IngestEndpoint, error) {
	var out IngestEndpoint
	if err := c.call(ctx, opReadIngestEndpoint, []string{org, id}, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReadIngestToken reads an endpoint's current token.
func (c *Client) ReadIngestToken(ctx context.Context, org, id string) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	if err := c.call(ctx, opReadIngestToken, []string{org, id}, nil, nil, &out); err != nil {
		return "", err
	}
	return out.Token, nil
}

// UpdateIngestEndpoint changes an endpoint's description or readers.
func (c *Client) UpdateIngestEndpoint(ctx context.Context, org, id string, patch IngestEndpointPatch) (*IngestEndpoint, error) {
	var out IngestEndpoint
	if err := c.call(ctx, opUpdateIngestEndpoint, []string{org, id}, nil, patch, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteIngestEndpoint deletes an endpoint with its topic and buffered events.
func (c *Client) DeleteIngestEndpoint(ctx context.Context, org, id string) error {
	return c.call(ctx, opDeleteIngestEndpoint, []string{org, id}, nil, nil, nil)
}
