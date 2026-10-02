package conduit

import (
	"context"
	"errors"
	"testing"
	"time"

	"conduit/internal/aws"
	"conduit/internal/config"
	"github.com/stretchr/testify/assert"
)

type fakeSessionClient struct{}

func (fakeSessionClient) StartSession(context.Context, aws.SessionParams) (*aws.Session, error) {
	return nil, nil
}

func (fakeSessionClient) PluginArgs(*aws.Session) []string { return nil }

func TestLineLogWriter(t *testing.T) {
	var lines []string
	writer := newLineLogWriter(func(line string) {
		lines = append(lines, line)
	})

	_, err := writer.Write([]byte("Starting session\nPort 3306"))
	assert.NoError(t, err)
	_, err = writer.Write([]byte(" opened\n\nWaiting for connections..."))
	assert.NoError(t, err)
	writer.Flush()

	want := []string{
		"Starting session",
		"Port 3306 opened",
		"Waiting for connections...",
	}
	assert.Equal(t, want, lines)
}

func TestNormalPluginLogLine(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{line: "Waiting for connections...", want: "Waiting for connections..."},
		{line: "Connection accepted for session [user@example.com-session]", want: "Connection accepted"},
		{line: "api error AccessDeniedException", want: "api error AccessDeniedException"},
		{line: "connection failed", want: "connection failed"},
		{line: "Starting session with SessionId: user@example.com-session", want: ""},
	}

	for _, test := range tests {
		assert.Equal(t, test.want, normalPluginLogLine(test.line), test.line)
	}
}

func TestRunWithInjectedDependencies(t *testing.T) {
	deps := dependencies{
		newClient: func(_ context.Context, profile, region string) (sessionClient, error) {
			assert.Equal(t, "test-profile", profile)
			assert.Equal(t, "test-region", region)
			return fakeSessionClient{}, nil
		},
		ensureSSOLogin: func(_ context.Context, profile, region string, openURL func(string) error) error {
			assert.Equal(t, "test-profile", profile)
			assert.Equal(t, "test-region", region)
			return openURL("https://example.com/device")
		},
		openURL: func(url, chromeProfile string) error {
			assert.Equal(t, "https://example.com/device", url)
			assert.Equal(t, "Profile 1", chromeProfile)
			return nil
		},
		runSession: func(_ context.Context, _ sessionClient, params aws.SessionParams, _ config.Config) error {
			assert.Equal(t, "i-test", params.Target)
			assert.Equal(t, []string{"3307"}, params.Parameters["portNumber"])
			return errors.New("plugin exited")
		},
		wait: func(context.Context, time.Duration) error {
			assert.Fail(t, "wait called with reconnect disabled")
			return nil
		},
	}

	err := runWithContext(context.Background(), config.Config{
		Profile:       "test-profile",
		Region:        "test-region",
		Target:        "i-test",
		Document:      "AWS-StartPortForwardingSession",
		LocalPort:     "3306",
		RemotePort:    "3307",
		ChromeProfile: "Profile 1",
		Reconnect:     false,
	}, deps)
	assert.NoError(t, err)
}
