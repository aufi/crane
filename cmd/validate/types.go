package validate

import (
	"time"

	"github.com/konveyor/crane/cmd/validate/domains"
)

// Exit codes for validate command
const (
	ExitSuccess            = 0 // PASS - all validations passed
	ExitUnresolved         = 2 // UNRESOLVED - incompatibilities found
	ExitMaxIterations      = 3 // MAX_ITERATIONS - safety stop (future use)
	ExitInputError         = 4 // INPUT_ERROR - config/input error
	ExitTargetConnectivity = 5 // TARGET_CONNECTIVITY - target unreachable/auth fail
)

// ValidationReport represents the complete validation result
type ValidationReport struct {
	Timestamp      time.Time            `json:"timestamp"`
	TargetContext  string               `json:"target_context"`
	InputDir       string               `json:"input_dir"`
	Status         string               `json:"status"` // PASS, UNRESOLVED, ERROR
	Domains        []domains.DomainResult `json:"domains"`
	BlockingIssues []domains.Issue      `json:"blocking_issues,omitempty"`
	Warnings       []domains.Issue      `json:"warnings,omitempty"`
	Summary        ValidationSummary    `json:"summary"`
}

// ValidationSummary provides aggregated statistics
type ValidationSummary struct {
	TotalResources     int `json:"total_resources"`
	ValidatedResources int `json:"validated_resources"`
	ErrorCount         int `json:"error_count"`
	WarningCount       int `json:"warning_count"`
}

// ReportStatus constants
const (
	ReportStatusPass       = "PASS"
	ReportStatusUnresolved = "UNRESOLVED"
	ReportStatusError      = "ERROR"
)

// Re-export domain constants for convenience
const (
	DomainStatusPass = domains.DomainStatusPass
	DomainStatusFail = domains.DomainStatusFail
	DomainStatusWarn = domains.DomainStatusWarn
	DomainStatusSkip = domains.DomainStatusSkip
)

const (
	IssueSeverityError   = domains.IssueSeverityError
	IssueSeverityWarning = domains.IssueSeverityWarning
)
