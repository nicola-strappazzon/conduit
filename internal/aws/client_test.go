package aws

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/credentials/ssocreds"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEndpoint(t *testing.T) {
	client := Client{Region: "eu-central-1"}

	assert.Equal(t, "https://ssm.eu-central-1.amazonaws.com", client.Endpoint())
}

func TestDisabledSSORefreshReadsCacheWithoutChangingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.json")
	provider := ssocreds.NewSSOTokenProvider(disabledSSORefresh{}, path)
	for _, expired := range []bool{true, false} {
		expiry := time.Now().Add(time.Hour)
		if expired {
			expiry = time.Now().Add(-time.Hour)
		}
		data := []byte(fmt.Sprintf(`{"accessToken":"shared-token","expiresAt":%q,"refreshToken":"refresh-token","clientId":"client","clientSecret":"secret"}`, expiry.UTC().Format(time.RFC3339)))
		require.NoError(t, os.WriteFile(path, data, 0600))
		token, err := provider.RetrieveBearerToken(context.Background())
		if expired {
			require.ErrorContains(t, err, "--no-sso-refresh")
		} else {
			require.NoError(t, err)
			require.Equal(t, "shared-token", token.Value)
		}
		after, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, data, after)
	}
}
