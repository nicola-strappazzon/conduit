package cli

import (
	"bytes"
	"testing"

	"conduit/config"
)

func TestRootCommandFlags(t *testing.T) {
	cmd := NewRootCmd(func(config.Config) error { return nil })

	want := map[string]string{
		"profile":            "name",
		"region":             "eu-central-1",
		"target":             "",
		"document":           "AWS-StartPortForwardingSession",
		"remote-host":        "",
		"remote-port":        "",
		"local-port":         "",
		"reconnect":          "true",
		"reconnect-delay-ms": "2000",
		"chrome-profile":     "",
	}

	for name, defaultValue := range want {
		flag := cmd.Flags().Lookup(name)
		if flag == nil {
			t.Errorf("flag --%s is not registered", name)
			continue
		}
		if flag.DefValue != defaultValue {
			t.Errorf("flag --%s default = %q, want %q", name, flag.DefValue, defaultValue)
		}
	}
}

func TestRootCommandShowsHelpWithoutFlags(t *testing.T) {
	cmd := NewRootCmd(func(config.Config) error { return nil })
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute command: %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte("Usage:")) {
		t.Errorf("help output does not contain usage: %q", output.String())
	}
}

func TestRootCommandValidatesConfiguration(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "missing target",
			args: []string{"--profile", "test-profile", "--local-port", "3306", "--remote-port", "3306"},
			want: `required flag(s) "target" not set`,
		},
		{
			name: "invalid port",
			args: []string{"--target", "i-bastion", "--local-port", "not-a-port", "--remote-port", "3306"},
			want: "--local-port must be an integer between 1 and 65535",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := NewRootCmd(func(config.Config) error { return nil })
			cmd.SetArgs(test.args)
			err := cmd.Execute()
			if err == nil || err.Error() != test.want {
				t.Fatalf("execute command error = %v, want %q", err, test.want)
			}
		})
	}
}
