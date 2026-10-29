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
// A nil openURL only prints the URL and code, so the device can be
// authorized from any other machine.
func EnsureSSOLogin(ctx context.Context, profile, region string, openURL func(string) error) error {
	sharedCfg, err := config.LoadSharedConfigProfile(ctx, profile)
	if err != nil {
		return fmt.Errorf("loading shared config profile %q: %w", profile, err)
	}

	startURL, ssoRegion, cacheKey, ok := ssoSessionParams(sharedCfg)
	if !ok {
		return nil
	}

	// Explicitly refresh the cached token for legacy profiles too: the SDK's
	// default credential chain only installs a token provider for sso_session.
	path, err := ssocreds.StandardCachedTokenFilepath(cacheKey)
	if err != nil {
		return err
	}
	oidc := ssooidc.NewFromConfig(aws.Config{Region: ssoRegion})
	if err := refreshCachedSSOToken(ctx, path, oidc); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Missing/expired sessions require user authorization. Network and
		// service failures must not trigger an unrelated device login.
		if !ssoLoginRequired(err) {
			return fmt.Errorf("refreshing SSO session: %w", err)
		}

		log.Printf("SSO cached token could not be renewed; checking whether login is required")
	}

	if err := validateCredentials(ctx, profile, region); err == nil {
		return nil
	}

	log.Printf("SSO session for profile %q requires authorization; starting device login (approve the URL before the code expires)...", profile)
	if err := performDeviceLogin(ctx, startURL, ssoRegion, cacheKey, openURL); err != nil {
		return fmt.Errorf("device login: %w", err)
	}

	return validateCredentials(ctx, profile, region)
}

var errSSOLoginRequired = errors.New("SSO login required")

// A rejected cached refresh request can be recovered by registering a new
// client and asking the user to authorize it. Transport/service failures
// must remain errors rather than triggering a device login.
func ssoLoginRequired(err error) bool {
	if errors.Is(err, errSSOLoginRequired) {
		return true
	}
	var apiErr interface{ ErrorCode() string }
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.ErrorCode() {
	case "InvalidGrantException", "InvalidClientException", "UnauthorizedClientException", "ExpiredTokenException", "InvalidRequestException":
		return true
	default:
		return false
	}
}

func refreshCachedSSOToken(ctx context.Context, path string, client ssocreds.CreateTokenAPIClient) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return errSSOLoginRequired
	}
	if err != nil {
		return err
	}
	var cached struct {
		AccessToken  string    `json:"accessToken"`
		ExpiresAt    time.Time `json:"expiresAt"`
		RefreshToken string    `json:"refreshToken"`
		ClientID     string    `json:"clientId"`
		ClientSecret string    `json:"clientSecret"`
	}
	if json.Unmarshal(data, &cached) != nil || cached.AccessToken == "" || cached.ExpiresAt.IsZero() {
		return errSSOLoginRequired
	}
	if time.Now().Before(cached.ExpiresAt) {
		return nil
	}
	if cached.RefreshToken == "" || cached.ClientID == "" || cached.ClientSecret == "" {
		return errSSOLoginRequired
	}
	log.Printf("SSO access token expired; attempting automatic renewal...")
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err = ssocreds.NewSSOTokenProvider(client, path).RetrieveBearerToken(ctx)
	if err == nil {
		log.Printf("SSO access token renewed automatically")
	}
	return err
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
	if openURL == nil {
		log.Printf("open this URL on any device to authorize this login: %s", verificationURL)
		log.Printf("or visit %s and enter code: %s",
			aws.ToString(auth.VerificationUri), aws.ToString(auth.UserCode))
	} else {
		log.Printf("opening browser to authorize this device: %s", verificationURL)
		log.Printf("if it doesn't open automatically, visit %s and enter code: %s",
			aws.ToString(auth.VerificationUri), aws.ToString(auth.UserCode))
		if err := openURL(verificationURL); err != nil {
			log.Printf("could not open browser automatically: %v", err)
		}
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
			var expired *types.ExpiredTokenException
			switch {
			case errors.As(err, &pending):
				continue
			case errors.As(err, &slowDown):
				interval += 5 * time.Second
				continue
			case errors.As(err, &expired):
				return fmt.Errorf("device authorization expired before approval; restart conduit to request a new code: %w", err)
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
