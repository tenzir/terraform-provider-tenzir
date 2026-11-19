package client

import "context"

var (
	opListMembers    = op("GET", "/organizations/{org}/members")
	opAddMember      = op("POST", "/organizations/{org}/members")
	opRemoveMember   = op("DELETE", "/organizations/{org}/members/{memberId}")
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

// NewMember is what adding an existing user to an organization takes.
type NewMember struct {
	UserID  string   `json:"userId"`
	RoleIDs []string `json:"roleIds"`
}

// ListMembers lists the members of an organization, invitations included.
func (c *Client) ListMembers(ctx context.Context, org string) ([]Member, error) {
	var out []Member
	if err := c.call(ctx, opListMembers, []string{org}, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AddMember makes an existing user a member without an invitation. Only
// platform administrators may.
func (c *Client) AddMember(ctx context.Context, org string, member NewMember) (*Member, error) {
	if member.RoleIDs == nil {
		member.RoleIDs = []string{}
	}
	var out Member
	if err := c.call(ctx, opAddMember, []string{org}, nil, member, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RemoveMember removes a member and its role assignments.
func (c *Client) RemoveMember(ctx context.Context, org, memberID string) error {
	return c.call(ctx, opRemoveMember, []string{org, memberID}, nil, nil, nil)
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
