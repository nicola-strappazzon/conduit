// Command conduit opens an AWS SSM port-forwarding session, equivalent to:
//
//	aws ssm start-session --profile <profile> --region <region> --target <target> \
//	    --document-name AWS-StartPortForwardingSession \
//	    --parameters '{"portNumber":["<remote>"],"localPortNumber":["<local>"]}'
//
// All AWS-specific logic (credentials, SSM sessions, SSO login) lives in
// the aws package; this file only handles CLI flags, the reconnect loop,
// exec'ing session-manager-plugin, and picking which browser/profile opens
// the SSO login page.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"conduit/aws"
	"conduit/browser"
	"github.com/spf13/cobra"
)

type options struct {
	profile       string
	region        string
	target        string
	document      string
	remoteHost    string
	localPort     string
	remotePort    string
	reconnect     bool
	reconnectMS   int
	chromeProfile string
}

type sessionClient interface {
	StartSession(context.Context, aws.SessionParams) (*aws.Session, error)
	PluginArgs(*aws.Session) []string
	TerminateSession(context.Context, string) error
}

type conduitDependencies struct {
	newClient        func(context.Context, string, string) (sessionClient, error)
	ensureSSOLogin   func(context.Context, string, string, func(string) error) error
	openURL          func(string, string) error
	runSession       func(context.Context, sessionClient, aws.SessionParams, options) (string, error)
	terminateSession func(sessionClient, string)
	wait             func(context.Context, time.Duration) error
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		log.Fatal(err)
	}
}

func newRootCmd() *cobra.Command {
	opts := options{}
	cmd := &cobra.Command{
		Use:           "conduit",
		Short:         "A CLI that simplifies AWS SSM port forwarding.",
		Long:          "Conduit opens and maintains AWS SSM port-forwarding connections. It handles SSO login and can reconnect automatically when a session ends.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			return runConduit(opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.profile, "profile", "name", "AWS profile")
	flags.StringVar(&opts.region, "region", "eu-central-1", "AWS region")
	flags.StringVar(&opts.target, "target", "i-0d89d20fd1db52703", "SSM instance ID")
	flags.StringVar(&opts.document, "document", "AWS-StartPortForwardingSession", "SSM document")
	flags.StringVar(&opts.remoteHost, "remote-host", "", "Remote host")
	flags.StringVar(&opts.remotePort, "remote-port", "3306", "Remote port")
	flags.StringVar(&opts.localPort, "local-port", "3306", "Local port")
	flags.BoolVar(&opts.reconnect, "reconnect", true, "Reconnect automatically")
	flags.IntVar(&opts.reconnectMS, "reconnect-delay-ms", 2000, "Reconnect delay (ms)")
	flags.StringVar(&opts.chromeProfile, "chrome-profile", "", "Chrome profile for SSO")

	return cmd
}

func runConduit(opts options) error {
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
		terminateSession: func(client sessionClient, sessionID string) {
			terminateSession(client, sessionID)
		},
		wait: waitForReconnect,
	}
}

func runConduitWithContext(ctx context.Context, opts options, deps conduitDependencies) error {
	client, err := deps.newClient(ctx, opts.profile, opts.region)
	if err != nil {
		return err
	}

	parameters := map[string][]string{
		"portNumber":      {opts.remotePort},
		"localPortNumber": {opts.localPort},
	}
	if opts.remoteHost != "" {
		parameters["host"] = []string{opts.remoteHost}
	}

	params := aws.SessionParams{
		Target:     opts.target,
		Document:   opts.document,
		Parameters: parameters,
	}

	openURL := func(url string) error { return deps.openURL(url, opts.chromeProfile) }

	for {
		if ctx.Err() != nil {
			log.Println("shutting down")
			return nil
		}

		if err := deps.ensureSSOLogin(ctx, opts.profile, opts.region, openURL); err != nil {
			return fmt.Errorf("SSO login: %w", err)
		}

		sessionID, err := deps.runSession(ctx, client, params, opts)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("error: %v", err)
		}
		if sessionID != "" {
			deps.terminateSession(client, sessionID)
		}

		if ctx.Err() != nil {
			log.Println("shutting down")
			return nil
		}
		if !opts.reconnect {
			return nil
		}

		log.Printf("session ended, reconnecting in %dms...", opts.reconnectMS)
		if err := deps.wait(ctx, time.Duration(opts.reconnectMS)*time.Millisecond); err != nil {
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
// process exits (connection closed, network drop, or ctx cancellation). It
// returns the session ID so the caller can terminate it server-side.
func runOnce(ctx context.Context, client sessionClient, params aws.SessionParams, opts options) (string, error) {
	session, err := client.StartSession(ctx, params)
	if err != nil {
		return "", err
	}

	target := opts.target
	if opts.remoteHost != "" {
		target = opts.remoteHost
	}
	log.Printf("session %s: forwarding localhost:%s -> %s:%s", session.ID, opts.localPort, target, opts.remotePort)

	cmd := exec.CommandContext(ctx, aws.PluginBinary, client.PluginArgs(session)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if ctx.Err() != nil {
		return session.ID, context.Canceled
	}
	if errors.Is(err, exec.ErrNotFound) {
		return session.ID, fmt.Errorf("%s not found in PATH; install it with: brew install --cask session-manager-plugin", aws.PluginBinary)
	}
	if err != nil {
		return session.ID, fmt.Errorf("%s: %w", aws.PluginBinary, err)
	}
	return session.ID, nil
}

func terminateSession(client sessionClient, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.TerminateSession(ctx, sessionID); err != nil {
		log.Printf("warning: could not terminate session %s server-side: %v", sessionID, err)
	}
}
