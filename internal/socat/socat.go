// Package socat builds the commands that manage a bastion listener.
package socat

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
)

var hostnamePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`)

// Runner runs a command in an interactive session on an SSM-managed target.
type Runner interface {
	Run(context.Context, string, string) (string, error)
}

// Manager ensures a bastion listener is ready before port forwarding begins.
type Manager struct {
	runner Runner
}

// NewManager returns a Manager that uses runner to execute commands remotely.
func NewManager(runner Runner) Manager {
	return Manager{runner: runner}
}

// Ensure asks the bastion to start or reuse the listener for port.
func (m Manager) Ensure(ctx context.Context, target, host, port string) error {
	command, err := EnsureCommand(host, port)
	if err != nil {
		return err
	}

	_, err = m.runner.Run(ctx, target, command)
	if err != nil {
		return fmt.Errorf("running socat setup: %w", err)
	}
	return nil
}

// Stop stops the listener for port without affecting socat listeners on other
// ports.
func (m Manager) Stop(ctx context.Context, target, port string) error {
	command, err := StopCommand(port)
	if err != nil {
		return err
	}

	_, err = m.runner.Run(ctx, target, command)
	if err != nil {
		return fmt.Errorf("stopping socat: %w", err)
	}
	return nil
}

// EnsureCommand builds the socat command used on the bastion.
func EnsureCommand(host, port string) (string, error) {
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return "", fmt.Errorf("port must be an integer between 1 and 65535")
	}
	if !hostnamePattern.MatchString(host) {
		return "", fmt.Errorf("host must be a hostname or IP address")
	}

	return fmt.Sprintf("sudo nohup socat TCP-LISTEN:%d,fork,reuseaddr TCP:%s:%d </dev/null >/tmp/socat-%d.log 2>&1 &", value, host, value, value), nil
}

// StopCommand builds the command that stops the listener for port.
func StopCommand(port string) (string, error) {
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return "", fmt.Errorf("port must be an integer between 1 and 65535")
	}

	return fmt.Sprintf("sudo pkill -f 'socat TCP-LISTEN:%d,fork,reuseaddr' || true", value), nil
}
