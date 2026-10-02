// Package aws wraps every AWS-specific concern of Conduit: loading
// credentials, starting/terminating SSM sessions, and performing SSO
// device-authorization login. It knows nothing about CLI flags, process
// management, or how a login URL gets opened — callers inject an openURL
// callback for that — so it can be reused or tested independently of the
// rest of the program.
package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// Client holds an authenticated AWS config for one profile/region, scoped
// to opening and tearing down SSM port-forwarding sessions.
type Client struct {
	cfg     aws.Config
	ssm     *ssm.Client
	Profile string
	Region  string
}

// NewClient loads AWS credentials for the given profile/region. It does not
// itself require a valid SSO session; credentials are resolved lazily on
// the first API call, so it's safe to construct before EnsureSSOLogin runs.
func NewClient(ctx context.Context, profile, region string) (*Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithSharedConfigProfile(profile),
		config.WithRegion(region),
	)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config for profile %q: %w", profile, err)
	}
	return &Client{
		cfg:     cfg,
		ssm:     ssm.NewFromConfig(cfg),
		Profile: profile,
		Region:  region,
	}, nil
}

// Endpoint returns the regional SSM service endpoint.
func (c *Client) Endpoint() string {
	return fmt.Sprintf("https://ssm.%s.amazonaws.com", c.Region)
}
