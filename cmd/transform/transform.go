package transform

import (
	"github.com/konveyor/crane/cmd/transform/listplugins"
	"github.com/konveyor/crane/cmd/transform/optionals"
	"github.com/konveyor/crane/internal/flags"
	"github.com/spf13/cobra"
)

func NewTransformCommand(f *flags.GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transform",
		Short: "Transform command group for plugin management utilities",
		Long: `Transform command group provides plugin-related subcommands:
  - list-plugins: List available transformation plugins
  - optionals: Show optional fields accepted by plugins

Note: The main transformation commands are now available as top-level commands:
  - crane transform-prepare: Create transformation patches for exported resources
  - crane transform-apply: Apply transformations to generate output manifests`,
	}

	// Add subcommands
	cmd.AddCommand(optionals.NewOptionalsCommand(f))
	cmd.AddCommand(listplugins.NewListPluginsCommand(f))

	return cmd
}
