package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"conduit/aws"
)

type fakeSessionClient struct{}

func (fakeSessionClient) StartSession(context.Context, aws.SessionParams) (*aws.Session, error) {
	return nil, nil
}

func (fakeSessionClient) PluginArgs(*aws.Session) []string { return nil }

func (fakeSessionClient) TerminateSession(context.Context, string) error { return nil }

func TestRootCommandFlags(t *testing.T) {
	cmd := newRootCmd()

	want := map[string]string{
		"profile":            "name",
		"region":             "eu-central-1",
		"target":             "i-0d89d20fd1db52703",
		"document":           "AWS-StartPortForwardingSession",
		"remote-host":        "",
		"remote-port":        "3306",
		"local-port":         "3306",
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

func TestConduitFlowUsesInjectedDependencies(t *testing.T) {
	var terminatedID string
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
		runSession: func(_ context.Context, _ sessionClient, params aws.SessionParams, _ options) (string, error) {
			if params.Target != "i-test" || params.Parameters["portNumber"][0] != "3307" {
				t.Fatalf("unexpected session parameters: %#v", params)
			}
			return "session-1", errors.New("plugin exited")
		},
		terminateSession: func(_ sessionClient, sessionID string) {
			terminatedID = sessionID
		},
		wait: func(context.Context, time.Duration) error {
			t.Fatal("wait called with reconnect disabled")
			return nil
		},
	}

	err := runConduitWithContext(context.Background(), options{
		profile:       "test-profile",
		region:        "test-region",
		target:        "i-test",
		document:      "AWS-StartPortForwardingSession",
		localPort:     "3306",
		remotePort:    "3307",
		chromeProfile: "Profile 1",
		reconnect:     false,
	}, deps)
	if err != nil {
		t.Fatalf("run conduit: %v", err)
	}
	if terminatedID != "session-1" {
		t.Errorf("terminated session = %q, want session-1", terminatedID)
	}
}
