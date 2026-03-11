# Kustomize-Only Workflow Implementation

## Overview

This implementation migrates `crane transform` and `crane apply` to a **Kustomize-only workflow**.

### Key Changes

1. **`crane transform`** now generates Kustomize overlay artifacts instead of JSONPatch files
2. **`crane apply`** uses `kubectl kustomize` for rendering instead of in-process JSONPatch application
3. Existing plugins remain compatible (they continue to return JSONPatch operations)

## New Directory Layout

After running `crane transform`, the following structure is created:

```
transform/
  kustomization.yaml          # Main Kustomize file
  resources/                  # Copies of export resources
    *.yaml
  patches/                    # JSON6902 patch files
    <namespace>--<group>-<version>--<kind>--<name>.patch.yaml
  reports/                    # Optional conflict reports
    ignored-patches.json
  whiteouts/                  # Optional whiteout tracking
    whiteouts.json
```

## Patch File Naming Convention

Pattern: `<namespace-or-_cluster>--<group-or-core>-<version>--<kind>--<name>.patch.yaml`

Examples:
- `default--core-v1--Service--nginx.patch.yaml`
- `_cluster--core-v1--Namespace--test-ns.patch.yaml`
- `production--apps-v1--Deployment--myapp.patch.yaml`
- `myns--route-openshift-io-v1--Route--frontend.patch.yaml`

## Implementation Details

### crane-lib Changes

New package: `transform/kustomize/`
- `types.go` - Data models for Kustomize artifacts
- `target.go` - Target derivation from unstructured objects
- `naming.go` - Deterministic patch file naming
- `serializer.go` - JSONPatch to YAML serialization
- `kustomization.go` - Kustomization file generation

Extended `transform/runner.go`:
- New method `RunForKustomize()` returns structured `TransformArtifact`
- Old `Run()` method remains for backward compatibility (deprecated)

### crane Changes

`cmd/transform/transform.go`:
- Removed JSONPatch file generation
- Added Kustomize overlay generation
- Resource files are copied to `transform/resources/`
- Patches written to `transform/patches/`
- Optional reports for whiteouts and ignored patches

`cmd/apply/apply.go`:
- Removed in-process JSONPatch application
- Now executes `kubectl kustomize <transform-dir>`
- Outputs to stdout or `--output-dir`

## Breaking Changes

1. **Transform output format changed** - no longer generates `transform-*` JSONPatch files
2. **Apply requires kubectl** - `kubectl` must be in PATH
3. **Export-dir no longer used by apply** - only transform-dir is needed

## Usage Examples

### Basic Workflow

```bash
# Transform (same as before)
crane transform --export-dir export --transform-dir transform

# Apply to stdout
crane apply --transform-dir transform

# Apply to file
crane apply --transform-dir transform --output-dir output
```

### Verify Kustomize Overlay

```bash
# Test rendering manually
kubectl kustomize transform
```

### Inspect Generated Artifacts

```bash
# View kustomization
cat transform/kustomization.yaml

# View patches
ls transform/patches/

# View reports (if any)
cat transform/reports/ignored-patches.json
cat transform/whiteouts/whiteouts.json
```

## Testing

### Unit Tests

Located in `crane-lib/transform/kustomize/*_test.go`:
- `target_test.go` - Target derivation tests
- `naming_test.go` - Patch file naming tests
- `serializer_test.go` - YAML serialization tests

Run tests:
```bash
cd crane-lib
go test ./transform/kustomize/... -v
```

### Integration Tests

Basic end-to-end test:
```bash
cd crane
mkdir -p test-data/export/default
# Create test resources in test-data/export/
./crane-new transform --export-dir test-data/export --transform-dir test-data/transform
kubectl kustomize test-data/transform
./crane-new apply --transform-dir test-data/transform
```

## Plugin Compatibility

Existing plugins require **no changes**:
- Plugins continue to return `jsonpatch.Patch` operations
- `crane transform` converts these to Kustomize-compatible patch files
- Conflict resolution and priority rules remain unchanged

## Resource Path Handling

Resources are **copied** to `transform/resources/` directory to satisfy kubectl kustomize security restrictions (files must be within or below the kustomization root).

Alternative approaches considered:
- Symlinks - rejected due to kubectl security checks
- Direct references to `../export/` - rejected due to kubectl path restrictions

## Reports

### Whiteout Report (`whiteouts/whiteouts.json`)

Example:
```json
[
  {
    "apiVersion": "route.openshift.io/v1",
    "kind": "Route",
    "name": "frontend",
    "namespace": "ns1",
    "requestedBy": ["OpenShiftPlugin"]
  }
]
```

### Ignored Patches Report (`reports/ignored-patches.json`)

Example:
```json
[
  {
    "resource": {
      "apiVersion": "apps/v1",
      "kind": "Deployment",
      "name": "myapp",
      "namespace": "ns1"
    },
    "path": "/spec/template/spec/containers/0/image",
    "selectedPlugin": "OpenShiftPlugin",
    "ignoredPlugin": "ImageStreamPlugin",
    "reason": "path-conflict-priority"
  }
]
```

## Future Enhancements

Potential improvements (not in current scope):
- Stage-aware pipeline (separate stage per plugin)
- Selective stage execution
- Pipeline configuration files
- Advanced conflict resolution policies

## Migration Notes

For existing crane users:
1. Delete old `transform/` directory before running new version
2. Old `transform-*` JSONPatch files are no longer generated
3. `crane apply` no longer needs `--export-dir` flag
4. Ensure `kubectl` is installed and in PATH

## References

- RFC: `move-crane/drafts/transform-kustomize-poc/crane-kustomize-ondisk-layout-rfc-draft.md`
- Implementation Plan: `move-crane/drafts/transform-kustomize-poc/crane-transform-apply-kustomize-implementation-plan.md`
- Task Breakdown: `move-crane/drafts/transform-kustomize-poc/crane-kustomize-migration-task-breakdown.md`
