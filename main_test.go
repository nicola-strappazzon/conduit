package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"conduit/aws"
	"conduit/config"
)

type fakeSessionClient struct{}

func (fakeSessionClient) StartSession(context.Context, aws.SessionParams) (*aws.Session, error) {
	return nil, nil
}

func (fakeSessionClient) PluginArgs(*aws.Session) []string { return nil }

func TestRootCommandFlags(t *testing.T) {
	cmd := newRootCmd()

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
	cmd := newRootCmd()
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

func TestRootCommandRequiresTargetWhenFlagsAreProvided(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--profile", "test-profile", "--local-port", "3306", "--remote-port", "3306"})

	err := cmd.Execute()
	if err == nil || err.Error() != `required flag(s) "target" not set` {
		t.Fatalf("execute command error = %v", err)
	}
}

func TestRootCommandValidatesPorts(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"--target", "i-bastion",
		"--local-port", "not-a-port",
		"--remote-port", "3306",
	})

	err := cmd.Execute()
	if err == nil || err.Error() != "--local-port must be an integer between 1 and 65535" {
		t.Fatalf("execute command error = %v", err)
	}
}

func TestLineLogWriter(t *testing.T) {
	var lines []string
	writer := newLineLogWriter(func(line string) {
		lines = append(lines, line)
	})

	if _, err := writer.Write([]byte("Starting session\nPort 3306")); err != nil {
		t.Fatalf("write first chunk: %v", err)
	}
	if _, err := writer.Write([]byte(" opened\n\nWaiting for connections...")); err != nil {
		t.Fatalf("write second chunk: %v", err)
	}
	writer.Flush()

	want := []string{
		"Starting session",
		"Port 3306 opened",
		"Waiting for connections...",
	}
	if len(lines) != len(want) {
		t.Fatalf("logged lines = %#v, want %#v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestConduitFlowUsesInjectedDependencies(t *testing.T) {
	deps := conduitDependencies{
		newClient: func(_ context.Context, profile, region string) (sessionClient, error) {
			if profile != "test-profile" || region != "test-region" {
				t.Fatalf("client config = %q, %q", profile, region)
			}
			return fakeSessionClient{}, nil
		},
		ensureSSOLogin: func(_ context.Context, profile, region string, openURL func(string) error) error {
			if profile != "test-profile" || region != "test-region" {
				t.Fatalf("SSO config = %q, %q", profile, region)
			}
			return openURL("https://example.com/device")
		},
		openURL: func(url, chromeProfile string) error {
			if url != "https://example.com/device" || chromeProfile != "Profile 1" {
				t.Fatalf("browser config = %q, %q", url, chromeProfile)
			}
			return nil
		},
		runSession: func(_ context.Context, _ sessionClient, params aws.SessionParams, _ config.Config) error {
			if params.Target != "i-test" || params.Parameters["portNumber"][0] != "3307" {
				t.Fatalf("unexpected session parameters: %#v", params)
			}
			return errors.New("plugin exited")
		},
		wait: func(context.Context, time.Duration) error {
			t.Fatal("wait called with reconnect disabled")
			return nil
		},
	}

	err := runConduitWithContext(context.Background(), config.Config{
		Profile:       "test-profile",
		Region:        "test-region",
		Target:        "i-test",
		Document:      "AWS-StartPortForwardingSession",
		LocalPort:     "3306",
		RemotePort:    "3307",
		ChromeProfile: "Profile 1",
		Reconnect:     false,
	}, deps)
	if err != nil {
		t.Fatalf("run conduit: %v", err)
	}
}
