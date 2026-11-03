package client

import (
	"context"
	"fmt"
	"strings"
)

// WellKnownPath is where a platform publishes its configuration, relative to
// the platform address rather than to the API.
const WellKnownPath = "/.well-known/tenzir-platform"

// Platform is what a platform publishes about itself.
type Platform struct {
	APIURL            string  `json:"apiUrl"`
	GatewayURL        string  `json:"gatewayUrl"`
	IngestURL         *string `json:"ingestUrl,omitempty"`
	HostedDeployments bool    `json:"hostedDeployments"`
}

func (c *Client) discover(ctx context.Context, platformURL string) (*Platform, error) {
	target := strings.TrimRight(platformURL, "/") + WellKnownPath
	var platform Platform
	if err := c.send(ctx, "GET "+WellKnownPath, "GET", target, nil, &platform, false); err != nil {
		return nil, fmt.Errorf("discovering the platform at %s: %w", platformURL, err)
	}
	if platform.APIURL == "" {
		return nil, fmt.Errorf("the platform at %s publishes no API URL", platformURL)
	}
	return &platform, nil
}
