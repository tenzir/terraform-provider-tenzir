package client

import (
	"context"
	"encoding/json"
)

var (
	opReadAPIKey      = op("GET", "/api-keys/{apiKeyId}")
	opCreateAPIKey    = op("POST", "/users/{userId}/api-keys")
	opRevokeAPIKey    = op("POST", "/api-keys/{apiKeyId}/revoke")
	opSetAPIKeyPolicy = op("POST", "/api-keys/{apiKeyId}/set-policy")
	opRenameAPIKey    = op("POST", "/api-keys/{apiKeyId}/rename")
)

// APIKey is a key's metadata and stored policy; never its secret.
type APIKey struct {
	ID        string          `json:"id"`
	Owner     APIKeyOwner     `json:"owner"`
	Name      string          `json:"name"`
	Policy    json.RawMessage `json:"policy"`
	CreatedAt string          `json:"createdAt"`
	RevokedAt *string         `json:"revokedAt,omitempty"`
}

// APIKeyOwner is the user or organization that owns a key.
type APIKeyOwner struct {
	Tag    string `json:"_tag"`
	UserID string `json:"userId,omitempty"`
	OrgID  string `json:"orgId,omitempty"`
}

// NewAPIKey is a freshly minted key; Key is readable only in this response.
type NewAPIKey struct {
	APIKey APIKey `json:"apiKey"`
	Key    string `json:"key"`
}

// ReadAPIKey reads a key by id.
func (c *Client) ReadAPIKey(ctx context.Context, id string) (*APIKey, error) {
	var out APIKey
	if err := c.call(ctx, opReadAPIKey, []string{id}, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateAPIKey mints a key for a user.
func (c *Client) CreateAPIKey(ctx context.Context, userID, name string, policy json.RawMessage) (*NewAPIKey, error) {
	body := struct {
		Name   string          `json:"name"`
		Policy json.RawMessage `json:"policy"`
	}{name, policy}
	var out NewAPIKey
	if err := c.call(ctx, opCreateAPIKey, []string{userID}, nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RevokeAPIKey stops a key working; its record stays.
func (c *Client) RevokeAPIKey(ctx context.Context, id string) (*APIKey, error) {
	var out APIKey
	if err := c.call(ctx, opRevokeAPIKey, []string{id}, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetAPIKeyPolicy replaces a key's policy.
func (c *Client) SetAPIKeyPolicy(ctx context.Context, id string, policy json.RawMessage) (*APIKey, error) {
	body := struct {
		Policy json.RawMessage `json:"policy"`
	}{policy}
	var out APIKey
	if err := c.call(ctx, opSetAPIKeyPolicy, []string{id}, nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RenameAPIKey changes a key's display name.
func (c *Client) RenameAPIKey(ctx context.Context, id, name string) (*APIKey, error) {
	body := struct {
		Name string `json:"name"`
	}{name}
	var out APIKey
	if err := c.call(ctx, opRenameAPIKey, []string{id}, nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
