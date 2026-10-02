// Package version defines the version subcommand.
package version

import "github.com/spf13/cobra"

// VERSION is set at release time through Go linker flags.
var VERSION string

// NewCommand creates the version command.
func NewCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version number",
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Println(VERSION)
		},
	}
}
