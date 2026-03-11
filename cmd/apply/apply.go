package apply

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/konveyor/crane/internal/flags"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type Options struct {
	// Two GlobalFlags struct fields are needed
	// 1. cobraGlobalFlags for explicit CLI args parsed by cobra
	// 2. globalFlags for the args merged with values from the viper config file
	cobraGlobalFlags *flags.GlobalFlags
	globalFlags      *flags.GlobalFlags
	// Two Flags struct fields are needed
	// 1. cobraFlags for explicit CLI args parsed by cobra
	// 2. Flags for the args merged with values from the viper config file
	cobraFlags Flags
	Flags
}

type Flags struct {
	TransformDir string `mapstructure:"transform-dir"`
	OutputDir    string `mapstructure:"output-dir"`
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	// TODO: @shawn-hurley
	return nil
}

func (o *Options) Validate() error {
	// TODO: @shawn-hurley
	return nil
}

func (o *Options) Run() error {
	return o.run()
}

func NewApplyCommand(f *flags.GlobalFlags) *cobra.Command {
	o := &Options{
		cobraGlobalFlags: f,
	}
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply the transformations to the exported resources and save results in an output directory",
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Complete(c, args); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			if err := o.Run(); err != nil {
				return err
			}

			return nil
		},
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlags(cmd.Flags())
			viper.Unmarshal(&o.Flags)
			viper.Unmarshal(&o.globalFlags)
		},
	}

	addFlagsForOptions(&o.cobraFlags, cmd)

	return cmd
}

func addFlagsForOptions(o *Flags, cmd *cobra.Command) {
	cmd.Flags().StringVarP(&o.TransformDir, "transform-dir", "t", "transform", "The path where the Kustomize overlay is located")
	cmd.Flags().StringVarP(&o.OutputDir, "output-dir", "o", "", "Optional path to save rendered manifests (if not specified, outputs to stdout)")
}

func (o *Options) run() error {
	log := o.globalFlags.GetLogger()

	transformDir, err := filepath.Abs(o.TransformDir)
	if err != nil {
		return err
	}

	// Validate that kustomization.yaml exists
	kustomizationPath := filepath.Join(transformDir, "kustomization.yaml")
	if _, err := os.Stat(kustomizationPath); os.IsNotExist(err) {
		return fmt.Errorf("kustomization.yaml not found in %s - please run 'crane transform' first", transformDir)
	}

	// Check if kubectl is available
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH - please install kubectl to use crane apply")
	}

	// Execute kubectl kustomize
	log.Infof("rendering Kustomize overlay from: %s", transformDir)
	cmd := exec.Command("kubectl", "kustomize", transformDir)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl kustomize failed: %w\nOutput: %s", err, string(output))
	}

	// Handle output
	if o.OutputDir != "" {
		outputDir, err := filepath.Abs(o.OutputDir)
		if err != nil {
			return err
		}

		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return err
		}

		outputPath := filepath.Join(outputDir, "all.yaml")
		if err := os.WriteFile(outputPath, output, 0644); err != nil {
			return err
		}

		log.Infof("rendered manifests written to: %s", outputPath)
	} else {
		// Output to stdout
		fmt.Print(string(output))
	}

	log.Infof("apply completed successfully")
	return nil
}
