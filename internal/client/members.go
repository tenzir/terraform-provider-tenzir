package client

import "context"

var (
	opListMembers    = op("GET", "/organizations/{org}/members")
	opAssignRoleTo   = op("POST", "/organizations/{org}/members/{memberId}/roles")
	opRevokeRoleFrom = op("DELETE", "/organizations/{org}/members/{memberId}/roles/{roleId}")
)

// UserSummary is a user without identity-provider details.
type UserSummary struct {
	ID     string  `json:"id"`
	Name   *string `json:"name,omitempty"`
	Email  *string `json:"email,omitempty"`
	Tombed bool    `json:"tombed"`
}

// Invitation is an invitation to an organization, without its secret.
type Invitation struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	CreatedBy string `json:"createdBy"`
}

// Member is a membership of an organization: a user, a pending invitation,
// or both, with the roles assigned to it.
type Member struct {
	ID            string        `json:"id"`
	ProvisionedBy string        `json:"provisionedBy"`
	User          *UserSummary  `json:"user,omitempty"`
	Invitation    *Invitation   `json:"invitation,omitempty"`
	Roles         []RoleSummary `json:"roles"`
}

// ListMembers lists the members of an organization, invitations included.
func (c *Client) ListMembers(ctx context.Context, org string) ([]Member, error) {
	var out []Member
	if err := c.call(ctx, opListMembers, []string{org}, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AssignRoleTo assigns a role to a member; an existing assignment stays.
func (c *Client) AssignRoleTo(ctx context.Context, org, memberID, roleID string) error {
	body := map[string]string{"roleId": roleID}
	return c.call(ctx, opAssignRoleTo, []string{org, memberID}, nil, body, nil)
}

// RevokeRoleFrom revokes a role from a member; an absent one stays absent.
func (c *Client) RevokeRoleFrom(ctx context.Context, org, memberID, roleID string) error {
	return c.call(ctx, opRevokeRoleFrom, []string{org, memberID, roleID}, nil, nil, nil)
}
