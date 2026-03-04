#!/bin/bash
#
# Crane Migration Test Script
#
# This script demonstrates a complete migration workflow using the new crane CLI structure:
# 1. Export resources from source cluster
# 2. Transform exported resources
# 3. Apply transformations
# 4. Validate against target cluster
# 5. Optionally import to target
#
# Usage: ./test-migration.sh [--dry-run] <source-context> <target-context> [namespace]
#

set -e  # Exit on error

# Parse flags
DRY_RUN=false
if [ "$1" = "--dry-run" ]; then
    DRY_RUN=true
    shift
fi

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Helper functions
log_info() {
    echo -e "${BLUE}ℹ ${NC}$1"
}

log_success() {
    echo -e "${GREEN}✓${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

log_error() {
    echo -e "${RED}✗${NC} $1"
}

print_header() {
    echo ""
    echo -e "${BLUE}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}═══════════════════════════════════════════════════════════════${NC}"
    echo ""
}

# Check arguments
if [ $# -lt 2 ]; then
    echo "Usage: $0 [--dry-run] <source-context> <target-context> [namespace]"
    echo ""
    echo "Arguments:"
    echo "  --dry-run       (Optional) Skip import to target cluster"
    echo "  source-context  Kubeconfig context for the source cluster"
    echo "  target-context  Kubeconfig context for the target cluster"
    echo "  namespace       (Optional) Namespace to migrate. Default: test-migration"
    echo ""
    echo "Examples:"
    echo "  $0 minikube kind-cluster my-app"
    echo "  $0 --dry-run minikube kind-cluster my-app"
    exit 1
fi

SOURCE_CONTEXT=$1
TARGET_CONTEXT=$2
NAMESPACE=${3:-test-migration}

# Configuration
TEST_BASE_DIR="./.crane-test"
WORK_DIR="${TEST_BASE_DIR}/migration-test-$(date +%Y%m%d-%H%M%S)"
EXPORT_DIR="${WORK_DIR}/export"
TRANSFORM_DIR="${WORK_DIR}/transform"
OUTPUT_DIR="${WORK_DIR}/output"
REPORT_DIR="${WORK_DIR}/reports"
CRANE_BIN="./crane"

# Check if crane binary exists
if [ ! -f "$CRANE_BIN" ]; then
    log_error "Crane binary not found at $CRANE_BIN"
    log_info "Building crane..."
    go build -o crane
    if [ $? -ne 0 ]; then
        log_error "Failed to build crane"
        exit 1
    fi
    log_success "Crane built successfully"
fi

# Verify contexts exist
log_info "Verifying kubeconfig contexts..."
if ! kubectl config get-contexts "$SOURCE_CONTEXT" &>/dev/null; then
    log_error "Source context '$SOURCE_CONTEXT' not found in kubeconfig"
    exit 1
fi

if ! kubectl config get-contexts "$TARGET_CONTEXT" &>/dev/null; then
    log_error "Target context '$TARGET_CONTEXT' not found in kubeconfig"
    exit 1
fi

log_success "Contexts verified"

# Create working directory
log_info "Creating working directory: $WORK_DIR"
mkdir -p "$TEST_BASE_DIR" "$WORK_DIR" "$EXPORT_DIR" "$TRANSFORM_DIR" "$OUTPUT_DIR" "$REPORT_DIR"

# Save test metadata
cat > "${WORK_DIR}/migration-metadata.txt" <<EOF
Crane Migration Test
====================
Date: $(date)
Source Context: $SOURCE_CONTEXT
Target Context: $TARGET_CONTEXT
Namespace: $NAMESPACE
Work Directory: $WORK_DIR
EOF

print_header "STEP 1: Export Resources from Source Cluster"

log_info "Switching to source context: $SOURCE_CONTEXT"
kubectl config use-context "$SOURCE_CONTEXT" >/dev/null

log_info "Checking if namespace exists..."
if ! kubectl get namespace "$NAMESPACE" &>/dev/null; then
    log_warning "Namespace '$NAMESPACE' does not exist on source cluster"
    log_info "Creating sample namespace and resources for testing..."

    # Create test namespace
    kubectl create namespace "$NAMESPACE"

    # Create sample resources
    kubectl -n "$NAMESPACE" create configmap test-config \
        --from-literal=key1=value1 \
        --from-literal=key2=value2

    kubectl -n "$NAMESPACE" create deployment test-app \
        --image=nginx:latest \
        --replicas=2

    kubectl -n "$NAMESPACE" expose deployment test-app \
        --port=80 \
        --target-port=80 \
        --name=test-service

    log_success "Created sample resources in namespace $NAMESPACE"

    # Wait a bit for resources to be created
    sleep 2
fi

log_info "Exporting namespace: $NAMESPACE"
"$CRANE_BIN" export \
    --context "$SOURCE_CONTEXT" \
    --namespace "$NAMESPACE" \
    --export-dir "$EXPORT_DIR"

if [ $? -ne 0 ]; then
    log_error "Export failed"
    exit 1
fi

# Count exported resources
EXPORT_COUNT=$(find "$EXPORT_DIR" -type f -name "*.yaml" -o -name "*.yml" | wc -l)
log_success "Exported $EXPORT_COUNT resource files"

print_header "STEP 2: Transform Resources"

log_info "Running transformation preparation..."
"$CRANE_BIN" transform-prepare \
    --export-dir "$EXPORT_DIR" \
    --transform-dir "$TRANSFORM_DIR"

if [ $? -ne 0 ]; then
    log_error "Transform-prepare failed"
    exit 1
fi

log_success "Transformation patches created"

print_header "STEP 3: Apply Transformations"

log_info "Applying transformations to generate output manifests..."
"$CRANE_BIN" transform-apply \
    --export-dir "$EXPORT_DIR" \
    --transform-dir "$TRANSFORM_DIR" \
    --output-dir "$OUTPUT_DIR"

if [ $? -ne 0 ]; then
    log_error "Transform apply failed"
    exit 1
fi

# Count output resources
OUTPUT_COUNT=$(find "$OUTPUT_DIR" -type f -name "*.yaml" -o -name "*.yml" | wc -l)
log_success "Generated $OUTPUT_COUNT output manifests"

print_header "STEP 4: Validate Against Target Cluster"

log_info "Switching to target context: $TARGET_CONTEXT"
kubectl config use-context "$TARGET_CONTEXT" >/dev/null

log_info "Running validation against target cluster..."
log_info "Target context: $TARGET_CONTEXT"

# Run validation with table output
"$CRANE_BIN" validate \
    --target-context "$TARGET_CONTEXT" \
    --input-dir "$OUTPUT_DIR" \
    --format table

VALIDATION_EXIT_CODE=$?

# Also save JSON report
"$CRANE_BIN" validate \
    --target-context "$TARGET_CONTEXT" \
    --input-dir "$OUTPUT_DIR" \
    --format json > "${REPORT_DIR}/validation-report.json"

if [ $VALIDATION_EXIT_CODE -eq 0 ]; then
    log_success "Validation PASSED - resources are compatible with target cluster"
    VALIDATION_STATUS="PASS"
elif [ $VALIDATION_EXIT_CODE -eq 2 ]; then
    log_warning "Validation found issues (exit code: 2)"
    log_info "Check the validation report for details"
    VALIDATION_STATUS="ISSUES_FOUND"
elif [ $VALIDATION_EXIT_CODE -eq 5 ]; then
    log_error "Validation failed - cannot connect to target cluster (exit code: 5)"
    VALIDATION_STATUS="CONNECTION_FAILED"
    exit 1
else
    log_error "Validation failed with exit code: $VALIDATION_EXIT_CODE"
    VALIDATION_STATUS="FAILED"
    exit 1
fi

print_header "STEP 5: Import to Target Cluster"

if [ "$DRY_RUN" = true ]; then
    log_info "Dry-run mode: Skipping import to target cluster"
    IMPORT_STATUS="SKIPPED (dry-run)"
elif [ "$VALIDATION_STATUS" = "PASS" ]; then
    log_info "Validation passed. Importing resources to target cluster..."

    log_info "Creating namespace on target cluster..."
    kubectl --context "$TARGET_CONTEXT" create namespace "$NAMESPACE" --dry-run=client -o yaml | \
        kubectl --context "$TARGET_CONTEXT" apply -f -

    log_info "Importing resources to target cluster..."
    kubectl --context "$TARGET_CONTEXT" apply -f "$OUTPUT_DIR" --recursive

    if [ $? -eq 0 ]; then
        log_success "Resources imported successfully"
        IMPORT_STATUS="SUCCESS"

        log_info "Verifying imported resources..."
        echo ""
        kubectl --context "$TARGET_CONTEXT" -n "$NAMESPACE" get all
        echo ""
    else
        log_error "Import failed"
        IMPORT_STATUS="FAILED"
        exit 1
    fi
else
    log_warning "Validation did not pass completely. Skipping automatic import."
    log_info "Review the validation report and manually import if needed:"
    log_info "  kubectl --context $TARGET_CONTEXT apply -f $OUTPUT_DIR --recursive"
    IMPORT_STATUS="SKIPPED (validation issues)"
fi

print_header "Migration Test Summary"

echo "Source Context:        $SOURCE_CONTEXT"
echo "Target Context:        $TARGET_CONTEXT"
echo "Namespace:             $NAMESPACE"
echo "Dry-run Mode:          $DRY_RUN"
echo "Exported Resources:    $EXPORT_COUNT files"
echo "Output Manifests:      $OUTPUT_COUNT files"
echo "Validation Status:     $VALIDATION_STATUS"
echo "Import Status:         $IMPORT_STATUS"
echo ""
echo "Work Directory:        $WORK_DIR"
echo "  ├── export/          Source cluster exports"
echo "  ├── transform/       Transformation patches"
echo "  ├── output/          Ready-to-import manifests"
echo "  └── reports/         Validation reports"
echo ""

if [ -f "${REPORT_DIR}/validation-report.json" ]; then
    log_info "Validation report saved to: ${REPORT_DIR}/validation-report.json"
fi

print_header "Next Steps"

echo "1. Review the validation report:"
echo "   cat ${REPORT_DIR}/validation-report.json | jq"
echo ""
echo "2. Inspect transformed manifests:"
echo "   ls -la $OUTPUT_DIR"
echo ""

if [ "$IMPORT_STATUS" = "SUCCESS" ]; then
    echo "3. Verify imported resources:"
    echo "   kubectl --context $TARGET_CONTEXT -n $NAMESPACE get all"
    echo ""
    echo "4. Clean up test resources (when done):"
    echo "   kubectl --context $SOURCE_CONTEXT delete namespace $NAMESPACE"
    echo "   kubectl --context $TARGET_CONTEXT delete namespace $NAMESPACE"
    echo "   rm -rf $TEST_BASE_DIR"
elif [ "$DRY_RUN" = true ]; then
    echo "3. To import to target cluster (run without --dry-run):"
    echo "   kubectl --context $TARGET_CONTEXT create namespace $NAMESPACE"
    echo "   kubectl --context $TARGET_CONTEXT apply -f $OUTPUT_DIR --recursive"
    echo ""
    echo "4. Clean up test resources (when done):"
    echo "   kubectl --context $SOURCE_CONTEXT delete namespace $NAMESPACE"
    echo "   rm -rf $TEST_BASE_DIR"
else
    echo "3. Manually apply to target cluster (if validation issues resolved):"
    echo "   kubectl --context $TARGET_CONTEXT create namespace $NAMESPACE"
    echo "   kubectl --context $TARGET_CONTEXT apply -f $OUTPUT_DIR --recursive"
    echo ""
    echo "4. Clean up test resources (when done):"
    echo "   kubectl --context $SOURCE_CONTEXT delete namespace $NAMESPACE"
    echo "   rm -rf $TEST_BASE_DIR"
fi
echo ""

log_success "Migration test completed!"
