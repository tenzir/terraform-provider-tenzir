package client

import "context"

// The internal endpoints that Tenzir runs its own installation with. Only
// platform administrators may call them.
var (
	opReadDemoOrgSetup   = op("GET", "/internal/onboarding/setup-demo-org")
	opSetDemoOrgSetup    = op("PUT", "/internal/onboarding/setup-demo-org")
	opRemoveDemoOrgSetup = op("DELETE", "/internal/onboarding/setup-demo-org")
)

// DemoOrgSetup names the organization every new user joins on signing up,
// and the custom role they get there.
type DemoOrgSetup struct {
	OrganizationID string `json:"organizationId"`
	RoleID         string `json:"roleId"`
}

// ReadDemoOrgSetup reads the demo organization, or nil if there is none.
func (c *Client) ReadDemoOrgSetup(ctx context.Context) (*DemoOrgSetup, error) {
	var out *DemoOrgSetup
	if err := c.call(ctx, opReadDemoOrgSetup, nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetDemoOrgSetup makes an organization the demo one, replacing any other.
func (c *Client) SetDemoOrgSetup(ctx context.Context, setup DemoOrgSetup) (*DemoOrgSetup, error) {
	var out DemoOrgSetup
	if err := c.call(ctx, opSetDemoOrgSetup, nil, nil, setup, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RemoveDemoOrgSetup makes new users join no organization.
func (c *Client) RemoveDemoOrgSetup(ctx context.Context) error {
	return c.call(ctx, opRemoveDemoOrgSetup, nil, nil, nil, nil)
}
