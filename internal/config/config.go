// Package config defines Conduit's command-line configuration.
package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Config holds the values accepted by the Conduit command line.
type Config struct {
	Profile       string
	Region        string
	Target        string
	Document      string
	LocalPort     string
	RemotePort    string
	Reconnect     bool
	ReconnectMS   int
	ChromeProfile string
	Debug         bool
	Host          string
}

// Defaults returns the configuration used before command-line flags are read.
func Defaults() Config {
	return Config{
		Profile:     "name",
		Region:      "eu-central-1",
		Document:    "AWS-StartPortForwardingSession",
		Reconnect:   true,
		ReconnectMS: 2000,
	}
}

// Validate checks the values that must be supplied for a port-forwarding session.
func (c Config) Validate() error {
	if missing := c.missingRequiredFlags(); len(missing) > 0 {
		return fmt.Errorf("required flag(s) %s not set", strings.Join(missing, ", "))
	}
	if err := validatePort("local-port", c.LocalPort); err != nil {
		return err
	}
	if err := validatePort("remote-port", c.RemotePort); err != nil {
		return err
	}
	if !hostnamePattern.MatchString(c.Host) {
		return fmt.Errorf("--host must be a hostname or IP address")
	}
	return nil
}

func (c Config) missingRequiredFlags() []string {
	var missing []string
	if c.Target == "" {
		missing = append(missing, `"target"`)
	}
	if c.LocalPort == "" {
		missing = append(missing, `"local-port"`)
	}
	if c.RemotePort == "" {
		missing = append(missing, `"remote-port"`)
	}
	if c.Host == "" {
		missing = append(missing, `"host"`)
	}
	return missing
}

func validatePort(name, value string) error {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("--%s must be an integer between 1 and 65535", name)
	}
	return nil
}

var hostnamePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`)
