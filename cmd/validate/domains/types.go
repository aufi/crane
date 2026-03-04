package domains

// DomainResult represents results from a single validation domain
type DomainResult struct {
	Name        string  `json:"name"`
	Status      string  `json:"status"` // PASS, FAIL, WARN
	Description string  `json:"description,omitempty"`
	Issues      []Issue `json:"issues,omitempty"`
	Duration    string  `json:"duration,omitempty"`
}

// Issue represents a single validation problem
type Issue struct {
	Severity    string `json:"severity"` // error, warning
	Message     string `json:"message"`
	Resource    string `json:"resource,omitempty"`
	Field       string `json:"field,omitempty"`
	Remediation string `json:"remediation,omitempty"`
	Details     string `json:"details,omitempty"`
}

// DomainStatus constants
const (
	DomainStatusPass = "PASS"
	DomainStatusFail = "FAIL"
	DomainStatusWarn = "WARN"
	DomainStatusSkip = "SKIP"
)

// IssueSeverity constants
const (
	IssueSeverityError   = "error"
	IssueSeverityWarning = "warning"
)
