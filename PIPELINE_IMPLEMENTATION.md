# Multi-Stage Pipeline Implementation

## Overview

This implementation extends the Kustomize-only workflow with a **multi-stage pipeline** where each plugin executes as a separate, isolated stage with its own Kustomize overlay and rendered output.

### Key Concepts

1. **Stage** = One plugin execution with isolated artifacts
2. **Pipeline** = Ordered sequence of stages
3. **Chaining** = Each stage consumes previous stage's rendered output
4. **Deterministic Ordering** = Stages sorted by order, then by plugin name

## Pipeline Directory Structure

```
transform/
  pipeline.yaml                 # Pipeline definition
  stages/
    10_KubernetesPlugin/       # Stage 1 (builtin sanitization)
      kustomization.yaml
      patches/
        *.patch.yaml
      resources/               # Only in first stage
        *.yaml
      reports/                 # Optional
        ignored-patches.json
      whiteouts/              # Optional
        whiteouts.json
      rendered.yaml           # Stage output
    20_OpenShiftPlugin/       # Stage 2 (example)
      kustomization.yaml
      patches/
        *.patch.yaml
      rendered.yaml
    30_ImageStreamPlugin/     # Stage 3 (example)
      kustomization.yaml
      patches/
        *.patch.yaml
      rendered.yaml
  final/
    rendered.yaml            # Copy of last stage output
```

## Stage Naming Convention

Format: `<order>_<pluginName>[:<comment>]`

Examples:
- `10_KubernetesPlugin`
- `20_OpenShiftPlugin:route_adjustments`
- `30_ImageStreamPlugin:registry_rewrite`

Components:
- **Order**: Numeric value (lower = earlier execution)
- **Plugin Name**: Sanitized plugin name
- **Comment**: Optional user-friendly description

## Order Assignment

### Default Behavior

1. **Kubernetes plugin**: Always Order `10` (first stage)
2. **Other plugins**: Auto-assigned in increments of `10` (20, 30, 40...)
3. **Sorting**: Primary by Order (ascending), secondary by plugin name (alphabetical)

### Order Overrides

Use `--plugin-priorities` flag:
```bash
crane transform --plugin-priorities KubernetesPlugin,OpenShiftPlugin,CustomPlugin
```

Maps to priorities:
- KubernetesPlugin: 0 (index 0)
- OpenShiftPlugin: 1 (index 1)
- CustomPlugin: 2 (index 2)

## Stage Chaining

### First Stage (e.g., `10_KubernetesPlugin`)

**Input**: Original export files (copied to `resources/`)

**Kustomization**:
```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - resources/apps_v1_deployment_nginx.yaml
  - resources/v1_service_nginx.yaml
patches:
  - path: patches/default--core-v1--Service--nginx.patch.yaml
    target:
      kind: Service
      name: nginx
      namespace: default
```

**Output**: `rendered.yaml` (result of `kubectl kustomize`)

### Subsequent Stages (e.g., `20_OpenShiftPlugin`)

**Input**: Previous stage's `rendered.yaml`

**Kustomization**:
```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../../10_KubernetesPlugin/rendered.yaml
patches:
  - path: patches/default--route-openshift-io-v1--Route--app.patch.yaml
    target:
      group: route.openshift.io
      version: v1
      kind: Route
      name: app
```

**Output**: `rendered.yaml` (cumulative transformations)

## Pipeline Manifest (`pipeline.yaml`)

```yaml
Stages:
- Comment: ""
  Enabled: true
  ID: 10_KubernetesPlugin
  Plugin: KubernetesPlugin
  Order: 10
  Required: false
- Comment: route_adjustments
  Enabled: true
  ID: 20_OpenShiftPlugin:route_adjustments
  Plugin: OpenShiftPlugin
  Order: 20
  Required: false
```

Fields:
- **ID**: Stage identifier (used in directory names)
- **Plugin**: Plugin name (must match plugin metadata)
- **Order**: Execution order
- **Required**: Whether stage must succeed
- **Enabled**: Whether stage should execute
- **Comment**: Optional description

## CLI Usage

### Basic Transform (All Stages)

```bash
crane transform --export-dir export --transform-dir transform
```

Executes full pipeline with all plugins.

### List Stages

```bash
crane transform --export-dir export --list-stages
```

Output:
```
Pipeline Stages:
================

1. Stage ID: 10_KubernetesPlugin
   Plugin: KubernetesPlugin
   Order: 10
   Required: false
   Enabled: true

2. Stage ID: 20_OpenShiftPlugin
   Plugin: OpenShiftPlugin
   Order: 20
   Required: false
   Enabled: true
```

### Apply with Pipeline

```bash
crane apply --transform-dir transform
```

Automatically detects pipeline structure and uses `final/rendered.yaml`.

### Apply with Output File

```bash
crane apply --transform-dir transform --output-dir output
```

Writes to `output/all.yaml`.

## Stage Execution Flow

1. **Build Pipeline**
   - Collect all plugins
   - Assign priorities (defaults + overrides)
   - Sort stages by Order
   - Generate stage IDs

2. **Execute Each Stage**
   - Create stage directory: `stages/<stage-id>/`
   - Run plugin for all resources
   - Generate patches: `patches/*.patch.yaml`
   - Generate reports (if any): `reports/`, `whiteouts/`
   - Create `kustomization.yaml`:
     - First stage: reference `resources/`
     - Subsequent: reference previous `rendered.yaml`
   - Render stage: `kubectl kustomize stages/<stage-id>/ > rendered.yaml`

3. **Finalize Pipeline**
   - Write `pipeline.yaml`
   - Copy last stage output to `final/rendered.yaml`

## Per-Stage Artifacts

Each stage directory contains:

### `kustomization.yaml`
Kustomize overlay configuration for this stage.

### `patches/*.patch.yaml`
JSON6902 patches generated by this stage's plugin.

### `resources/` (first stage only)
Copies of original export files.

### `rendered.yaml`
Output of `kubectl kustomize` for this stage (input for next stage).

### `reports/` (optional)
- `ignored-patches.json`: Conflicts resolved by Order

### `whiteouts/` (optional)
- `whiteouts.json`: Resources excluded from output

## Benefits

### Traceability
- Each plugin's changes isolated in its own stage
- Easy to inspect what each plugin does
- Clear provenance of transformations

### Debugging
- Run pipeline up to specific stage
- Inspect intermediate outputs
- Identify which stage causes issues

### Selective Execution
- Re-run single stage
- Skip problematic stages
- Test stage in isolation

### CI/CD Integration
- Stage-by-stage validation
- Parallel stage testing (future)
- Incremental migration workflows

## Implementation Details

### Stage Rendering

Each stage is rendered using `kubectl kustomize`:
```go
cmd := exec.Command("kubectl", "kustomize", stageDir)
output, err := cmd.CombinedOutput()
```

Rendered output is written to `rendered.yaml` for the next stage to consume.

### Resource Copying (First Stage Only)

First stage copies export files to `resources/`:
```go
resourcesDir := filepath.Join(stageDir, "resources")
// Copy each export file
os.WriteFile(filepath.Join(resourcesDir, fileName), data, 0644)
```

Subsequent stages don't copy - they reference previous `rendered.yaml`.

### Stage Kustomization Generation

```go
// First stage
kustomize.GenerateStageKustomization(artifacts, resourcePaths, true, "")

// Subsequent stages
relPath := "../../10_KubernetesPlugin/rendered.yaml"
kustomize.GenerateStageKustomization(artifacts, nil, false, relPath)
```

## Kubernetes Plugin Special Handling

The built-in Kubernetes plugin:
- Always executes first (Order 10)
- Provides default sanitization (removes clusterIP, etc.)
- Cannot be skipped in current implementation
- Acts as foundation for other plugins

## Future Enhancements (Not Implemented)

Potential additions for future iterations:

### Stage Selection
- `--stage <stage-id>`: Run single stage
- `--from-stage <stage-id>`: Run from stage to end
- `--to-stage <stage-id>`: Run from start to stage
- `--stages <id1,id2>`: Run specific stages

### Pipeline Configuration File
```yaml
# transform-pipeline.yaml
defaultStageOrder: 10
plugins:
  KubernetesPlugin:
    Order: 10
    comment: default_cleanup
    enabled: true
  OpenShiftPlugin:
    Order: 20
    comment: route_adjustments
    enabled: true
```

### Resume Support
```bash
crane transform --resume
```
Continue from first incomplete stage.

### Advanced Conflict Resolution
- Per-path conflict policies
- Stage-level Order overrides
- Conflict reporting dashboards

## Migration from Single-Stage

### Old Structure (Single Kustomization)
```
transform/
  kustomization.yaml
  resources/
  patches/
```

### New Structure (Pipeline)
```
transform/
  pipeline.yaml
  stages/
    10_KubernetesPlugin/
      kustomization.yaml
      resources/
      patches/
      rendered.yaml
  final/
    rendered.yaml
```

### Migration Steps

1. **No action needed** - Pipeline is default mode
2. Old workflows automatically use new structure
3. `crane apply` detects pipeline vs legacy

## Testing

### Unit Tests

```bash
cd crane-lib
go test ./transform/kustomize -run TestPipeline -v
```

Tests cover:
- Pipeline building
- Stage ID generation
- Order sorting
- Serialization/deserialization

### Integration Test

```bash
cd crane
crane transform --export-dir test-data/export --transform-dir test-data/transform
ls -R test-data/transform/stages/
cat test-data/transform/pipeline.yaml
crane apply --transform-dir test-data/transform
```

## Troubleshooting

### Issue: Stage rendering fails

**Check**: Does `kubectl` work?
```bash
kubectl kustomize transform/stages/10_KubernetesPlugin/
```

### Issue: Missing resources in stage

**Check**: First stage should have `resources/` directory
```bash
ls transform/stages/10_KubernetesPlugin/resources/
```

### Issue: Subsequent stages empty

**Check**: Previous stage's `rendered.yaml` exists
```bash
cat transform/stages/10_KubernetesPlugin/rendered.yaml
```

### Issue: Wrong stage order

**Check**: `pipeline.yaml` priorities
```bash
grep -A5 "Order:" transform/pipeline.yaml
```

## References

- Base Implementation: `KUSTOMIZE_IMPLEMENTATION.md`
- RFC Draft: `move-crane/drafts/transform-kustomize-poc/crane-transform-apply-stepwise-plugin-pipeline-draft.md`
- CLI Spec: `move-crane/drafts/transform-kustomize-poc/crane-stepwise-pipeline-cli-spec-draft.md`
