package socat

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeRunner struct {
	target  string
	command string
	output  string
	err     error
}

func (r *fakeRunner) Run(_ context.Context, target, command string) (string, error) {
	r.target = target
	r.command = command
	return r.output, r.err
}

func TestEnsureCommand(t *testing.T) {
	command, err := EnsureCommand("database.internal", "3306")

	assert.NoError(t, err)
	assert.Equal(t, "sudo nohup socat TCP-LISTEN:3306,fork,reuseaddr TCP:database.internal:3306 </dev/null >/tmp/socat-3306.log 2>&1 &", command)
}

func TestEnsureCommandRejectsInvalidPort(t *testing.T) {
	_, err := EnsureCommand("database.internal", "invalid")

	assert.EqualError(t, err, "port must be an integer between 1 and 65535")
}

func TestEnsureCommandRejectsInvalidHost(t *testing.T) {
	_, err := EnsureCommand("database.internal; rm", "3306")

	assert.EqualError(t, err, "host must be a hostname or IP address")
}

func TestStopCommand(t *testing.T) {
	command, err := StopCommand("3306")

	assert.NoError(t, err)
	assert.Equal(t, "sudo pkill -f 'socat TCP-LISTEN:3306,fork,reuseaddr' || true", command)
}

func TestManagerEnsure(t *testing.T) {
	runner := &fakeRunner{}

	err := NewManager(runner).Ensure(context.Background(), "i-bastion", "database.internal", "3306")

	assert.NoError(t, err)
	assert.Equal(t, "i-bastion", runner.target)
	assert.Contains(t, runner.command, "TCP-LISTEN:3306")
}

func TestManagerEnsureReturnsRunnerError(t *testing.T) {
	runner := &fakeRunner{err: errors.New("sudo failed")}

	err := NewManager(runner).Ensure(context.Background(), "i-bastion", "database.internal", "3306")

	assert.EqualError(t, err, "running socat setup: sudo failed")
}
