package client

import (
	"context"
	"encoding/json"
)

var opReadAccount = op("GET", "/account")

// Principal is who the platform takes the caller to be.
type Principal struct {
	Tag    string          `json:"_tag"`
	User   *User           `json:"user,omitempty"`
	Policy json.RawMessage `json:"policy"`
}

// User is a platform user.
type User struct {
	ID    string  `json:"id"`
	Email *string `json:"email,omitempty"`
	Name  *string `json:"name,omitempty"`
}

// ReadAccount tells who the configured credential authenticates.
func (c *Client) ReadAccount(ctx context.Context) (*Principal, error) {
	var p Principal
	if err := c.call(ctx, opReadAccount, nil, nil, nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
