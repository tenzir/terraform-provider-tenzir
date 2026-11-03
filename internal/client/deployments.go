package client

import (
	"context"
	"encoding/json"
)

var (
	opListDeploymentTypes  = op("GET", "/organizations/{org}/workspaces/{workspace}/deployment-types")
	opListDeploymentImages = op("GET", "/deployment-images")
)

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
