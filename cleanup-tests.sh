#!/bin/bash
#
# Cleanup Crane Test Artifacts
#
# Removes all test artifacts created by test-migration.sh and test-validate-only.sh
#

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}ℹ${NC} $1"; }
log_success() { echo -e "${GREEN}✓${NC} $1"; }
log_warning() { echo -e "${YELLOW}⚠${NC} $1"; }

TEST_DIR="./.crane-test"

if [ ! -d "$TEST_DIR" ]; then
    log_info "No test artifacts found (${TEST_DIR} does not exist)"
    exit 0
fi

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "  Crane Test Cleanup"
echo "═══════════════════════════════════════════════════════════"
echo ""

# Count test runs
TEST_COUNT=$(find "$TEST_DIR" -maxdepth 1 -type d -name "migration-test-*" | wc -l)
REPORT_COUNT=$(find "$TEST_DIR" -maxdepth 1 -type f -name "validation-report-*.json" | wc -l)

log_info "Found:"
echo "  - $TEST_COUNT migration test run(s)"
echo "  - $REPORT_COUNT validation report(s)"

# Calculate total size
TOTAL_SIZE=$(du -sh "$TEST_DIR" 2>/dev/null | cut -f1)
log_info "Total disk usage: $TOTAL_SIZE"

echo ""
read -p "Delete all test artifacts in ${TEST_DIR}? (y/N) " -n 1 -r
echo ""

if [[ $REPLY =~ ^[Yy]$ ]]; then
    log_info "Removing test artifacts..."
    rm -rf "$TEST_DIR"
    log_success "Test artifacts removed"
else
    log_info "Cleanup cancelled"

    echo ""
    log_info "To manually clean up:"
    echo "  rm -rf $TEST_DIR"

    echo ""
    log_info "To remove specific test run:"
    echo "  rm -rf ${TEST_DIR}/migration-test-YYYYMMDD-HHMMSS"
fi

echo ""
