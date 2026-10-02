package aws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// EnsureSSOLogin makes sure the given profile has a valid IAM Identity
// Center (SSO) session, performing the OAuth device-authorization flow
// against the SSO OIDC API directly (no "aws sso login" subprocess) when
// the cached token is missing or expired. It is a no-op for non-SSO
// profiles.
//
// openURL is called with the verification URL the user must open to
// authorize the device; this package has no opinion on browsers, chrome
// profiles, or operating systems — that's entirely the caller's concern.
func EnsureSSOLogin(ctx context.Context, profile, region string, openURL func(string) error) error {
	sharedCfg, err := config.LoadSharedConfigProfile(ctx, profile)
	if err != nil {
		return fmt.Errorf("loading shared config profile %q: %w", profile, err)
	}

	startURL, ssoRegion, cacheKey, ok := ssoSessionParams(sharedCfg)
	if !ok {
		return nil
	}

	if err := validateCredentials(ctx, profile, region); err == nil {
		return nil
	}

	log.Printf("SSO session for profile %q is missing or expired, starting device login...", profile)
	if err := performDeviceLogin(ctx, startURL, ssoRegion, cacheKey, openURL); err != nil {
		return fmt.Errorf("device login: %w", err)
	}

	return validateCredentials(ctx, profile, region)
}

func ssoSessionParams(cfg config.SharedConfig) (startURL, region, cacheKey string, ok bool) {
	if cfg.SSOSession != nil {
		return cfg.SSOSession.SSOStartURL, cfg.SSOSession.SSORegion, cfg.SSOSession.Name, true
	}
	if cfg.SSOStartURL != "" {
		return cfg.SSOStartURL, cfg.SSORegion, cfg.SSOStartURL, true
	}
	return "", "", "", false
}

func validateCredentials(ctx context.Context, profile, region string) error {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithSharedConfigProfile(profile),
		config.WithRegion(region),
	)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err = sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	return err
}

// performDeviceLogin runs the OAuth 2.0 device-authorization grant against
// SSO OIDC: register a public client, request device+user codes, ask the
// caller to open the verification URL, then poll for the token. On success
// the token is written to the same cache file the AWS CLI/SDK read from
// (~/.aws/sso/cache/<sha1>.json), so the regular SDK credential chain picks
// it up transparently on the next call.
func performDeviceLogin(ctx context.Context, startURL, region, cacheKey string, openURL func(string) error) error {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return fmt.Errorf("loading config for oidc client: %w", err)
	}
	oidc := ssooidc.NewFromConfig(cfg)

	reg, err := oidc.RegisterClient(ctx, &ssooidc.RegisterClientInput{
		ClientName: aws.String("conduit"),
		ClientType: aws.String("public"),
		Scopes:     []string{"sso:account:access"},
	})
	if err != nil {
		return fmt.Errorf("RegisterClient: %w", err)
	}

	auth, err := oidc.StartDeviceAuthorization(ctx, &ssooidc.StartDeviceAuthorizationInput{
		ClientId:     reg.ClientId,
		ClientSecret: reg.ClientSecret,
		StartUrl:     aws.String(startURL),
	})
	if err != nil {
		return fmt.Errorf("StartDeviceAuthorization: %w", err)
	}

	verificationURL := aws.ToString(auth.VerificationUriComplete)
	log.Printf("opening browser to authorize this device: %s", verificationURL)
	log.Printf("if it doesn't open automatically, visit %s and enter code: %s",
		aws.ToString(auth.VerificationUri), aws.ToString(auth.UserCode))
	if err := openURL(verificationURL); err != nil {
		log.Printf("could not open browser automatically: %v", err)
	}

	interval := time.Duration(auth.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(auth.ExpiresIn) * time.Second)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("device authorization expired before it was approved")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}

		token, err := oidc.CreateToken(ctx, &ssooidc.CreateTokenInput{
			ClientId:     reg.ClientId,
			ClientSecret: reg.ClientSecret,
			GrantType:    aws.String("urn:ietf:params:oauth:grant-type:device_code"),
			DeviceCode:   auth.DeviceCode,
		})
		if err != nil {
			var pending *types.AuthorizationPendingException
			var slowDown *types.SlowDownException
			switch {
			case errors.As(err, &pending):
				continue
			case errors.As(err, &slowDown):
				interval += 5 * time.Second
				continue
			default:
				return fmt.Errorf("CreateToken: %w", err)
			}
		}

		return storeSSOToken(cacheKey, token, reg)
	}
}

func storeSSOToken(cacheKey string, token *ssooidc.CreateTokenOutput, reg *ssooidc.RegisterClientOutput) error {
	path, err := ssocreds.StandardCachedTokenFilepath(cacheKey)
	if err != nil {
		return fmt.Errorf("computing cached token path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating sso cache dir: %w", err)
	}

	cached := map[string]string{
		"accessToken":  aws.ToString(token.AccessToken),
		"expiresAt":    time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).UTC().Format(time.RFC3339),
		"clientId":     aws.ToString(reg.ClientId),
		"clientSecret": aws.ToString(reg.ClientSecret),
	}
	if rt := aws.ToString(token.RefreshToken); rt != "" {
		cached["refreshToken"] = rt
	}

	data, err := json.Marshal(cached)
	if err != nil {
		return fmt.Errorf("marshaling cached token: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing cached token file: %w", err)
	}
	log.Printf("SSO login successful, cached token at %s", path)
	return nil
}
