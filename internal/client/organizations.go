package client

import (
	"context"
	"encoding/json"
)

var (
	opReadOrganization = op("GET", "/organizations/{org}")
	opListPeople       = op("GET", "/organizations/{org}/people")
	opListRoles        = op("GET", "/organizations/{org}/roles")
	opCreateRole       = op("POST", "/organizations/{org}/roles")
	opReadRole         = op("GET", "/organizations/{org}/roles/{roleId}")
	opUpdateRole       = op("POST", "/organizations/{org}/roles/{roleId}/update")
	opDeleteRole       = op("DELETE", "/organizations/{org}/roles/{roleId}")
	opAssignMemberRole = op("PUT", "/organizations/{org}/people/{userId}/roles/{roleId}")
	opRevokeMemberRole = op("DELETE", "/organizations/{org}/people/{userId}/roles/{roleId}")
)

// Organization is an organization with the number of its members.
type Organization struct {
	ID          string  `json:"id"`
	Slug        *string `json:"slug,omitempty"`
	Name        string  `json:"name"`
	IconURL     *string `json:"iconUrl,omitempty"`
	CreatedBy   *string `json:"createdBy,omitempty"`
	CreatedAt   string  `json:"createdAt"`
	MemberCount int64   `json:"memberCount"`
}

// Person is an organization member and the roles assigned to them.
type Person struct {
	UserID string        `json:"userId"`
	Name   *string       `json:"name,omitempty"`
	Email  *string       `json:"email,omitempty"`
	Roles  []RoleSummary `json:"roles"`
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

// ListPeople lists the members of an organization.
func (c *Client) ListPeople(ctx context.Context, org string) ([]Person, error) {
	var out []Person
	if err := c.call(ctx, opListPeople, []string{org}, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
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

// AssignMemberRole assigns a role to a member; an existing assignment stays.
func (c *Client) AssignMemberRole(ctx context.Context, org, userID, roleID string) error {
	return c.call(ctx, opAssignMemberRole, []string{org, userID, roleID}, nil, nil, nil)
}

// RevokeMemberRole revokes a role from a member; an absent one stays absent.
func (c *Client) RevokeMemberRole(ctx context.Context, org, userID, roleID string) error {
	return c.call(ctx, opRevokeMemberRole, []string{org, userID, roleID}, nil, nil, nil)
}
