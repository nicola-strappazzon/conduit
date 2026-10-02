package aws

import (
	"testing"

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
