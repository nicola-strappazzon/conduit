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
	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
)

// Client holds an authenticated AWS config for one profile/region, scoped
// to opening and tearing down SSM port-forwarding sessions.
type Client struct {
	cfg     aws.Config
	ssm     ssmAPI
	Profile string
	Region  string
}

type ssmAPI interface {
	StartSession(context.Context, *ssm.StartSessionInput, ...func(*ssm.Options)) (*ssm.StartSessionOutput, error)
}

// NewClient loads AWS credentials for the given profile/region. It does not
// itself require a valid SSO session; credentials are resolved lazily on
// the first API call, so it's safe to construct before EnsureSSOLogin runs.
func NewClient(ctx context.Context, profile, region string) (*Client, error) {
	return newClient(ctx, profile, region, false)
}

// NewClientWithoutSSORefresh uses shared tokens but never renews the SSO session.
func NewClientWithoutSSORefresh(ctx context.Context, profile, region string) (*Client, error) {
	return newClient(ctx, profile, region, true)
}

func newClient(ctx context.Context, profile, region string, noRefresh bool) (*Client, error) {
	options := []func(*config.LoadOptions) error{
		config.WithSharedConfigProfile(profile),
		config.WithRegion(region),
	}
	if noRefresh {
		options = append(options, config.WithSSOTokenProviderOptions(func(o *ssocreds.SSOTokenProviderOptions) { o.Client = disabledSSORefresh{} }))
	}
	cfg, err := config.LoadDefaultConfig(ctx, options...)
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

// The SDK otherwise refreshes modern sso_session profiles independently of
// EnsureSSOLogin. Deny CreateToken here while retaining its cache reader.
type disabledSSORefresh struct{}

func (disabledSSORefresh) CreateToken(context.Context, *ssooidc.CreateTokenInput, ...func(*ssooidc.Options)) (*ssooidc.CreateTokenOutput, error) {
	return nil, fmt.Errorf("SSO token expired and --no-sso-refresh is enabled; renew the shared session using another Conduit instance or aws sso login")
}
