#!/bin/bash
#
# Quick Validation Test
#
# This script tests only the validate command against existing manifests
#
# Usage: ./test-validate-only.sh <target-context> <manifests-dir>
#

set -e

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

log_info() { echo -e "${BLUE}ℹ${NC} $1"; }
log_success() { echo -e "${GREEN}✓${NC} $1"; }
log_warning() { echo -e "${YELLOW}⚠${NC} $1"; }
log_error() { echo -e "${RED}✗${NC} $1"; }

if [ $# -lt 2 ]; then
    echo "Usage: $0 <target-context> <manifests-dir>"
    echo ""
    echo "Example:"
    echo "  $0 prod-cluster ./output"
    exit 1
fi

TARGET_CONTEXT=$1
MANIFESTS_DIR=$2
CRANE_BIN="./crane"

# Build if needed
if [ ! -f "$CRANE_BIN" ]; then
    log_info "Building crane..."
    go build -o crane
fi

# Verify context
log_info "Verifying target context: $TARGET_CONTEXT"
if ! kubectl config get-contexts "$TARGET_CONTEXT" &>/dev/null; then
    log_error "Context '$TARGET_CONTEXT' not found"
    exit 1
fi

# Verify manifests directory
if [ ! -d "$MANIFESTS_DIR" ]; then
    log_error "Manifests directory not found: $MANIFESTS_DIR"
    exit 1
fi

MANIFEST_COUNT=$(find "$MANIFESTS_DIR" -type f \( -name "*.yaml" -o -name "*.yml" \) | wc -l)
log_info "Found $MANIFEST_COUNT manifest files"

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "  Validation Test"
echo "═══════════════════════════════════════════════════════════"
echo ""

# Test 1: Table format
log_info "Running validation (table format)..."
"$CRANE_BIN" validate \
    --target-context "$TARGET_CONTEXT" \
    --input-dir "$MANIFESTS_DIR" \
    --format table

EXIT_CODE=$?

echo ""
echo "═══════════════════════════════════════════════════════════"

# Test 2: JSON format
TEST_BASE_DIR="./.crane-test"
mkdir -p "$TEST_BASE_DIR"
REPORT_FILE="${TEST_BASE_DIR}/validation-report-$(date +%Y%m%d-%H%M%S).json"
log_info "Saving JSON report to: $REPORT_FILE"
"$CRANE_BIN" validate \
    --target-context "$TARGET_CONTEXT" \
    --input-dir "$MANIFESTS_DIR" \
    --format json > "$REPORT_FILE"

# Show summary from JSON
if command -v jq &> /dev/null; then
    echo ""
    log_info "Validation Summary (from JSON):"
    jq -r '.summary | to_entries | .[] | "  \(.key): \(.value)"' "$REPORT_FILE"

    echo ""
    log_info "Domain Results:"
    jq -r '.domains[] | "  [\(.status)] \(.name) - \(.issues | length) issues"' "$REPORT_FILE"
fi

echo ""
case $EXIT_CODE in
    0)
        log_success "Validation PASSED (exit code: 0)"
        ;;
    2)
        log_warning "Validation found incompatibilities (exit code: 2)"
        ;;
    4)
        log_error "Input/config error (exit code: 4)"
        ;;
    5)
        log_error "Target cluster connectivity failed (exit code: 5)"
        ;;
    *)
        log_error "Unknown exit code: $EXIT_CODE"
        ;;
esac

echo ""
log_info "Full JSON report: $REPORT_FILE"

if command -v jq &> /dev/null; then
    echo ""
    echo "View full report with: jq . $REPORT_FILE"
else
    echo ""
    echo "Install 'jq' for better JSON viewing"
fi

exit $EXIT_CODE
