package client

import (
	"context"
	"encoding/json"
)

var (
	opReadOrganization      = op("GET", "/organizations/{org}")
	opCreateOrganization    = op("POST", "/users/{userId}/organizations")
	opProvisionOrganization = op("POST", "/organizations")
	opUpdateOrganization    = op("POST", "/organizations/{org}/update")
	opDeleteOrganization    = op("DELETE", "/organizations/{org}")
	opListRoles             = op("GET", "/organizations/{org}/roles")
	opCreateRole            = op("POST", "/organizations/{org}/roles")
	opReadRole              = op("GET", "/organizations/{org}/roles/{roleId}")
	opUpdateRole            = op("POST", "/organizations/{org}/roles/{roleId}/update")
	opDeleteRole            = op("DELETE", "/organizations/{org}/roles/{roleId}")
)

// Organization is an organization with the number of its members.
type Organization struct {
	ID          string  `json:"id"`
	Slug        *string `json:"slug,omitempty"`
	Name        string  `json:"name"`
	IconURL     *string `json:"iconUrl,omitempty"`
	CreatedAt   string  `json:"createdAt"`
	MemberCount int64   `json:"memberCount"`
	Edition     string  `json:"edition"`
}

// NewOrganization is what creating an organization takes. Without a name,
// the organization is named after its slug.
type NewOrganization struct {
	Slug    string  `json:"slug"`
	Name    *string `json:"name,omitempty"`
	IconURL *string `json:"iconUrl,omitempty"`
}

// OrganizationPatch changes the fields it sets. ClearIconURL removes the icon.
type OrganizationPatch struct {
	Slug         *string
	Name         *string
	IconURL      *string
	ClearIconURL bool
}

func (p OrganizationPatch) MarshalJSON() ([]byte, error) {
	return json.Marshal(WorkspacePatch(p))
}

// RoleSummary is a role without its policy. Tag is Builtin or Custom.
type RoleSummary struct {
	Tag   string `json:"_tag"`
	ID    string `json:"id"`
	Label string `json:"label"`
	Color string `json:"color"`
}

// Role is a role with its policy, as the server normalized it.
type Role struct {
	RoleSummary
	Description *string         `json:"description,omitempty"`
	Policy      json.RawMessage `json:"policy"`
}

// NewRole is what creating a custom role takes.
type NewRole struct {
	Label       string          `json:"label"`
	Color       string          `json:"color"`
	Description *string         `json:"description,omitempty"`
	Policy      json.RawMessage `json:"policy"`
}

// RolePatch changes the fields it sets.
type RolePatch struct {
	Label       *string         `json:"label,omitempty"`
	Color       *string         `json:"color,omitempty"`
	Description *string         `json:"description,omitempty"`
	Policy      json.RawMessage `json:"policy,omitempty"`
}

// ReadOrganization reads an organization by id or slug.
func (c *Client) ReadOrganization(ctx context.Context, org string) (*Organization, error) {
	var out Organization
	if err := c.call(ctx, opReadOrganization, []string{org}, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateOrganization creates a Community organization with the given user as its sole owner.
func (c *Client) CreateOrganization(ctx context.Context, userID string, org NewOrganization) (*Organization, error) {
	var out Organization
	if err := c.call(ctx, opCreateOrganization, []string{userID}, nil, org, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ProvisionOrganization creates an organization on the provisioned plan and
// without members. Only platform administrators may.
func (c *Client) ProvisionOrganization(ctx context.Context, org NewOrganization) (*Organization, error) {
	var out Organization
	if err := c.call(ctx, opProvisionOrganization, nil, nil, org, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateOrganization changes an organization.
func (c *Client) UpdateOrganization(ctx context.Context, org string, patch OrganizationPatch) (*Organization, error) {
	var out Organization
	if err := c.call(ctx, opUpdateOrganization, []string{org}, nil, patch, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteOrganization deletes an empty organization.
func (c *Client) DeleteOrganization(ctx context.Context, org string) error {
	return c.call(ctx, opDeleteOrganization, []string{org}, nil, nil, nil)
}

// ListRoles lists the built-in and custom roles of an organization.
func (c *Client) ListRoles(ctx context.Context, org string) ([]RoleSummary, error) {
	var out []RoleSummary
	if err := c.call(ctx, opListRoles, []string{org}, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateRole creates a custom role.
func (c *Client) CreateRole(ctx context.Context, org string, role NewRole) (*Role, error) {
	var out Role
	if err := c.call(ctx, opCreateRole, []string{org}, nil, role, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReadRole reads a built-in or custom role.
func (c *Client) ReadRole(ctx context.Context, org, roleID string) (*Role, error) {
	var out Role
	if err := c.call(ctx, opReadRole, []string{org, roleID}, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateRole changes a custom role.
func (c *Client) UpdateRole(ctx context.Context, org, roleID string, patch RolePatch) (*Role, error) {
	var out Role
	if err := c.call(ctx, opUpdateRole, []string{org, roleID}, nil, patch, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteRole deletes a custom role and all its assignments.
func (c *Client) DeleteRole(ctx context.Context, org, roleID string) error {
	return c.call(ctx, opDeleteRole, []string{org, roleID}, nil, nil, nil)
}
