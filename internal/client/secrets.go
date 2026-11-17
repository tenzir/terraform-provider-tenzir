package client

import "context"

var (
	opReadOrganizationSecret   = op("GET", "/organizations/{org}/secrets/{name}")
	opCreateOrganizationSecret = op("POST", "/organizations/{org}/secrets")
	opSetOrganizationSecret    = op("POST", "/organizations/{org}/secrets/{name}/set")
	opDeleteOrganizationSecret = op("DELETE", "/organizations/{org}/secrets/{name}")

	opReadWorkspaceSecret   = op("GET", "/organizations/{org}/workspaces/{workspace}/secrets/{name}")
	opCreateWorkspaceSecret = op("POST", "/organizations/{org}/workspaces/{workspace}/secrets")
	opSetWorkspaceSecret    = op("POST", "/organizations/{org}/workspaces/{workspace}/secrets/{name}/set")
	opDeleteWorkspaceSecret = op("DELETE", "/organizations/{org}/workspaces/{workspace}/secrets/{name}")

	opReadDeploymentSecret   = op("GET", deploymentPath+"/secrets/{name}")
	opCreateDeploymentSecret = op("POST", deploymentPath+"/secrets")
	opSetDeploymentSecret    = op("POST", deploymentPath+"/secrets/{name}/set")
	opDeleteDeploymentSecret = op("DELETE", deploymentPath+"/secrets/{name}")
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

func (c *Client) secretOps(scope SecretScope) (read, create, set, del Operation) {
	switch scope.Kind() {
	case "Deployment":
		return opReadDeploymentSecret, opCreateDeploymentSecret, opSetDeploymentSecret, opDeleteDeploymentSecret
	case "Workspace":
		return opReadWorkspaceSecret, opCreateWorkspaceSecret, opSetWorkspaceSecret, opDeleteWorkspaceSecret
	default:
		return opReadOrganizationSecret, opCreateOrganizationSecret, opSetOrganizationSecret, opDeleteOrganizationSecret
	}
}

// ReadSecret reads one secret of a scope. For a deployment, an inherited
// secret of that name is not found.
func (c *Client) ReadSecret(ctx context.Context, scope SecretScope, name string) (*Secret, error) {
	read, _, _, _ := c.secretOps(scope)
	var out Secret
	if err := c.call(ctx, read, append(scope.args(), name), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateSecret creates a secret, failing if one of that name exists.
func (c *Client) CreateSecret(ctx context.Context, scope SecretScope, name string, write SecretWrite) (*Secret, error) {
	_, create, _, _ := c.secretOps(scope)
	body := struct {
		Name string `json:"name"`
		SecretWrite
	}{name, write}
	var out Secret
	if err := c.call(ctx, create, scope.args(), nil, body, &out); err != nil {
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
