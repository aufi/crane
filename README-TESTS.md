# Crane Test Scripts - Quick Reference

Test scripts for the new Crane CLI migration workflow with `validate` command.

## Quick Start

```bash
# Full migration test with automatic import (minikube → CRC)
./test-migration.sh minikube crc-admin

# Dry-run (validation only, skip import)
./test-migration.sh --dry-run minikube crc-admin

# Quick validation only
./test-validate-only.sh crc-admin .crane-test/migration-test-*/output

# Cleanup test artifacts
./cleanup-tests.sh
```

## Available Scripts

| Script | Purpose | Usage |
|--------|---------|-------|
| `test-migration.sh` | Full end-to-end migration test with auto-import | `./test-migration.sh [--dry-run] <source> <target> [namespace]` |
| `test-validate-only.sh` | Quick validation test | `./test-validate-only.sh <target> <manifest-dir>` |
| `cleanup-tests.sh` | Remove test artifacts | `./cleanup-tests.sh` |

### test-migration.sh Modes

- **Default (auto-import)**: Validates and automatically imports to target if validation passes
- **--dry-run**: Validates only, skips import step

## Test Workflow

The migration test follows this workflow:

```
1. Export        → crane export
2. Transform     → crane transform-prepare
3. Apply         → crane transform-apply
4. Validate      → crane validate (NEW!)
5. Import        → kubectl apply (automatic if validation passes, skip with --dry-run)
```

## Output Structure

All test artifacts are stored in `.crane-test/` (git-ignored):

```
.crane-test/
├── migration-test-20260303-153804/
│   ├── export/          # Exported from source cluster
│   ├── transform/       # Transformation patches
│   ├── output/          # Ready-to-import manifests
│   ├── reports/
│   │   └── validation-report.json
│   └── migration-metadata.txt
└── validation-report-*.json  # Standalone validation reports
```

## Validation Command

The new `validate` command checks target cluster compatibility:

### Domains Checked

1. **Target Reachability & Authentication**
   - API server connectivity
   - Authentication
   - RBAC permissions

2. **API Compatibility**
   - Resource types (GVKs) existence
   - API version support
   - Deprecated API detection
   - Schema validation

### Exit Codes

- `0` = PASS (safe to migrate)
- `2` = UNRESOLVED (issues found)
- `4` = INPUT_ERROR
- `5` = CONNECTIVITY_ERROR

### Example Usage

```bash
# Validate output manifests
crane validate \
  --target-context prod-cluster \
  --input-dir output

# JSON output
crane validate \
  --target-context prod-cluster \
  --input-dir output \
  --format json > report.json

# Strict mode (fail on warnings)
crane validate \
  --target-context prod-cluster \
  --input-dir output \
  --fail-on-warn
```

## Common Test Scenarios

### 1. Local Clusters (Minikube ↔ Kind)

```bash
# Full migration with import
./test-migration.sh minikube kind-cluster test-app

# Validation only (dry-run)
./test-migration.sh --dry-run minikube kind-cluster test-app
```

### 2. OpenShift → Kubernetes

```bash
# Auto-import after validation
./test-migration.sh crc-admin minikube openshift-app

# Dry-run to check compatibility first
./test-migration.sh --dry-run crc-admin minikube openshift-app
```

Tests detection of OpenShift-specific resources (Routes, DeploymentConfigs).

### 3. Kubernetes Version Upgrade

```bash
# Auto-import
./test-migration.sh old-k8s-1-24 new-k8s-1-28 my-app

# Check for deprecated APIs first
./test-migration.sh --dry-run old-k8s-1-24 new-k8s-1-28 my-app
```

Tests deprecated API version detection.

### 4. Validation Only

```bash
# Option 1: Use dry-run mode
./test-migration.sh --dry-run source target namespace

# Option 2: Manual workflow with validation-only script
crane export --context source --namespace app -e export
crane transform-prepare -e export -t transform
crane transform-apply -e export -t transform -o output
./test-validate-only.sh target-cluster ./output
```

## Viewing Results

### JSON Report with jq

```bash
# Summary
jq '.summary' .crane-test/migration-test-*/reports/validation-report.json

# Domain statuses
jq '.domains[] | {name, status, issues: (.issues | length)}' report.json

# Blocking issues only
jq '.blocking_issues[]' report.json

# All warnings
jq '.warnings[]' report.json
```

### Table Output

The default table format provides human-readable output:

```
================================================================================
Validation Report - 2026-03-03T15:37:54+01:00
================================================================================
Target Context:  crc-admin
Input Directory: ./.crane-test/migration-test-20260303-153804/output
Status:          PASS
================================================================================

Summary:
        METRIC        | COUNT
----------------------+--------
  Total Resources     |     3
  Validated Resources |     3
  Errors              |     0
  Warnings            |     3

Validation Domains:
              DOMAIN             | STATUS | ISSUES |   DURATION
---------------------------------+--------+--------+---------------
  Target Reachability &          | PASS   |      0 | 27.468695ms
  Authentication                 |        |        |
  API Compatibility              | WARN   |      3 | 191.993126ms
```

## Troubleshooting

### Build Errors

```bash
# Rebuild crane
go build -o crane
```

### Context Not Found

```bash
# List available contexts
kubectl config get-contexts

# Switch context
kubectl config use-context <context-name>
```

### Permission Errors

```bash
# Check permissions
kubectl auth can-i create deployments --namespace test-migration
kubectl auth can-i '*' '*' --all-namespaces
```

### No Test Artifacts

Ensure `.crane-test/` directory exists:
```bash
mkdir -p .crane-test
```

## CI/CD Integration

### Example: GitHub Actions

```yaml
- name: Run Migration Validation
  run: |
    cd crane
    ./test-migration.sh minikube staging-cluster
```

### Example: GitLab CI

```yaml
test:migration:
  script:
    - cd crane
    - ./test-migration.sh source-cluster target-cluster
  artifacts:
    paths:
      - crane/.crane-test/
```

## Cleanup

```bash
# Interactive cleanup
./cleanup-tests.sh

# Force cleanup
rm -rf .crane-test/

# Clean namespaces
kubectl --context source delete namespace test-migration
kubectl --context target delete namespace test-migration
```

## Full Documentation

See [TESTING.md](./TESTING.md) for complete documentation including:
- Detailed validation domain explanations
- Advanced usage examples
- Storage class mapping
- Custom plugin configuration
- CI/CD integration templates

## Support

- View validation details in JSON reports
- Check remediation suggestions in output
- Consult main Crane documentation
