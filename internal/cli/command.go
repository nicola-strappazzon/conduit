// Package cli defines Conduit's Cobra command.
package cli

import (
	"conduit/internal/cli/version"
	"conduit/internal/config"

	"github.com/spf13/cobra"
)

// RunFunc executes Conduit with a validated configuration.
type RunFunc func(config.Config) error

// NewRootCmd creates Conduit's root command.
func NewRootCmd(run RunFunc) *cobra.Command {
	opts := config.Defaults()
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
			if err := opts.Validate(); err != nil {
				return err
			}
			return run(opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Profile, "profile", opts.Profile, "AWS profile")
	flags.StringVar(&opts.Region, "region", opts.Region, "AWS region")
	flags.StringVar(&opts.Target, "target", opts.Target, "SSM instance ID (required)")
	flags.StringVar(&opts.Document, "document", opts.Document, "SSM document")
	flags.StringVar(&opts.RemoteHost, "remote-host", opts.RemoteHost, "Remote host")
	flags.StringVar(&opts.RemotePort, "remote-port", opts.RemotePort, "Remote port (required)")
	flags.StringVar(&opts.LocalPort, "local-port", opts.LocalPort, "Local port (required)")
	flags.BoolVar(&opts.Reconnect, "reconnect", opts.Reconnect, "Reconnect automatically")
	flags.IntVar(&opts.ReconnectMS, "reconnect-delay-ms", opts.ReconnectMS, "Reconnect delay (ms)")
	flags.StringVar(&opts.ChromeProfile, "chrome-profile", opts.ChromeProfile, "Chrome profile for SSO")
	cmd.AddCommand(version.NewCommand())

	return cmd
}
