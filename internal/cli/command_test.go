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
		"region":             "eu-central-1",
		"target":             "",
		"document":           "AWS-StartPortForwardingSession",
		"remote-port":        "",
		"local-port":         "",
		"reconnect":          "true",
		"reconnect-delay-ms": "2000",
		"chrome-profile":     "",
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
