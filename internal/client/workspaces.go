package client

import (
	"context"
	"encoding/json"
)

var (
	opListWorkspaces  = op("GET", "/organizations/{org}/workspaces")
	opReadWorkspace   = op("GET", "/organizations/{org}/workspaces/{workspace}")
	opCreateWorkspace = op("POST", "/organizations/{org}/workspaces")
	opUpdateWorkspace = op("POST", "/organizations/{org}/workspaces/{workspace}/update")
	opDeleteWorkspace = op("DELETE", "/organizations/{org}/workspaces/{workspace}")
)

// Workspace is a workspace of an organization.
type Workspace struct {
	ID        string  `json:"id"`
	Slug      string  `json:"slug"`
	Name      string  `json:"name"`
	IconURL   *string `json:"iconUrl,omitempty"`
	OrgID     string  `json:"orgId"`
	CreatedAt string  `json:"createdAt"`
}

// NewWorkspace is what creating a workspace takes. Without a name, the
// workspace is named after its slug.
type NewWorkspace struct {
	Slug    string  `json:"slug"`
	Name    *string `json:"name,omitempty"`
	IconURL *string `json:"iconUrl,omitempty"`
}

// WorkspacePatch changes the fields it sets. ClearIconURL removes the icon.
type WorkspacePatch struct {
	Slug         *string
	Name         *string
	IconURL      *string
	ClearIconURL bool
}

func (p WorkspacePatch) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	if p.Slug != nil {
		m["slug"] = *p.Slug
	}
	if p.Name != nil {
		m["name"] = *p.Name
	}
	if p.IconURL != nil {
		m["iconUrl"] = *p.IconURL
	} else if p.ClearIconURL {
		m["iconUrl"] = nil
	}
	return json.Marshal(m)
}

// ListWorkspaces lists the workspaces of an organization.
func (c *Client) ListWorkspaces(ctx context.Context, org string) ([]Workspace, error) {
	var out []Workspace
	if err := c.call(ctx, opListWorkspaces, []string{org}, nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ReadWorkspace reads a workspace by id or slug.
func (c *Client) ReadWorkspace(ctx context.Context, org, workspace string) (*Workspace, error) {
	var out Workspace
	if err := c.call(ctx, opReadWorkspace, []string{org, workspace}, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateWorkspace creates a workspace in an organization.
func (c *Client) CreateWorkspace(ctx context.Context, org string, ws NewWorkspace) (*Workspace, error) {
	var out Workspace
	if err := c.call(ctx, opCreateWorkspace, []string{org}, nil, ws, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateWorkspace changes a workspace.
func (c *Client) UpdateWorkspace(ctx context.Context, org, workspace string, patch WorkspacePatch) (*Workspace, error) {
	var out Workspace
	if err := c.call(ctx, opUpdateWorkspace, []string{org, workspace}, nil, patch, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteWorkspace deletes a workspace; the platform refuses while it holds
// deployments.
func (c *Client) DeleteWorkspace(ctx context.Context, org, workspace string) error {
	return c.call(ctx, opDeleteWorkspace, []string{org, workspace}, nil, nil, nil)
}
