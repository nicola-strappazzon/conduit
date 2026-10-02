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
}

type conduitDependencies struct {
	newClient      func(context.Context, string, string) (sessionClient, error)
	ensureSSOLogin func(context.Context, string, string, func(string) error) error
	openURL        func(string, string) error
	runSession     func(context.Context, sessionClient, aws.SessionParams, options) error
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
			if opts.target == "" {
				return fmt.Errorf("required flag(s) \"target\" not set")
			}
			return runConduit(opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.profile, "profile", "name", "AWS profile")
	flags.StringVar(&opts.region, "region", "eu-central-1", "AWS region")
	flags.StringVar(&opts.target, "target", "", "SSM instance ID (required)")
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
		wait:           waitForReconnect,
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

		err := deps.runSession(ctx, client, params, opts)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Println(err.Error())
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
// process exits (connection closed, network drop, or ctx cancellation).
func runOnce(ctx context.Context, client sessionClient, params aws.SessionParams, opts options) error {
	session, err := client.StartSession(ctx, params)
	if err != nil {
		return err
	}

	target := opts.target
	if opts.remoteHost != "" {
		target = opts.remoteHost
	}
	log.Printf("session %s: forwarding localhost:%s -> %s:%s", session.ID, opts.localPort, target, opts.remotePort)

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
