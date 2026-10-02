// Package conduit runs the port-forwarding application.
package conduit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"conduit/internal/aws"
	"conduit/internal/browser"
	"conduit/internal/config"
	"conduit/internal/socat"

	"github.com/creack/pty"
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
	ensureSocat    func(context.Context, sessionClient, string, string, string) error
	stopSocat      func(context.Context, sessionClient, string, string) error
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
		ensureSocat:    ensureSocat,
		stopSocat:      stopSocat,
		wait:           waitForReconnect,
	}
}

func runWithContext(ctx context.Context, cfg config.Config, deps dependencies) error {
	client, err := deps.newClient(ctx, cfg.Profile, cfg.Region)
	if err != nil {
		return err
	}
	var socatReady bool
	defer func() {
		if !socatReady {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = deps.stopSocat(cleanupCtx, client, cfg.Target, cfg.RemotePort)
	}()

	parameters := map[string][]string{
		"portNumber":      {cfg.RemotePort},
		"localPortNumber": {cfg.LocalPort},
	}
	params := aws.SessionParams{
		Target:     cfg.Target,
		Document:   cfg.Document,
		Parameters: parameters,
	}

	openURL := func(url string) error { return deps.openURL(url, cfg.ChromeProfile) }

	for {
		if ctx.Err() != nil {
			return nil
		}

		if err := deps.ensureSSOLogin(ctx, cfg.Profile, cfg.Region, openURL); err != nil {
			return fmt.Errorf("SSO login: %w", err)
		}
		if err := deps.ensureSocat(ctx, client, cfg.Target, cfg.Host, cfg.RemotePort); err != nil {
			return fmt.Errorf("ensuring socat: %w", err)
		}
		socatReady = true

		err := deps.runSession(ctx, client, params, cfg)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Println(err.Error())
		}

		if ctx.Err() != nil {
			return nil
		}
		if !cfg.Reconnect {
			return nil
		}

		if cfg.Debug {
			log.Printf("session ended, reconnecting in %dms...", cfg.ReconnectMS)
		}
		if err := deps.wait(ctx, time.Duration(cfg.ReconnectMS)*time.Millisecond); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
	}
}

func ensureSocat(ctx context.Context, client sessionClient, target, host, port string) error {
	return socat.NewManager(commandRunner{client: client}).Ensure(ctx, target, host, port)
}

func stopSocat(ctx context.Context, client sessionClient, target, port string) error {
	return socat.NewManager(commandRunner{client: client}).Stop(ctx, target, port)
}

type commandRunner struct {
	client sessionClient
}

func (r commandRunner) Run(ctx context.Context, target, command string) (string, error) {
	session, err := r.client.StartSession(ctx, aws.SessionParams{
		Target:   target,
		Document: aws.InteractiveCommandDocument,
		Parameters: map[string][]string{
			"command": {"bash -l"},
		},
	})
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, aws.PluginBinary, r.client.PluginArgs(session)...)
	terminal, err := pty.Start(cmd)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("%s not found in PATH; install it with: brew install --cask session-manager-plugin", aws.PluginBinary)
		}
		return "", fmt.Errorf("starting interactive socat setup session: %w", err)
	}
	defer terminal.Close()

	var output bytes.Buffer
	outputDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&output, terminal)
		close(outputDone)
	}()

	select {
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		<-outputDone
		return "", ctx.Err()
	case <-time.After(time.Second):
	}

	if _, err := io.WriteString(terminal, command+"\nexit\n"); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		<-outputDone
		return "", fmt.Errorf("sending socat setup command: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		<-outputDone
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("%s not found in PATH; install it with: brew install --cask session-manager-plugin", aws.PluginBinary)
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return output.String(), fmt.Errorf("running socat setup session: %w", err)
	}
	<-outputDone

	return output.String(), nil
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

	if cfg.Debug {
		log.Printf("forwarding localhost:%s -> %s:%s", cfg.LocalPort, cfg.Target, cfg.RemotePort)
	}

	cmd := exec.CommandContext(ctx, aws.PluginBinary, client.PluginArgs(session)...)
	cmd.Stdin = os.Stdin
	pluginLog := newLineLogWriter(func(line string) {
		if cfg.Debug {
			log.Print(line)
			return
		}
		if normalLine := normalPluginLogLine(line); normalLine != "" {
			log.Print(normalLine)
		}
	})
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

func normalPluginLogLine(line string) string {
	if line == "Waiting for connections..." {
		return line
	}
	if strings.HasPrefix(line, "Connection accepted for session") {
		return "Connection accepted"
	}

	lowercaseLine := strings.ToLower(line)
	if strings.Contains(lowercaseLine, "error") ||
		strings.Contains(lowercaseLine, "failed") ||
		strings.Contains(lowercaseLine, "denied") ||
		strings.Contains(lowercaseLine, "unable") {
		return line
	}

	return ""
}
