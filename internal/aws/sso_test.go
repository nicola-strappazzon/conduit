package aws

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	"github.com/stretchr/testify/require"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/stretchr/testify/assert"
)

func TestSSOSessionParams(t *testing.T) {
	tests := []struct {
		name    string
		cfg     awscfg.SharedConfig
		start   string
		region  string
		cache   string
		present bool
	}{
		{
			name: "sso session",
			cfg: awscfg.SharedConfig{SSOSession: &awscfg.SSOSession{
				Name: "work", SSORegion: "eu-central-1", SSOStartURL: "https://start.example.com",
			}},
			start: "https://start.example.com", region: "eu-central-1", cache: "work", present: true,
		},
		{
			name:    "legacy sso",
			cfg:     awscfg.SharedConfig{SSOStartURL: "https://legacy.example.com", SSORegion: "us-east-1"},
			start:   "https://legacy.example.com",
			region:  "us-east-1",
			cache:   "https://legacy.example.com",
			present: true,
		},
		{name: "non sso profile"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start, region, cache, present := ssoSessionParams(test.cfg)

			assert.Equal(t, test.start, start)
			assert.Equal(t, test.region, region)
			assert.Equal(t, test.cache, cache)
			assert.Equal(t, test.present, present)
		})
	}
}

type refreshClient struct {
	calls int
	err   error
}

func (c *refreshClient) CreateToken(ctx context.Context, input *ssooidc.CreateTokenInput, opts ...func(*ssooidc.Options)) (*ssooidc.CreateTokenOutput, error) {
	c.calls++
	if aws.ToString(input.GrantType) != "refresh_token" || aws.ToString(input.RefreshToken) != "refresh-old" {
		return nil, errors.New("unexpected refresh request")
	}
	if c.err != nil {
		return nil, c.err
	}
	return &ssooidc.CreateTokenOutput{AccessToken: aws.String("access-new"), RefreshToken: aws.String("refresh-new"), ExpiresIn: 3600}, nil
}

func TestRefreshCachedSSOToken(t *testing.T) {
	for _, scenario := range []string{"expired", "valid", "missing", "no refresh token", "network failure", "malformed"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "token.json")
			client := &refreshClient{}
			expiry := time.Now().Add(-time.Hour)
			if scenario == "valid" {
				expiry = time.Now().Add(time.Hour)
			}
			cached := map[string]any{"accessToken": "access-old", "expiresAt": expiry.UTC().Format(time.RFC3339), "refreshToken": "refresh-old", "clientId": "client", "clientSecret": "secret", "custom": "preserved"}
			if scenario == "no refresh token" {
				delete(cached, "refreshToken")
			}
			data, err := json.Marshal(cached)
			require.NoError(t, err)
			if scenario == "malformed" {
				data = []byte("invalid")
			}
			if scenario != "missing" {
				require.NoError(t, os.WriteFile(path, data, 0600))
			}
			networkErr := errors.New("network unavailable")
			if scenario == "network failure" {
				client.err = networkErr
			}
			err = refreshCachedSSOToken(context.Background(), path, client)
			switch scenario {
			case "missing", "no refresh token", "malformed":
				require.ErrorIs(t, err, errSSOLoginRequired)
				require.Zero(t, client.calls)
			case "network failure":
				require.ErrorIs(t, err, networkErr)
				got, readErr := os.ReadFile(path)
				require.NoError(t, readErr)
				require.Equal(t, data, got)
			case "valid":
				require.NoError(t, err)
				require.Zero(t, client.calls)
			case "expired":
				require.NoError(t, err)
				require.Equal(t, 1, client.calls)
				got, readErr := os.ReadFile(path)
				require.NoError(t, readErr)
				require.NoError(t, json.Unmarshal(got, &cached))
				require.Equal(t, "access-new", cached["accessToken"])
				require.Equal(t, "refresh-new", cached["refreshToken"])
				require.Equal(t, "preserved", cached["custom"])
			}
		})
	}
}

// Opt-in check of real credentials; never starts device authorization or opens a tunnel.
func TestLiveSSORefresh(t *testing.T) {
	profile := os.Getenv("CONDUIT_TEST_SSO_PROFILE")
	if profile == "" {
		t.Skip("set CONDUIT_TEST_SSO_PROFILE to check a real SSO session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg, err := awscfg.LoadSharedConfigProfile(ctx, profile)
	require.NoError(t, err)
	_, region, key, ok := ssoSessionParams(cfg)
	require.True(t, ok, "profile must use SSO")
	path, err := ssocreds.StandardCachedTokenFilepath(key)
	require.NoError(t, err)
	require.NoError(t, refreshCachedSSOToken(ctx, path, ssooidc.NewFromConfig(aws.Config{Region: region})))
	require.NoError(t, validateCredentials(ctx, profile, region))
}
