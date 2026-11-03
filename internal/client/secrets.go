package client

import (
	"context"
	"fmt"
)

var (
	opListOrganizationSecrets  = op("GET", "/organizations/{org}/secrets")
	opCreateOrganizationSecret = op("POST", "/organizations/{org}/secrets/{name}")
	opSetOrganizationSecret    = op("PUT", "/organizations/{org}/secrets/{name}")
	opDeleteOrganizationSecret = op("DELETE", "/organizations/{org}/secrets/{name}")

	opListWorkspaceSecrets  = op("GET", "/organizations/{org}/workspaces/{workspace}/secrets")
	opCreateWorkspaceSecret = op("POST", "/organizations/{org}/workspaces/{workspace}/secrets/{name}")
	opSetWorkspaceSecret    = op("PUT", "/organizations/{org}/workspaces/{workspace}/secrets/{name}")
	opDeleteWorkspaceSecret = op("DELETE", "/organizations/{org}/workspaces/{workspace}/secrets/{name}")

	opListDeploymentSecrets  = op("GET", "/organizations/{org}/workspaces/{workspace}/deployments/{deployment}/secrets")
	opCreateDeploymentSecret = op("POST", "/organizations/{org}/workspaces/{workspace}/deployments/{deployment}/secrets/{name}")
	opSetDeploymentSecret    = op("PUT", "/organizations/{org}/workspaces/{workspace}/deployments/{deployment}/secrets/{name}")
	opDeleteDeploymentSecret = op("DELETE", "/organizations/{org}/workspaces/{workspace}/deployments/{deployment}/secrets/{name}")
)

// SecretScope names the organization, workspace or deployment that holds a
// secret. The most specific one set is the owner.
type SecretScope struct {
	Organization string
	Workspace    string
	Deployment   string
}

// Kind is the owner tag that the platform reports for secrets of this scope.
func (s SecretScope) Kind() string {
	switch {
	case s.Deployment != "":
		return "Deployment"
	case s.Workspace != "":
		return "Workspace"
	default:
		return "Organization"
	}
}

func (s SecretScope) args() []string {
	switch s.Kind() {
	case "Deployment":
		return []string{s.Organization, s.Workspace, s.Deployment}
	case "Workspace":
		return []string{s.Organization, s.Workspace}
	default:
		return []string{s.Organization}
	}
}

// Secret is what the platform tells about a secret; never its value.
type Secret struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Owner      SecretOwner `json:"owner"`
	ReadableBy ReadableBy  `json:"readableBy"`
	Version    int64       `json:"version"`
	UpdatedBy  string      `json:"updatedBy"`
	CreatedAt  string      `json:"createdAt"`
	UpdatedAt  string      `json:"updatedAt"`
}

// SecretOwner is the organization, workspace or deployment holding a secret.
type SecretOwner struct {
	Tag          string `json:"_tag"`
	OrgID        string `json:"orgId"`
	WorkspaceID  string `json:"workspaceId,omitempty"`
	DeploymentID string `json:"deploymentId,omitempty"`
}

// SecretWrite sets a value, its readers, or both. A deployment secret takes
// no readers.
type SecretWrite struct {
	Value      *string     `json:"value,omitempty"`
	ReadableBy *ReadableBy `json:"readableBy,omitempty"`
}

func (c *Client) secretOps(scope SecretScope) (list, create, set, del Operation) {
	switch scope.Kind() {
	case "Deployment":
		return opListDeploymentSecrets, opCreateDeploymentSecret, opSetDeploymentSecret, opDeleteDeploymentSecret
	case "Workspace":
		return opListWorkspaceSecrets, opCreateWorkspaceSecret, opSetWorkspaceSecret, opDeleteWorkspaceSecret
	default:
		return opListOrganizationSecrets, opCreateOrganizationSecret, opSetOrganizationSecret, opDeleteOrganizationSecret
	}
}

// ListSecrets lists the secrets a scope holds. For a deployment, the platform
// also lists what it inherits; this keeps only the deployment's own.
func (c *Client) ListSecrets(ctx context.Context, scope SecretScope) ([]Secret, error) {
	list, _, _, _ := c.secretOps(scope)
	var all []Secret
	if err := c.call(ctx, list, scope.args(), nil, nil, &all); err != nil {
		return nil, err
	}
	own := all[:0]
	for _, s := range all {
		if s.Owner.Tag == scope.Kind() {
			own = append(own, s)
		}
	}
	return own, nil
}

// ReadSecret reads one secret of a scope, as a 404 *Error when it is absent.
//
// The platform has no single-secret read yet, so this filters the listing.
func (c *Client) ReadSecret(ctx context.Context, scope SecretScope, name string) (*Secret, error) {
	secrets, err := c.ListSecrets(ctx, scope)
	if err != nil {
		return nil, err
	}
	for i := range secrets {
		if secrets[i].Name == name {
			return &secrets[i], nil
		}
	}
	list, _, _, _ := c.secretOps(scope)
	return nil, &Error{
		Operation: list.String(),
		Status:    404,
		Tag:       "NotFound",
		Message:   fmt.Sprintf("no secret named %q", name),
	}
}

// CreateSecret creates a secret, failing if one of that name exists.
func (c *Client) CreateSecret(ctx context.Context, scope SecretScope, name string, write SecretWrite) (*Secret, error) {
	_, create, _, _ := c.secretOps(scope)
	var out Secret
	if err := c.call(ctx, create, append(scope.args(), name), nil, write, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetSecret creates a secret or changes its value or readers.
func (c *Client) SetSecret(ctx context.Context, scope SecretScope, name string, write SecretWrite) (*Secret, error) {
	_, _, set, _ := c.secretOps(scope)
	var out Secret
	if err := c.call(ctx, set, append(scope.args(), name), nil, write, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSecret deletes a secret.
func (c *Client) DeleteSecret(ctx context.Context, scope SecretScope, name string) error {
	_, _, _, del := c.secretOps(scope)
	return c.call(ctx, del, append(scope.args(), name), nil, nil, nil)
}
