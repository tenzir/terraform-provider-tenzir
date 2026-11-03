package client

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

var (
	opListDeploymentTypes  = op("GET", "/organizations/{org}/workspaces/{workspace}/deployment-types")
	opListDeploymentImages = op("GET", "/deployment-images")

	opCreateDeployment        = op("POST", "/organizations/{org}/workspaces/{workspace}/deployments")
	opCreateHostedDeployment  = op("POST", "/organizations/{org}/workspaces/{workspace}/hosted-deployments")
	opReadDeploymentDetails   = op("GET", "/deployments/{deploymentId}/details")
	opUpdateDeployment        = op("POST", "/deployments/{deploymentId}/update")
	opChangeDeploymentTracker = op("PUT", "/deployments/{deploymentId}/tracker")
	opChangeDeploymentType    = op("PUT", "/deployments/{deploymentId}/type")
	opRotateDeploymentKey     = op("POST", "/deployments/{deploymentId}/rotate-key")
	opDeleteDeployment        = op("DELETE", "/deployments/{deploymentId}")
)

// Deployment is what the platform records about a deployment.
type Deployment struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspaceId"`
	Name        string  `json:"name"`
	Version     *string `json:"version,omitempty"`
	LastSeen    *string `json:"lastSeen,omitempty"`
}

// Tracker is the branch a deployment follows, and whether it deploys new
// commits on its own.
type Tracker struct {
	Branch     string `json:"branch"`
	AutoDeploy bool   `json:"autoDeploy"`
}

// DeploymentDetails is a deployment with who hosts it, whether it is
// connected, and its tracker.
type DeploymentDetails struct {
	Deployment Deployment `json:"deployment"`
	Reachable  bool       `json:"reachable"`
	// Hosting is "tenzir" or "customer".
	Hosting   string             `json:"hosting"`
	Size      *string            `json:"size,omitempty"`
	Region    *string            `json:"region,omitempty"`
	Source    *string            `json:"source,omitempty"`
	Runtime   *string            `json:"runtime,omitempty"`
	Upgrade   *DeploymentUpgrade `json:"upgrade,omitempty"`
	CreatedAt string             `json:"createdAt"`
	Tracker   Tracker            `json:"tracker"`
}

// DeploymentUpgrade says whether a deployment runs the platform's version:
// Tag is "Current" or "Available", which carries the image to upgrade to.
type DeploymentUpgrade struct {
	Tag   string `json:"_tag"`
	Image string `json:"image,omitempty"`
}

// NewDeployment is what creating a self-hosted deployment takes. Without a
// branch it follows main, and without autoDeploy it deploys automatically.
type NewDeployment struct {
	Name       string  `json:"name"`
	Branch     *string `json:"branch,omitempty"`
	AutoDeploy *bool   `json:"autoDeploy,omitempty"`
}

// NewHostedDeployment is what creating a deployment that Tenzir runs takes.
// The platform picks the image, size and region it is not given.
type NewHostedDeployment struct {
	NewDeployment
	Image  *string `json:"image,omitempty"`
	Size   *string `json:"size,omitempty"`
	Region *string `json:"region,omitempty"`
}

// DeploymentPatch changes the fields it sets.
type DeploymentPatch struct {
	Name *string `json:"name,omitempty"`
}

// DeploymentOffer says where and how large the platform can run a deployment
// for a workspace.
type DeploymentOffer struct {
	Edition      string           `json:"edition"`
	Availability string           `json:"availability"`
	Regions      []string         `json:"regions"`
	Types        []DeploymentType `json:"types"`
	DefaultSize  string           `json:"defaultSize"`
}

// DeploymentType is a size and what a running hour of it costs. The rate is
// a fixed-point number, kept as the platform encodes it; null means unpriced.
type DeploymentType struct {
	Size           string          `json:"size"`
	CreditsPerHour json.RawMessage `json:"creditsPerHour"`
}

// DeploymentImages lists the images that match the platform.
type DeploymentImages struct {
	Default string            `json:"default"`
	Images  []DeploymentImage `json:"images"`
}

// DeploymentImage is one deployment image.
type DeploymentImage struct {
	Tag       string `json:"tag"`
	Title     string `json:"title"`
	Reference string `json:"reference"`
}

// ListDeploymentTypes reads what deployments the platform can run in a
// workspace.
func (c *Client) ListDeploymentTypes(ctx context.Context, org, workspace string) (*DeploymentOffer, error) {
	var out DeploymentOffer
	if err := c.call(ctx, opListDeploymentTypes, []string{org, workspace}, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListDeploymentImages lists the images that match the platform.
func (c *Client) ListDeploymentImages(ctx context.Context) (*DeploymentImages, error) {
	var out DeploymentImages
	if err := c.call(ctx, opListDeploymentImages, nil, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateDeployment registers a self-hosted deployment, which has no key yet.
func (c *Client) CreateDeployment(ctx context.Context, org, workspace string, d NewDeployment) (*Deployment, error) {
	var out Deployment
	if err := c.call(ctx, opCreateDeployment, []string{org, workspace}, nil, d, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateHostedDeployment registers a deployment that Tenzir runs, and starts
// it. It connects some time after this returns.
func (c *Client) CreateHostedDeployment(ctx context.Context, org, workspace string, d NewHostedDeployment) (*Deployment, error) {
	var out Deployment
	if err := c.call(ctx, opCreateHostedDeployment, []string{org, workspace}, nil, d, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReadDeploymentDetails reads a deployment with its hosting, connection and
// tracker.
func (c *Client) ReadDeploymentDetails(ctx context.Context, id string) (*DeploymentDetails, error) {
	var out DeploymentDetails
	if err := c.call(ctx, opReadDeploymentDetails, []string{id}, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateDeployment renames a deployment.
func (c *Client) UpdateDeployment(ctx context.Context, id string, patch DeploymentPatch) (*Deployment, error) {
	var out Deployment
	if err := c.call(ctx, opUpdateDeployment, []string{id}, nil, patch, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ChangeDeploymentTracker sets the branch a deployment follows and whether it
// deploys new commits on its own. The platform refuses a branch without
// commits.
func (c *Client) ChangeDeploymentTracker(ctx context.Context, id string, t Tracker) (*Tracker, error) {
	var out Tracker
	if err := c.call(ctx, opChangeDeploymentTracker, []string{id}, nil, t, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ChangeDeploymentType runs a hosted deployment as another size, which
// restarts it.
func (c *Client) ChangeDeploymentType(ctx context.Context, id, size string) error {
	body := struct {
		Size string `json:"size"`
	}{size}
	return c.call(ctx, opChangeDeploymentType, []string{id}, nil, body, nil)
}

// RotateDeploymentKey mints a new key for a self-hosted deployment, which
// stops the previous one working, and returns its secret.
func (c *Client) RotateDeploymentKey(ctx context.Context, id string) (string, error) {
	var out struct {
		Secret string `json:"secret"`
	}
	if err := c.call(ctx, opRotateDeploymentKey, []string{id}, nil, nil, &out); err != nil {
		return "", err
	}
	return out.Secret, nil
}

// deleteRetries bounds how often DeleteDeployment retries a 503, which the
// platform answers while Kafka is unavailable to detach the deployment from
// its ingest endpoints.
var deleteRetries = []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second}

// DeleteDeployment deletes a deployment with its metrics, after handing a
// connected one an empty configuration.
func (c *Client) DeleteDeployment(ctx context.Context, id string) error {
	for _, wait := range deleteRetries {
		err := c.call(ctx, opDeleteDeployment, []string{id}, nil, nil, nil)
		if !HasStatus(err, http.StatusServiceUnavailable) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(wait):
		}
	}
	return c.call(ctx, opDeleteDeployment, []string{id}, nil, nil, nil)
}
