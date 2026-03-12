package transform

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane-lib/transform"
	"github.com/konveyor/crane-lib/transform/kustomize"
	"github.com/konveyor/crane/cmd/transform/listplugins"
	"github.com/konveyor/crane/cmd/transform/optionals"
	"github.com/konveyor/crane/internal/file"
	"github.com/konveyor/crane/internal/flags"
	"github.com/konveyor/crane/internal/plugin"
	"github.com/sirupsen/logrus"
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
	ExportDir         string            `mapstructure:"export-dir"`
	PluginDir         string            `mapstructure:"plugin-dir"`
	TransformDir      string            `mapstructure:"transform-dir"`
	IgnoredPatchesDir string            `mapstructure:"ignored-patches-dir"`
	PluginPriorities  []string          `mapstructure:"plugin-priorities"`
	SkipPlugins       []string          `mapstructure:"skip-plugins"`
	OptionalFlags     string            `mapstructure:"optional-flags"`
	ListStages        bool              `mapstructure:"list-stages"`
	StageComments     map[string]string `mapstructure:"stage-comments"`
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	// TODO: @sseago
	return nil
}

func (o *Options) Validate() error {
	// TODO: @sseago
	return nil
}

func (o *Options) Run() error {
	return o.run()
}

func NewTransformCommand(f *flags.GlobalFlags) *cobra.Command {
	o := &Options{
		cobraGlobalFlags: f,
	}
	cmd := &cobra.Command{
		Use:   "transform",
		Short: "Create the transformations for the exported resources and plugins and save the results in a transform directory",
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
			viper.BindPFlags(cmd.PersistentFlags())
			viper.Unmarshal(&o.Flags)
			viper.Unmarshal(&o.globalFlags)
		},
	}

	addFlagsForOptions(&o.cobraFlags, cmd)
	cmd.AddCommand(optionals.NewOptionalsCommand(f))
	cmd.AddCommand(listplugins.NewListPluginsCommand(f))
	return cmd
}

func addFlagsForOptions(o *Flags, cmd *cobra.Command) {
	home := os.Getenv("HOME")
	defaultPluginDir := home + plugin.DefaultLocalPluginDir
	cmd.Flags().StringVarP(&o.ExportDir, "export-dir", "e", "export", "The path where the kubernetes resources are saved")
	cmd.Flags().StringVarP(&o.TransformDir, "transform-dir", "t", "transform", "The path where files that contain the transformations are saved")
	cmd.Flags().StringVar(&o.IgnoredPatchesDir, "ignored-patches-dir", "", "The path where files that contain transformations that were discarded due to conflicts are saved. If left blank, these files will not be saved.")
	cmd.Flags().StringSliceVar(&o.PluginPriorities, "plugin-priorities", nil, "A comma-separated list of plugin names. A plugin listed will take priority in the case of patch conflict over a plugin listed later in the list or over one not listed at all.")
	cmd.Flags().StringVar(&o.OptionalFlags, "optional-flags", "", "JSON string holding flag value pairs to be passed to all plugins ran in transform operation. (ie. '{\"foo-flag\": \"foo-a=/data,foo-b=/data\", \"bar-flag\": \"bar-value\"}')")
	cmd.Flags().BoolVar(&o.ListStages, "list-stages", false, "List the planned pipeline stages and exit without generating files")
	// These flags pass down to subcommands
	cmd.PersistentFlags().StringVarP(&o.PluginDir, "plugin-dir", "p", defaultPluginDir, "The path where binary plugins are located")
	cmd.PersistentFlags().StringSliceVarP(&o.SkipPlugins, "skip-plugins", "s", nil, "A comma-separated list of plugins to skip")

}

func (o *Options) run() error {
	log := o.globalFlags.GetLogger()
	// Load all the resources from the export dir
	exportDir, err := filepath.Abs(o.ExportDir)
	if err != nil {
		// Handle errors better for users.
		return err
	}

	pluginDir, err := filepath.Abs(o.PluginDir)
	if err != nil {
		return err
	}

	transformDir, err := filepath.Abs(o.TransformDir)
	if err != nil {
		return err
	}

	plugins, err := plugin.GetFilteredPlugins(pluginDir, o.SkipPlugins, log)
	if err != nil {
		return err
	}
	files, err := file.ReadFiles(context.TODO(), exportDir)
	if err != nil {
		return err
	}

	runner := transform.Runner{Log: log.WithField("command", "transform").Logger}
	if len(o.PluginPriorities) > 0 {
		runner.PluginPriorities = o.getPluginPrioritiesMap()
	}

	if len(o.OptionalFlags) > 0 {
		err = json.Unmarshal([]byte(o.OptionalFlags), &runner.OptionalFlags)
		if err != nil {
			return err
		}
		runner.OptionalFlags = optionalFlagsToLower(runner.OptionalFlags)
		log.Debugf("parsed optional-flags: %v", runner.OptionalFlags)
	}

	return o.runPipelineWorkflow(files, plugins, runner, exportDir, transformDir, log)
}

func (o *Options) getPluginPrioritiesMap() map[string]int {
	prioritiesMap := make(map[string]int)
	for i, pluginName := range o.PluginPriorities {
		if len(pluginName) > 0 {
			prioritiesMap[pluginName] = i
		}
	}
	return prioritiesMap
}

// Returns an extras map with lowercased keys, since any keys coming from the config file
// are lower-cased by viper
func optionalFlagsToLower(inFlags map[string]string) map[string]string {
	lowerMap := make(map[string]string)
	for key, val := range inFlags {
		lowerMap[strings.ToLower(key)] = val
	}
	return lowerMap
}

// runPipelineWorkflow executes the multi-stage pipeline transform workflow
func (o *Options) runPipelineWorkflow(files []file.File, plugins []transform.Plugin, runner transform.Runner, exportDir, transformDir string, log *logrus.Logger) error {
	// Build pipeline from plugins
	priorityMap := o.getPluginPrioritiesMap()

	// Extract plugin names
	pluginNames := make([]string, len(plugins))
	for i, p := range plugins {
		pluginNames[i] = p.Metadata().Name
	}

	pipeline, err := kustomize.BuildPipeline(pluginNames, priorityMap, o.StageComments)
	if err != nil {
		return fmt.Errorf("failed to build pipeline: %w", err)
	}

	// If --list-stages, print and exit
	if o.ListStages {
		fmt.Print(pipeline.ListStages())
		return nil
	}

	log.Infof("executing pipeline with %d stages", len(pipeline.Stages))

	// Create base transform directory structure
	stagesDir := filepath.Join(transformDir, "stages")
	if err := os.MkdirAll(stagesDir, 0755); err != nil {
		return err
	}

	// Execute each stage
	var prevRenderedPath string
	for stageIdx, stage := range pipeline.Stages {
		isFirstStage := stageIdx == 0

		log.Infof("executing stage %d/%d: %s (plugin: %s)", stageIdx+1, len(pipeline.Stages), stage.ID, stage.Plugin)

		// Find plugin for this stage
		var stagePlugin transform.Plugin
		for _, p := range plugins {
			if p.Metadata().Name == stage.Plugin {
				stagePlugin = p
				break
			}
		}
		if stagePlugin == nil {
			return fmt.Errorf("plugin not found for stage %s: %s", stage.ID, stage.Plugin)
		}

		// Run plugin for all resources
		stageArtifacts := []kustomize.TransformArtifact{}
		for _, f := range files {
			artifact, err := runner.RunForKustomize(f.Unstructured, []transform.Plugin{stagePlugin})
			if err != nil {
				log.Errorf("failed to transform resource %s in stage %s: %v", f.Info.Name(), stage.ID, err)
				return err
			}
			stageArtifacts = append(stageArtifacts, artifact)

			if artifact.IsWhiteOut {
				log.Debugf("[%s] resource %s/%s %s is whiteout", stage.ID, artifact.Target.Namespace, artifact.Target.Kind, artifact.Target.Name)
			}
		}

		// Create stage directory
		stageDir := filepath.Join(stagesDir, stage.ID)
		patchesDir := filepath.Join(stageDir, "patches")
		reportsDir := filepath.Join(stageDir, "reports")
		whiteoutsDir := filepath.Join(stageDir, "whiteouts")

		if err := os.MkdirAll(patchesDir, 0755); err != nil {
			return err
		}

		// Write patches for this stage
		for _, artifact := range stageArtifacts {
			if artifact.IsWhiteOut || len(artifact.Patches) == 0 {
				continue
			}

			patchFileName := kustomize.GeneratePatchFileName(artifact.Target)
			patchFilePath := filepath.Join(patchesDir, patchFileName)

			patchYAML, err := kustomize.SerializePatchToYAML(artifact.Patches)
			if err != nil {
				return err
			}

			if err := os.WriteFile(patchFilePath, patchYAML, 0644); err != nil {
				return err
			}

			log.Debugf("[%s] wrote patch: %s", stage.ID, patchFileName)
		}

		// Generate kustomization for this stage
		var kustomizationYAML []byte
		if isFirstStage {
			// First stage: copy resources and reference them
			resourcesDir := filepath.Join(stageDir, "resources")
			if err := os.MkdirAll(resourcesDir, 0755); err != nil {
				return err
			}

			resourcePathMap := make(map[string]string)
			for _, f := range files {
				target, err := kustomize.DeriveTarget(f.Unstructured)
				if err != nil {
					continue
				}
				key := fmt.Sprintf("%s/%s/%s/%s", target.Namespace, target.Group, target.Kind, target.Name)

				fileName := filepath.Base(f.Path)
				destPath := filepath.Join(resourcesDir, fileName)

				data, err := os.ReadFile(f.Path)
				if err != nil {
					return err
				}
				if err := os.WriteFile(destPath, data, 0644); err != nil {
					return err
				}

				resourcePathMap[key] = filepath.Join("resources", fileName)
			}

			kustomizationYAML, err = kustomize.GenerateStageKustomization(stageArtifacts, resourcePathMap, true, "")
		} else {
			// Subsequent stages: reference previous stage rendered output
			relPrevRendered, err := filepath.Rel(stageDir, prevRenderedPath)
			if err != nil {
				return err
			}
			kustomizationYAML, err = kustomize.GenerateStageKustomization(stageArtifacts, nil, false, relPrevRendered)
		}

		if err != nil {
			return err
		}

		kustomizationPath := filepath.Join(stageDir, "kustomization.yaml")
		if err := os.WriteFile(kustomizationPath, kustomizationYAML, 0644); err != nil {
			return err
		}

		// Generate stage reports
		whiteoutReport, err := kustomize.GenerateWhiteOutReport(stageArtifacts)
		if err != nil {
			return err
		}
		if whiteoutReport != nil {
			if err := os.MkdirAll(whiteoutsDir, 0755); err != nil {
				return err
			}
			whiteoutPath := filepath.Join(whiteoutsDir, "whiteouts.json")
			if err := os.WriteFile(whiteoutPath, whiteoutReport, 0644); err != nil {
				return err
			}
		}

		ignoredReport, err := kustomize.GenerateIgnoredPatchesReport(stageArtifacts)
		if err != nil {
			return err
		}
		if ignoredReport != nil {
			if err := os.MkdirAll(reportsDir, 0755); err != nil {
				return err
			}
			ignoredPath := filepath.Join(reportsDir, "ignored-patches.json")
			if err := os.WriteFile(ignoredPath, ignoredReport, 0644); err != nil {
				return err
			}
		}

		// Render this stage using kubectl kustomize
		renderedPath := filepath.Join(stageDir, "rendered.yaml")
		if err := renderStageWithKubectl(stageDir, renderedPath, log); err != nil {
			return fmt.Errorf("failed to render stage %s: %w", stage.ID, err)
		}

		prevRenderedPath = renderedPath
		log.Infof("completed stage %s", stage.ID)
	}

	// Write pipeline.yaml
	pipelineYAML, err := kustomize.SerializePipeline(pipeline)
	if err != nil {
		return err
	}

	pipelinePath := filepath.Join(transformDir, "pipeline.yaml")
	if err := os.WriteFile(pipelinePath, pipelineYAML, 0644); err != nil {
		return err
	}

	// Create final/ directory with symlink to last stage
	finalDir := filepath.Join(transformDir, "final")
	if err := os.MkdirAll(finalDir, 0755); err != nil {
		return err
	}

	finalRenderedPath := filepath.Join(finalDir, "rendered.yaml")
	os.Remove(finalRenderedPath) // Remove if exists

	// Copy instead of symlink for portability
	if prevRenderedPath != "" {
		data, err := os.ReadFile(prevRenderedPath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(finalRenderedPath, data, 0644); err != nil {
			return err
		}
	}

	log.Infof("pipeline completed successfully - generated in: %s", transformDir)
	log.Infof("final rendered output: %s", finalRenderedPath)
	return nil
}

// renderStageWithKubectl renders a stage kustomization using kubectl
func renderStageWithKubectl(stageDir, outputPath string, log *logrus.Logger) error {
	cmd := exec.Command("kubectl", "kustomize", stageDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl kustomize failed: %w\nOutput: %s", err, string(output))
	}

	if err := os.WriteFile(outputPath, output, 0644); err != nil {
		return err
	}

	log.Debugf("rendered stage to: %s", outputPath)
	return nil
}

// runKustomizeWorkflow is deprecated, kept for reference
func (o *Options) runKustomizeWorkflow(files []file.File, plugins []transform.Plugin, runner transform.Runner, exportDir, transformDir string, log *logrus.Logger) error {
	// Collect all artifacts
	artifacts := []kustomize.TransformArtifact{}

	for _, f := range files {
		artifact, err := runner.RunForKustomize(f.Unstructured, plugins)
		if err != nil {
			log.Errorf("failed to transform resource %s: %v", f.Info.Name(), err)
			return err
		}

		artifacts = append(artifacts, artifact)

		if artifact.IsWhiteOut {
			log.Infof("resource %s/%s %s is whiteout (requested by: %v)",
				artifact.Target.Namespace,
				artifact.Target.Kind,
				artifact.Target.Name,
				artifact.WhiteOutRequestedBy)
		}

		if len(artifact.IgnoredPatches) > 0 {
			log.Infof("resource %s/%s %s has %d ignored patches due to conflicts",
				artifact.Target.Namespace,
				artifact.Target.Kind,
				artifact.Target.Name,
				len(artifact.IgnoredPatches))
		}
	}

	// Create transform directory structure
	patchesDir := filepath.Join(transformDir, "patches")
	reportsDir := filepath.Join(transformDir, "reports")
	whiteoutsDir := filepath.Join(transformDir, "whiteouts")
	resourcesDir := filepath.Join(transformDir, "resources")

	if err := os.MkdirAll(patchesDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(resourcesDir, 0755); err != nil {
		return err
	}

	// Generate and write patch files
	for _, artifact := range artifacts {
		if artifact.IsWhiteOut || len(artifact.Patches) == 0 {
			continue
		}

		patchFileName := kustomize.GeneratePatchFileName(artifact.Target)
		patchFilePath := filepath.Join(patchesDir, patchFileName)

		patchYAML, err := kustomize.SerializePatchToYAML(artifact.Patches)
		if err != nil {
			return err
		}

		if err := os.WriteFile(patchFilePath, patchYAML, 0644); err != nil {
			return err
		}

		log.Debugf("wrote patch file: %s", patchFilePath)
	}

	// Build resource path map and copy resource files
	resourcePathMap := make(map[string]string)
	for _, f := range files {
		target, err := kustomize.DeriveTarget(f.Unstructured)
		if err != nil {
			continue
		}
		key := fmt.Sprintf("%s/%s/%s/%s", target.Namespace, target.Group, target.Kind, target.Name)

		// Copy resource file to resources dir
		fileName := filepath.Base(f.Path)
		destPath := filepath.Join(resourcesDir, fileName)

		data, err := os.ReadFile(f.Path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(destPath, data, 0644); err != nil {
			return err
		}

		resourcePathMap[key] = filepath.Join("resources", fileName)
		log.Debugf("copied resource: %s -> %s", f.Path, destPath)
	}

	// Generate kustomization.yaml
	kustomizationYAML, err := kustomize.GenerateKustomizationWithPaths(artifacts, resourcePathMap)
	if err != nil {
		return err
	}

	kustomizationPath := filepath.Join(transformDir, "kustomization.yaml")
	if err := os.WriteFile(kustomizationPath, kustomizationYAML, 0644); err != nil {
		return err
	}
	log.Infof("wrote kustomization.yaml: %s", kustomizationPath)

	// Generate whiteout report if needed
	whiteoutReport, err := kustomize.GenerateWhiteOutReport(artifacts)
	if err != nil {
		return err
	}
	if whiteoutReport != nil {
		if err := os.MkdirAll(whiteoutsDir, 0755); err != nil {
			return err
		}
		whiteoutPath := filepath.Join(whiteoutsDir, "whiteouts.json")
		if err := os.WriteFile(whiteoutPath, whiteoutReport, 0644); err != nil {
			return err
		}
		log.Infof("wrote whiteout report: %s", whiteoutPath)
	}

	// Generate ignored patches report if needed
	ignoredReport, err := kustomize.GenerateIgnoredPatchesReport(artifacts)
	if err != nil {
		return err
	}
	if ignoredReport != nil {
		if err := os.MkdirAll(reportsDir, 0755); err != nil {
			return err
		}
		ignoredPath := filepath.Join(reportsDir, "ignored-patches.json")
		if err := os.WriteFile(ignoredPath, ignoredReport, 0644); err != nil {
			return err
		}
		log.Infof("wrote ignored patches report: %s", ignoredPath)
	}

	log.Infof("transform completed successfully - generated Kustomize overlay in: %s", transformDir)
	return nil
}
