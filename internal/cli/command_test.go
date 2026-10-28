package cli

import (
	"bytes"
	"testing"

	"conduit/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestRootCommandFlags(t *testing.T) {
	cmd := NewRootCmd(func(config.Config) error { return nil })

	want := map[string]string{
		"profile":            "name",
		"listen-address":     "127.0.0.1",
		"region":             "eu-central-1",
		"target":             "",
		"document":           "AWS-StartPortForwardingSession",
		"remote-port":        "",
		"local-port":         "",
		"reconnect":          "true",
		"reconnect-delay-ms": "2000",
		"chrome-profile":     "",
		"no-browser":         "false",
		"no-sso-refresh":     "false",
		"debug":              "false",
		"host":               "",
	}

	for name, defaultValue := range want {
		flag := cmd.Flags().Lookup(name)
		if assert.NotNil(t, flag, "flag --%s is not registered", name) {
			assert.Equal(t, defaultValue, flag.DefValue, "flag --%s default", name)
		}
	}
}

func TestRootCommandShowsHelpWithoutFlags(t *testing.T) {
	cmd := NewRootCmd(func(config.Config) error { return nil })
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs(nil)

	assert.NoError(t, cmd.Execute())
	assert.Contains(t, output.String(), "Usage:")
}

func TestRootCommandValidatesConfiguration(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "missing target",
			args: []string{"--profile", "test-profile", "--host", "database.internal", "--local-port", "3306", "--remote-port", "3306"},
			want: `required flag(s) "target" not set`,
		},
		{
			name: "invalid port",
			args: []string{"--target", "i-bastion", "--host", "database.internal", "--local-port", "not-a-port", "--remote-port", "3306"},
			want: "--local-port must be an integer between 1 and 65535",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := NewRootCmd(func(config.Config) error { return nil })
			cmd.SetArgs(test.args)
			err := cmd.Execute()
			assert.EqualError(t, err, test.want)
		})
	}
}

func TestListenAddressFlag(t *testing.T) {
	var got config.Config
	cmd := NewRootCmd(func(c config.Config) error { got = c; return nil })
	cmd.SetArgs([]string{"--target", "i-bastion", "--host", "db.internal", "--local-port", "3307", "--remote-port", "3306", "--listen-address", "192.168.1.100"})
	assert.NoError(t, cmd.Execute())
	assert.Equal(t, "192.168.1.100", got.ListenAddress)
}

func TestNoSSORefreshFlag(t *testing.T) {
	var got config.Config
	cmd := NewRootCmd(func(c config.Config) error { got = c; return nil })
	cmd.SetArgs([]string{"--target", "i-bastion", "--host", "db.internal", "--local-port", "3307", "--remote-port", "3306", "--no-sso-refresh"})
	assert.NoError(t, cmd.Execute())
	assert.True(t, got.NoSSORefresh)
}
