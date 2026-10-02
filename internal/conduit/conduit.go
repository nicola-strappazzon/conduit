// Package conduit runs the port-forwarding application.
package conduit

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
	"conduit/config"
)

type sessionClient interface {
	StartSession(context.Context, aws.SessionParams) (*aws.Session, error)
	PluginArgs(*aws.Session) []string
}

type dependencies struct {
	newClient      func(context.Context, string, string) (sessionClient, error)
	ensureSSOLogin func(context.Context, string, string, func(string) error) error
	openURL        func(string, string) error
	runSession     func(context.Context, sessionClient, aws.SessionParams, config.Config) error
	wait           func(context.Context, time.Duration) error
}

// Run opens the tunnel and reconnects according to cfg.
func Run(cfg config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runWithContext(ctx, cfg, productionDependencies())
}

func productionDependencies() dependencies {
	return dependencies{
		newClient: func(ctx context.Context, profile, region string) (sessionClient, error) {
			return aws.NewClient(ctx, profile, region)
		},
		ensureSSOLogin: aws.EnsureSSOLogin,
		openURL:        browser.Open,
		runSession:     runOnce,
		wait:           waitForReconnect,
	}
}

func runWithContext(ctx context.Context, cfg config.Config, deps dependencies) error {
	client, err := deps.newClient(ctx, cfg.Profile, cfg.Region)
	if err != nil {
		return err
	}

	parameters := map[string][]string{
		"portNumber":      {cfg.RemotePort},
		"localPortNumber": {cfg.LocalPort},
	}
	if cfg.RemoteHost != "" {
		parameters["host"] = []string{cfg.RemoteHost}
	}

	params := aws.SessionParams{
		Target:     cfg.Target,
		Document:   cfg.Document,
		Parameters: parameters,
	}

	openURL := func(url string) error { return deps.openURL(url, cfg.ChromeProfile) }

	for {
		if ctx.Err() != nil {
			log.Println("shutting down")
			return nil
		}

		if err := deps.ensureSSOLogin(ctx, cfg.Profile, cfg.Region, openURL); err != nil {
			return fmt.Errorf("SSO login: %w", err)
		}

		err := deps.runSession(ctx, client, params, cfg)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Println(err.Error())
		}

		if ctx.Err() != nil {
			log.Println("shutting down")
			return nil
		}
		if !cfg.Reconnect {
			return nil
		}

		log.Printf("session ended, reconnecting in %dms...", cfg.ReconnectMS)
		if err := deps.wait(ctx, time.Duration(cfg.ReconnectMS)*time.Millisecond); err != nil {
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

func runOnce(ctx context.Context, client sessionClient, params aws.SessionParams, cfg config.Config) error {
	session, err := client.StartSession(ctx, params)
	if err != nil {
		return err
	}

	target := cfg.Target
	if cfg.RemoteHost != "" {
		target = cfg.RemoteHost
	}
	log.Printf("session %s: forwarding localhost:%s -> %s:%s", session.ID, cfg.LocalPort, target, cfg.RemotePort)

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
