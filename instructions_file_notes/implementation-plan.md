# Implementation Plan: Migration YAML File Support

## Overview
Add support for `crane transform` to accept a YAML configuration file that defines transform parameters and multi-stage pipeline configuration.

## Goals
- Enable declarative configuration of transform operations via YAML file
- Support both minimal and full configuration formats
- Maintain backward compatibility with existing CLI flags
- CLI flags should override YAML file values

## File Format

### Minimal Format
```yaml
transform:
  stages:
    - name: "10_KubernetesPlugin"
      type: "plugin"
```

### Full Format
```yaml
transform:
  exportDir: "export"
  transformDir: "transform"
  ignoredPatchesDir: ""
  pluginDir: "~/.local/share/crane/plugins"
  optionalFlags: ""
  kustomizeArgs: ""
  force: false
  
  stages:
    - name: "10_KubernetesPlugin"
      type: "plugin"
      description: "Apply default Kubernetes transformations"
      autoRegenerate: true
    - name: "50_CustomLabels"
      type: "custom"
      description: "Add custom labels"
      autoRegenerate: false
```

## Implementation Steps

### 1. Define YAML Structure
**File:** `internal/transform/config.go` (new file)

- [ ] Create `TransformConfig` struct to represent the YAML structure
- [ ] Create `StageConfig` struct for individual stage definitions
- [ ] Add YAML tags for proper unmarshaling
- [ ] Add validation methods

```go
type TransformConfig struct {
    ExportDir         string        `yaml:"exportDir,omitempty"`
    TransformDir      string        `yaml:"transformDir,omitempty"`
    IgnoredPatchesDir string        `yaml:"ignoredPatchesDir,omitempty"`
    PluginDir         string        `yaml:"pluginDir,omitempty"`
    OptionalFlags     string        `yaml:"optionalFlags,omitempty"`
    KustomizeArgs     string        `yaml:"kustomizeArgs,omitempty"`
    Force             bool          `yaml:"force,omitempty"`
    Stages            []StageConfig `yaml:"stages,omitempty"`
}

type StageConfig struct {
    Name            string `yaml:"name"`
    Type            string `yaml:"type"` // "plugin" or "custom"
    Description     string `yaml:"description,omitempty"`
    AutoRegenerate  bool   `yaml:"autoRegenerate,omitempty"`
}
```

### 2. Add Config File Loading
**File:** `internal/transform/config.go`

- [ ] Implement `LoadConfig(path string) (*TransformConfig, error)` function
- [ ] Handle YAML parsing with `gopkg.in/yaml.v3`
- [ ] Implement config validation
- [ ] Support both absolute and relative paths
- [ ] Handle missing/optional fields with defaults

### 3. Update CLI Command
**File:** `cmd/transform/transform.go`

- [ ] Add `--config` flag to accept config file path
  ```go
  cmd.Flags().StringVar(&o.ConfigFile, "config", "", "Path to YAML configuration file")
  ```
- [ ] Load config file if provided in `PreRun` or early in `Run()`
- [ ] Merge config file values with CLI flags (CLI flags take precedence)
- [ ] Update `Flags` struct if needed

### 4. Implement Config Merging Logic
**File:** `cmd/transform/transform.go`

- [ ] Create `mergeConfigWithFlags()` method
- [ ] Priority order: CLI flags > YAML config > defaults
- [ ] Handle special cases (empty strings, zero values)
- [ ] Ensure viper compatibility

```go
func (o *Options) mergeConfigWithFlags(config *internalTransform.TransformConfig) {
    // CLI flags override config file
    if o.ExportDir == "export" && config.ExportDir != "" {
        o.ExportDir = config.ExportDir
    }
    // ... repeat for other fields
}
```

### 5. Integrate Stages from Config
**File:** `cmd/transform/transform.go` and `internal/transform/orchestrator.go`

When config file with stages is provided, the behavior changes:

- [ ] Add `CreateStagesFromConfig()` function to create all stage directories at once
- [ ] Create all stage directories before running any transforms
- [ ] Mark all newly created stages in `NewlyCreatedStages` map
- [ ] For plugin stages (`type: "plugin"`), mark for auto-regeneration
- [ ] For custom stages (`type: "custom"`), create empty directory only
- [ ] Validate all stage names follow convention before creating any
- [ ] Use config-defined stages instead of `DiscoverStages()` when config is provided

**Implementation approach:**

```go
// In cmd/transform/transform.go
func (o *Options) createStagesFromConfig(config *internalTransform.TransformConfig, orchestrator *internalTransform.Orchestrator) error {
    // Validate all stage names first
    for _, stageConfig := range config.Stages {
        if err := internalTransform.ValidateStageName(stageConfig.Name); err != nil {
            return err
        }
    }
    
    // Create all stage directories
    for _, stageConfig := range config.Stages {
        stageDir := filepath.Join(o.TransformDir, stageConfig.Name)
        
        // Check if stage already exists
        _, err := os.Stat(stageDir)
        if err == nil {
            // Stage exists - skip creation unless force flag
            if !o.Force && stageConfig.Type == "custom" {
                log.Infof("Stage %s already exists, skipping creation", stageConfig.Name)
                continue
            }
        }
        
        // Create stage directory
        if err := os.MkdirAll(stageDir, 0700); err != nil {
            return fmt.Errorf("failed to create stage directory %s: %w", stageConfig.Name, err)
        }
        
        // Mark as newly created so orchestrator can populate it
        orchestrator.NewlyCreatedStages[stageConfig.Name] = true
        
        log.Infof("Created stage directory: %s (type: %s)", stageConfig.Name, stageConfig.Type)
    }
    
    return nil
}
```

**Execution flow with config file:**

1. Load config file
2. Merge with CLI flags
3. Validate all stages in config
4. **Create all stage directories at once** (new step)
5. Run multi-stage pipeline on all created stages
6. Each stage gets populated during execution:
   - Plugin stages: auto-generated with plugin output
   - Custom stages: empty directory for manual customization

**Key difference from current behavior:**
- Current: stages created one-by-one on demand
- With config: all stages created upfront, then executed sequentially

This allows users to define entire pipeline in YAML and have it created in one command run.

### 6. Add Validation
**File:** `internal/transform/config.go`

- [ ] Validate stage names (format: `[number]_[Name][Plugin]?`)
- [ ] Ensure stage types are valid ("plugin" or "custom")
- [ ] Check for duplicate stage names
- [ ] Validate paths exist or can be created
- [ ] Validate kustomizeArgs format
- [ ] Validate optionalFlags JSON format

```go
func (c *TransformConfig) Validate() error {
    // Validate stages
    stageNames := make(map[string]bool)
    for _, stage := range c.Stages {
        if stage.Name == "" {
            return fmt.Errorf("stage name cannot be empty")
        }
        if stageNames[stage.Name] {
            return fmt.Errorf("duplicate stage name: %s", stage.Name)
        }
        stageNames[stage.Name] = true
        
        if stage.Type != "plugin" && stage.Type != "custom" {
            return fmt.Errorf("invalid stage type: %s (must be 'plugin' or 'custom')", stage.Type)
        }
    }
    return nil
}
```

### 7. Update Tests
**Files:** Various test files

- [ ] Add unit tests for config loading (`internal/transform/config_test.go`)
- [ ] Test YAML parsing with valid/invalid files
- [ ] Test config validation
- [ ] Test flag merging logic
- [ ] Add integration test with sample config files
- [ ] Test backward compatibility (no config file)

### 8. Documentation
**Files:** `docs/` directory

- [ ] Update `crane transform --help` text
- [ ] Create example config files in `docs/examples/`
- [ ] Add section to existing transform documentation
- [ ] Document precedence rules (CLI > YAML > defaults)
- [ ] Add troubleshooting section

## Interaction with `crane apply`

**Important:** `crane apply` does NOT need the config file.

After running `crane transform --config migration.yaml`:
1. All stages are created in `transform/` directory
2. Each stage has its own `kustomization.yaml`
3. `crane apply` automatically discovers stages via `DiscoverStages()`
4. `crane apply` runs `kubectl kustomize` on the final stage
5. Output is written to `output/output.yaml`

**Workflow:**
```bash
# Step 1: Create multi-stage pipeline from config
crane transform --config migration.yaml

# Step 2: Apply transformations (no config needed)
crane apply

# The apply command:
# - Discovers stages in transform/
# - Applies final stage (highest priority)
# - Generates output/output.yaml
```

**Why apply doesn't need config:**
- Stages already exist in filesystem after transform
- Each stage has complete kustomization.yaml
- Stage discovery is filesystem-based (DiscoverStages)
- No additional metadata needed beyond what's in stage directories

**Config file is only for `crane transform`** - it defines what to create and how to configure the transformation process.

## Backward Compatibility

- If no `--config` flag is provided, crane works exactly as before
- All existing CLI flags continue to work
- CLI flags override config file values
- No breaking changes to existing workflows

## Testing Strategy

1. **Unit Tests:**
   - Config parsing (valid/invalid YAML)
   - Config validation
   - Flag merging logic

2. **Integration Tests:**
   - Run transform with minimal config
   - Run transform with full config
   - CLI flags override config values
   - Missing config file error handling

3. **Manual Testing:**
   - Test with example config files
   - Verify backward compatibility
   - Test error messages

## Future Enhancements

- Support for environment variable expansion in config (e.g., `${HOME}`)
- Config file auto-discovery (e.g., `.crane.yaml` in current directory)
- Multiple config file support with merging
- JSON format support alongside YAML
- Schema validation with JSON Schema
- Interactive config file generation (`crane transform init-config`)

## Dependencies

- `gopkg.in/yaml.v3` - YAML parsing (likely already a dependency)
- No new external dependencies required

## Estimated Effort

- Config structure and loading: 2-3 hours
- CLI integration and merging: 3-4 hours
- Stage integration: 2-3 hours
- Validation: 2 hours
- Testing: 4-5 hours
- Documentation: 2 hours

**Total: ~15-20 hours**

## Multi-Stage Creation Flow

When user runs `crane transform --config migration.yaml`:

1. **Pre-flight validation**
   - Parse YAML config
   - Validate all stage names and types
   - Check for duplicates
   - Fail fast if any validation errors

2. **Stage directory creation** (all at once)
   - Create all stage directories defined in config
   - Mark all as newly created in `NewlyCreatedStages` map
   - Skip existing stages unless `--force` flag set

3. **Sequential execution**
   - Stage 1: Read from `export/`, write to `transform/10_KubernetesPlugin/.work/output/`
   - Stage 2: Read from `transform/10_KubernetesPlugin/.work/output/`, write to `transform/50_CustomLabels/.work/output/`
   - Stage N: ...

4. **Result**
   - All stages have been created and populated
   - Pipeline ready for subsequent runs or manual customization

**Example:**
```bash
# User has migration.yaml with 2 stages
crane transform --config migration.yaml

# Output:
# Created stage directory: 10_KubernetesPlugin (type: plugin)
# Created stage directory: 50_CustomLabels (type: custom)
# Executing stage 1/2: 10_KubernetesPlugin
# ... plugin runs, generates patches ...
# Executing stage 2/2: 50_CustomLabels
# ... empty stage, no patches generated ...
# Transform complete. 2 stages created.
```

## Questions/Decisions Needed

1. Should config file support environment variable expansion?
2. Should we auto-discover config files (e.g., `.crane.yaml`) or require explicit `--config` flag?
3. Should we support JSON format in addition to YAML?
4. Should we validate that plugin stages end with "Plugin" suffix?
5. What should happen if config defines stages but stage directories already exist?
   - **Proposed**: Skip creation for custom stages, auto-regenerate plugin stages (current behavior)
6. Should custom stages with `type: "custom"` run any plugins or just create empty directory?
   - **Proposed**: Create empty directory only, user adds patches manually later
