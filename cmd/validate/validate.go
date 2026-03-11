package validate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/konveyor/crane/cmd/validate/domains"
	"github.com/konveyor/crane/internal/file"
	"github.com/konveyor/crane/internal/flags"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Re-export types from domains for external use
type DomainResult = domains.DomainResult
type Issue = domains.Issue

type Options struct {
	cobraGlobalFlags *flags.GlobalFlags
	globalFlags      *flags.GlobalFlags
	cobraFlags       Flags
	Flags
}

type Flags struct {
	TargetContext   string   `mapstructure:"target-context"`
	InputDir        string   `mapstructure:"input-dir"`
	ExportDir       string   `mapstructure:"export-dir"`
	StorageClassMap []string `mapstructure:"storage-class-map"`
	Format          string   `mapstructure:"format"`
	FailOnWarn      bool     `mapstructure:"fail-on-warn"`
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	if o.TargetContext == "" {
		return fmt.Errorf("--target-context is required")
	}
	if o.InputDir == "" && o.ExportDir == "" {
		return fmt.Errorf("either --input-dir or --export-dir must be specified")
	}
	return nil
}

func (o *Options) Validate() error {
	// Verify format is valid
	validFormats := map[string]bool{"json": true, "table": true}
	if !validFormats[o.Format] {
		return fmt.Errorf("invalid format: %s (must be 'json' or 'table')", o.Format)
	}
	return nil
}

func (o *Options) Run() error {
	return o.run()
}

func NewValidateCommand(f *flags.GlobalFlags) *cobra.Command {
	o := &Options{
		cobraGlobalFlags: f,
	}
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate that manifests are importable into a target cluster",
		Long: `Validate verifies that transformed manifests are compatible with and importable
into a target Kubernetes cluster. It checks:
  1. Target cluster reachability, authentication, and permissions
  2. API compatibility and resource availability
  3. Resource schema validation via server-side dry-run

The command is read-only and performs no mutations on the target cluster.`,
		Example: `  # Validate output manifests against production cluster
  crane validate --target-context prod-cluster --input-dir output

  # Get detailed JSON report
  crane validate --target-context prod-cluster --input-dir output --format json

  # Fail on warnings (strict mode)
  crane validate --target-context prod-cluster --input-dir output --fail-on-warn`,
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
			viper.Unmarshal(&o.Flags)
			viper.Unmarshal(&o.globalFlags)
		},
	}

	addFlagsForOptions(&o.cobraFlags, cmd)
	return cmd
}

func addFlagsForOptions(o *Flags, cmd *cobra.Command) {
	cmd.Flags().StringVarP(&o.TargetContext, "target-context", "c", "", "Kubeconfig context for the target cluster (required)")
	cmd.Flags().StringVarP(&o.InputDir, "input-dir", "i", "", "Directory containing manifests to validate (typically 'output' from transform apply)")
	cmd.Flags().StringVarP(&o.ExportDir, "export-dir", "e", "", "Directory containing exported resources (alternative to input-dir)")
	cmd.Flags().StringSliceVar(&o.StorageClassMap, "storage-class-map", nil, "Storage class mappings (format: src=dst,...)")
	cmd.Flags().StringVar(&o.Format, "format", "table", "Output format: json or table")
	cmd.Flags().BoolVar(&o.FailOnWarn, "fail-on-warn", false, "Exit with error code if warnings are found")

	cmd.MarkFlagRequired("target-context")
}

func (o *Options) run() error {
	log := o.globalFlags.GetLogger()
	ctx := context.Background()

	// Determine which directory to read from
	sourceDir := o.InputDir
	if sourceDir == "" {
		sourceDir = o.ExportDir
	}

	absSourceDir, err := filepath.Abs(sourceDir)
	if err != nil {
		return fmt.Errorf("failed to resolve source directory: %w", err)
	}

	log.Infof("Validating manifests from: %s", absSourceDir)
	log.Infof("Target context: %s", o.TargetContext)

	// Read all manifest files
	files, err := file.ReadFiles(ctx, absSourceDir)
	if err != nil {
		return fmt.Errorf("failed to read manifest files: %w", err)
	}

	if len(files) == 0 {
		return fmt.Errorf("no manifest files found in %s", absSourceDir)
	}

	log.Infof("Found %d manifest files", len(files))

	// Extract objects and GVKs
	objects := make([]*unstructured.Unstructured, 0, len(files))
	gvks := make([]schema.GroupVersionKind, 0)
	gvkSet := make(map[string]bool)

	for _, f := range files {
		obj := f.Unstructured
		objects = append(objects, &obj)

		gvk := obj.GroupVersionKind()
		gvkKey := gvk.String()
		if !gvkSet[gvkKey] {
			gvks = append(gvks, gvk)
			gvkSet[gvkKey] = true
		}
	}

	log.Infof("Found %d unique resource types", len(gvks))

	// Initialize validation report
	report := ValidationReport{
		Timestamp:     time.Now(),
		TargetContext: o.TargetContext,
		InputDir:      absSourceDir,
		Domains:       []DomainResult{},
		Summary: ValidationSummary{
			TotalResources:     len(objects),
			ValidatedResources: 0,
			ErrorCount:         0,
			WarningCount:       0,
		},
	}

	// Domain 1: Auth & Permissions
	log.Info("Running validation domain: Target Reachability & Authentication")
	authValidator, err := domains.NewAuthValidator(o.TargetContext)
	if err != nil {
		report.Status = ReportStatusError
		report.Domains = append(report.Domains, DomainResult{
			Name:   domains.AuthDomainName,
			Status: DomainStatusFail,
			Issues: []Issue{{
				Severity: IssueSeverityError,
				Message:  fmt.Sprintf("Failed to initialize auth validator: %v", err),
			}},
		})
		return o.outputReport(report, ExitTargetConnectivity)
	}

	authResult := authValidator.Validate(ctx, gvks)
	report.Domains = append(report.Domains, authResult)

	// If auth fails, stop here
	if authResult.Status == domains.DomainStatusFail {
		report.Status = ReportStatusError
		o.aggregateIssues(&report)
		return o.outputReport(report, ExitTargetConnectivity)
	}

	// Domain 2: API Compatibility
	log.Info("Running validation domain: API Compatibility")
	apiValidator, err := domains.NewAPIValidator(
		authValidator.GetRestConfig(),
		authValidator.GetDiscoveryClient(),
	)
	if err != nil {
		report.Status = ReportStatusError
		report.Domains = append(report.Domains, DomainResult{
			Name:   domains.APIDomainName,
			Status: DomainStatusFail,
			Issues: []Issue{{
				Severity: IssueSeverityError,
				Message:  fmt.Sprintf("Failed to initialize API validator: %v", err),
			}},
		})
		return o.outputReport(report, ExitInputError)
	}

	apiResult := apiValidator.Validate(ctx, objects)
	report.Domains = append(report.Domains, apiResult)

	// Domain 3: Capacity checks
	log.Info("Running validation domain: Resource Capacity")
	capacityValidator, err := domains.NewCapacityValidator(authValidator.GetKubernetesClient())
	if err != nil {
		report.Status = ReportStatusError
		report.Domains = append(report.Domains, DomainResult{
			Name:   domains.CapacityDomainName,
			Status: DomainStatusFail,
			Issues: []Issue{{
				Severity: IssueSeverityError,
				Message:  fmt.Sprintf("Failed to initialize capacity validator: %v", err),
			}},
		})
		return o.outputReport(report, ExitInputError)
	}

	// Parse storage class map
	storageClassMap := domains.ParseStorageClassMap(o.StorageClassMap)
	capacityResult := capacityValidator.Validate(ctx, objects, storageClassMap)
	report.Domains = append(report.Domains, capacityResult)

	// Aggregate issues and determine final status
	o.aggregateIssues(&report)
	report.Summary.ValidatedResources = len(objects)

	// Determine exit code
	exitCode := o.determineExitCode(report)

	return o.outputReport(report, exitCode)
}

func (o *Options) aggregateIssues(report *ValidationReport) {
	for _, domain := range report.Domains {
		for _, issue := range domain.Issues {
			if issue.Severity == domains.IssueSeverityError {
				report.BlockingIssues = append(report.BlockingIssues, issue)
				report.Summary.ErrorCount++
			} else if issue.Severity == domains.IssueSeverityWarning {
				report.Warnings = append(report.Warnings, issue)
				report.Summary.WarningCount++
			}
		}
	}

	// Set overall status
	if report.Summary.ErrorCount > 0 {
		report.Status = ReportStatusUnresolved
	} else if report.Summary.WarningCount > 0 {
		if o.FailOnWarn {
			report.Status = ReportStatusUnresolved
		} else {
			report.Status = ReportStatusPass
		}
	} else {
		report.Status = ReportStatusPass
	}
}

func (o *Options) determineExitCode(report ValidationReport) int {
	if report.Summary.ErrorCount > 0 {
		return ExitUnresolved
	}
	if report.Summary.WarningCount > 0 && o.FailOnWarn {
		return ExitUnresolved
	}
	return ExitSuccess
}

func (o *Options) outputReport(report ValidationReport, exitCode int) error {
	switch o.Format {
	case "json":
		return o.outputJSON(report, exitCode)
	case "table":
		return o.outputTable(report, exitCode)
	default:
		return fmt.Errorf("unsupported format: %s", o.Format)
	}
}

func (o *Options) outputJSON(report ValidationReport, exitCode int) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return err
	}

	if exitCode != ExitSuccess {
		os.Exit(exitCode)
	}
	return nil
}

func (o *Options) outputTable(report ValidationReport, exitCode int) error {
	log := o.globalFlags.GetLogger()

	// Header
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Printf("Validation Report - %s\n", report.Timestamp.Format(time.RFC3339))
	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("Target Context:  %s\n", report.TargetContext)
	fmt.Printf("Input Directory: %s\n", report.InputDir)
	fmt.Printf("Status:          %s\n", report.Status)
	fmt.Println(strings.Repeat("=", 80))

	// Summary table
	fmt.Println("\nSummary:")
	summaryTable := tablewriter.NewWriter(os.Stdout)
	summaryTable.SetHeader([]string{"Metric", "Count"})
	summaryTable.SetBorder(false)
	summaryTable.Append([]string{"Total Resources", fmt.Sprintf("%d", report.Summary.TotalResources)})
	summaryTable.Append([]string{"Validated Resources", fmt.Sprintf("%d", report.Summary.ValidatedResources)})
	summaryTable.Append([]string{"Errors", fmt.Sprintf("%d", report.Summary.ErrorCount)})
	summaryTable.Append([]string{"Warnings", fmt.Sprintf("%d", report.Summary.WarningCount)})
	summaryTable.Render()

	// Domain results
	fmt.Println("\nValidation Domains:")
	domainTable := tablewriter.NewWriter(os.Stdout)
	domainTable.SetHeader([]string{"Domain", "Status", "Issues", "Duration"})
	domainTable.SetBorder(false)

	for _, domain := range report.Domains {
		issueCount := fmt.Sprintf("%d", len(domain.Issues))
		domainTable.Append([]string{domain.Name, domain.Status, issueCount, domain.Duration})
	}
	domainTable.Render()

	// Blocking issues
	if len(report.BlockingIssues) > 0 {
		fmt.Println("\nBlocking Issues:")
		for i, issue := range report.BlockingIssues {
			fmt.Printf("\n%d. [%s] %s\n", i+1, strings.ToUpper(issue.Severity), issue.Message)
			if issue.Resource != "" {
				fmt.Printf("   Resource: %s\n", issue.Resource)
			}
			if issue.Details != "" {
				fmt.Printf("   Details: %s\n", issue.Details)
			}
			if issue.Remediation != "" {
				fmt.Printf("   Remediation: %s\n", issue.Remediation)
			}
		}
	}

	// Warnings
	if len(report.Warnings) > 0 {
		fmt.Println("\nWarnings:")
		for i, issue := range report.Warnings {
			fmt.Printf("\n%d. [%s] %s\n", i+1, strings.ToUpper(issue.Severity), issue.Message)
			if issue.Resource != "" {
				fmt.Printf("   Resource: %s\n", issue.Resource)
			}
			if issue.Details != "" {
				fmt.Printf("   Details: %s\n", issue.Details)
			}
			if issue.Remediation != "" {
				fmt.Printf("   Remediation: %s\n", issue.Remediation)
			}
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 80))

	// Final status message
	switch report.Status {
	case ReportStatusPass:
		log.Info("✓ Validation PASSED - all checks successful")
	case ReportStatusUnresolved:
		log.Warn("✗ Validation FAILED - unresolved issues found")
	case ReportStatusError:
		log.Error("✗ Validation ERROR - critical failure occurred")
	}

	fmt.Println(strings.Repeat("=", 80) + "\n")

	if exitCode != ExitSuccess {
		os.Exit(exitCode)
	}
	return nil
}
