// Command conduit opens an AWS SSM port-forwarding session, equivalent to:
//
//	aws ssm start-session --profile <profile> --region <region> --target <target> \
//	    --document-name AWS-StartPortForwardingSession \
//	    --parameters '{"portNumber":["<remote>"],"localPortNumber":["<local>"]}'
//
// All AWS-specific logic (credentials, SSM sessions, SSO login) lives in
// the aws package; this file handles the reconnect loop, exec'ing
// session-manager-plugin, and picking which browser/profile opens the SSO
// login page.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"conduit/aws"
	"conduit/browser"
	"conduit/cli"
	"conduit/config"
)

type sessionClient interface {
	StartSession(context.Context, aws.SessionParams) (*aws.Session, error)
	PluginArgs(*aws.Session) []string
}

type conduitDependencies struct {
	newClient      func(context.Context, string, string) (sessionClient, error)
	ensureSSOLogin func(context.Context, string, string, func(string) error) error
	openURL        func(string, string) error
	runSession     func(context.Context, sessionClient, aws.SessionParams, config.Config) error
	wait           func(context.Context, time.Duration) error
}

// lineLogWriter turns a process stream into individual application log lines.
// A process may split one line across several Write calls, so incomplete lines
// are buffered until a newline arrives.
type lineLogWriter struct {
	mu      sync.Mutex
	pending []byte
	logLine func(string)
}

func newLineLogWriter(logLine func(string)) *lineLogWriter {
	return &lineLogWriter{logLine: logLine}
}

func (w *lineLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.pending = append(w.pending, p...)
	w.writeCompleteLines()
	return len(p), nil
}

func (w *lineLogWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.log(strings.TrimSpace(string(w.pending)))
	w.pending = nil
}

func (w *lineLogWriter) writeCompleteLines() {
	for {
		newline := bytes.IndexByte(w.pending, '\n')
		if newline == -1 {
			return
		}
		w.log(strings.TrimSpace(string(w.pending[:newline])))
		w.pending = w.pending[newline+1:]
	}
}

func (w *lineLogWriter) log(line string) {
	if line != "" {
		w.logLine(line)
	}
}

func main() {
	if err := cli.NewRootCmd(runConduit).Execute(); err != nil {
		log.Fatal(err)
	}
}

func runConduit(opts config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runConduitWithContext(ctx, opts, productionDependencies())
}

func productionDependencies() conduitDependencies {
	return conduitDependencies{
		newClient: func(ctx context.Context, profile, region string) (sessionClient, error) {
			return aws.NewClient(ctx, profile, region)
		},
		ensureSSOLogin: aws.EnsureSSOLogin,
		openURL:        browser.Open,
		runSession:     runOnce,
		wait:           waitForReconnect,
	}
}

func runConduitWithContext(ctx context.Context, opts config.Config, deps conduitDependencies) error {
	client, err := deps.newClient(ctx, opts.Profile, opts.Region)
	if err != nil {
		return err
	}

	parameters := map[string][]string{
		"portNumber":      {opts.RemotePort},
		"localPortNumber": {opts.LocalPort},
	}
	if opts.RemoteHost != "" {
		parameters["host"] = []string{opts.RemoteHost}
	}

	params := aws.SessionParams{
		Target:     opts.Target,
		Document:   opts.Document,
		Parameters: parameters,
	}

	openURL := func(url string) error { return deps.openURL(url, opts.ChromeProfile) }

	for {
		if ctx.Err() != nil {
			log.Println("shutting down")
			return nil
		}

		if err := deps.ensureSSOLogin(ctx, opts.Profile, opts.Region, openURL); err != nil {
			return fmt.Errorf("SSO login: %w", err)
		}

		err := deps.runSession(ctx, client, params, opts)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Println(err.Error())
		}

		if ctx.Err() != nil {
			log.Println("shutting down")
			return nil
		}
		if !opts.Reconnect {
			return nil
		}

		log.Printf("session ended, reconnecting in %dms...", opts.ReconnectMS)
		if err := deps.wait(ctx, time.Duration(opts.ReconnectMS)*time.Millisecond); err != nil {
			if errors.Is(err, context.Canceled) {
				log.Println("shutting down")
				return nil
			}
			return err
		}
	}
}

func waitForReconnect(ctx context.Context, delay time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

// runOnce starts one SSM session and blocks until the session-manager-plugin
// process exits (connection closed, network drop, or ctx cancellation).
func runOnce(ctx context.Context, client sessionClient, params aws.SessionParams, opts config.Config) error {
	session, err := client.StartSession(ctx, params)
	if err != nil {
		return err
	}

	target := opts.Target
	if opts.RemoteHost != "" {
		target = opts.RemoteHost
	}
	log.Printf("session %s: forwarding localhost:%s -> %s:%s", session.ID, opts.LocalPort, target, opts.RemotePort)

	cmd := exec.CommandContext(ctx, aws.PluginBinary, client.PluginArgs(session)...)
	cmd.Stdin = os.Stdin
	pluginLog := newLineLogWriter(func(line string) { log.Print(line) })
	cmd.Stdout = pluginLog
	cmd.Stderr = pluginLog

	err = cmd.Run()
	pluginLog.Flush()
	if ctx.Err() != nil {
		return context.Canceled
	}
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%s not found in PATH; install it with: brew install --cask session-manager-plugin", aws.PluginBinary)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", aws.PluginBinary, err)
	}
	return nil
}
