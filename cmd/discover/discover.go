package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/konveyor/crane/internal/flags"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Options struct {
	cobraGlobalFlags *flags.GlobalFlags
	globalFlags      *flags.GlobalFlags
	cobraFlags       Flags
	Flags
}

type Flags struct {
	SourceContext  string   `mapstructure:"source-context"`
	Namespace      string   `mapstructure:"namespace"`
	AllNamespaces  bool     `mapstructure:"all-namespaces"`
	LabelSelector  string   `mapstructure:"selector"`
	IncludeGVK     []string `mapstructure:"include-gvk"`
	ExcludeGVK     []string `mapstructure:"exclude-gvk"`
	PlanTarget     string   `mapstructure:"plan-target"`
	Format         string   `mapstructure:"format"`
	OutputPlanFile string   `mapstructure:"output-plan"`
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	if o.SourceContext == "" {
		return fmt.Errorf("--source-context is required")
	}
	if !o.AllNamespaces && o.Namespace == "" {
		return fmt.Errorf("either --namespace or --all-namespaces must be specified")
	}
	return nil
}

func (o *Options) Validate() error {
	validFormats := map[string]bool{"json": true, "yaml": true, "table": true}
	if !validFormats[o.Format] {
		return fmt.Errorf("invalid format: %s (must be 'json', 'yaml', or 'table')", o.Format)
	}
	return nil
}

func (o *Options) Run() error {
	return o.run()
}

func NewDiscoverCommand(f *flags.GlobalFlags) *cobra.Command {
	o := &Options{
		cobraGlobalFlags: f,
	}
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Discover and analyze resources in source cluster for migration planning",
		Long: `Discover inspects source cluster resources to help with migration planning.

This read-only command helps you:
  1. Inspect source-cluster resources relevant to migration
  2. Iteratively define and test selectors (labels/namespaces)
  3. Optionally generate migration-plan guidance for a specific target cluster

Use this before 'crane export' to understand scope and identify potential issues early.`,
		Example: `  # Discover resources in a specific namespace
  crane discover --source-context src --namespace app-a

  # Discover with label selector
  crane discover --source-context src --namespace app-a --selector "team=payments"

  # Discover all namespaces with GVK filter
  crane discover --source-context src --all-namespaces --include-gvk "apps/v1/Deployment,v1/Service"

  # Generate migration plan for target cluster
  crane discover --source-context src --namespace prod --plan-target dst --output-plan plan.json`,
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
	cmd.Flags().StringVarP(&o.SourceContext, "source-context", "s", "", "Kubeconfig context for the source cluster (required)")
	cmd.Flags().StringVarP(&o.Namespace, "namespace", "n", "", "Namespace to discover (mutually exclusive with --all-namespaces)")
	cmd.Flags().BoolVarP(&o.AllNamespaces, "all-namespaces", "A", false, "Discover resources in all namespaces")
	cmd.Flags().StringVar(&o.LabelSelector, "selector", "", "Kubernetes label selector to filter resources")
	cmd.Flags().StringSliceVar(&o.IncludeGVK, "include-gvk", nil, "Include only these GVKs (format: group/version/kind)")
	cmd.Flags().StringSliceVar(&o.ExcludeGVK, "exclude-gvk", nil, "Exclude these GVKs (format: group/version/kind)")
	cmd.Flags().StringVarP(&o.PlanTarget, "plan-target", "t", "", "Target context for migration planning (optional)")
	cmd.Flags().StringVar(&o.Format, "format", "table", "Output format: json, yaml, or table")
	cmd.Flags().StringVarP(&o.OutputPlanFile, "output-plan", "o", "", "Write migration plan to file (JSON format)")

	cmd.MarkFlagRequired("source-context")
}

func (o *Options) run() error {
	log := o.globalFlags.GetLogger()
	ctx := context.Background()

	log.Infof("Discovering resources in source cluster: %s", o.SourceContext)

	// Connect to source cluster
	sourceClient, sourceConfig, err := o.getKubeClient(o.SourceContext)
	if err != nil {
		return fmt.Errorf("failed to connect to source cluster: %w", err)
	}

	// Discover resources
	inventory, err := o.discoverResources(ctx, sourceClient, sourceConfig)
	if err != nil {
		return fmt.Errorf("failed to discover resources: %w", err)
	}

	// Build discovery report
	report := DiscoveryReport{
		Timestamp:      time.Now(),
		SourceContext:  o.SourceContext,
		Namespace:      o.Namespace,
		AllNamespaces:  o.AllNamespaces,
		LabelSelector:  o.LabelSelector,
		ResourceCounts: inventory.ResourceCounts,
		TotalResources: inventory.TotalCount,
		Namespaces:     inventory.Namespaces,
	}

	// If plan-target specified, generate migration plan
	if o.PlanTarget != "" {
		log.Infof("Generating migration plan for target: %s", o.PlanTarget)
		plan, err := o.generateMigrationPlan(ctx, inventory, o.PlanTarget)
		if err != nil {
			log.Warnf("Failed to generate migration plan: %v", err)
		} else {
			report.MigrationPlan = plan
		}
	}

	// Output report
	return o.outputReport(report)
}

func (o *Options) getKubeClient(contextName string) (kubernetes.Interface, *rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{
		CurrentContext: contextName,
	}

	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return client, config, nil
}

func (o *Options) discoverResources(ctx context.Context, client kubernetes.Interface, config *rest.Config) (*ResourceInventory, error) {
	log := o.globalFlags.GetLogger()

	discoveryClient := client.Discovery()
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Get all server resources
	serverResources, err := discoveryClient.ServerPreferredResources()
	if err != nil {
		log.Warnf("Discovery returned partial results: %v", err)
	}

	inventory := &ResourceInventory{
		ResourceCounts: make(map[string]int),
		Namespaces:     make(map[string]bool),
	}

	// Parse GVK filters
	includeMap := o.parseGVKFilter(o.IncludeGVK)
	excludeMap := o.parseGVKFilter(o.ExcludeGVK)

	// Iterate through resources
	for _, apiResourceList := range serverResources {
		gv, err := schema.ParseGroupVersion(apiResourceList.GroupVersion)
		if err != nil {
			continue
		}

		for _, resource := range apiResourceList.APIResources {
			// Skip subresources
			if strings.Contains(resource.Name, "/") {
				continue
			}

			gvk := schema.GroupVersionKind{
				Group:   gv.Group,
				Version: gv.Version,
				Kind:    resource.Kind,
			}

			// Apply GVK filters
			if len(includeMap) > 0 && !includeMap[gvk.String()] {
				continue
			}
			if excludeMap[gvk.String()] {
				continue
			}

			// Count resources
			count, namespaces, err := o.countResources(ctx, dynamicClient, gv, resource)
			if err != nil {
				log.Debugf("Failed to count %s: %v", gvk.String(), err)
				continue
			}

			if count > 0 {
				inventory.ResourceCounts[gvk.String()] = count
				inventory.TotalCount += count

				for ns := range namespaces {
					inventory.Namespaces[ns] = true
				}
			}
		}
	}

	return inventory, nil
}

func (o *Options) countResources(ctx context.Context, client dynamic.Interface, gv schema.GroupVersion, resource metav1.APIResource) (int, map[string]bool, error) {
	gvr := schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: resource.Name,
	}

	namespaces := make(map[string]bool)
	listOptions := metav1.ListOptions{}
	if o.LabelSelector != "" {
		listOptions.LabelSelector = o.LabelSelector
	}

	var totalCount int

	if resource.Namespaced {
		if o.AllNamespaces {
			// List across all namespaces
			list, err := client.Resource(gvr).List(ctx, listOptions)
			if err != nil {
				return 0, nil, err
			}
			totalCount = len(list.Items)
			for _, item := range list.Items {
				namespaces[item.GetNamespace()] = true
			}
		} else {
			// List in specific namespace
			list, err := client.Resource(gvr).Namespace(o.Namespace).List(ctx, listOptions)
			if err != nil {
				return 0, nil, err
			}
			totalCount = len(list.Items)
			if totalCount > 0 {
				namespaces[o.Namespace] = true
			}
		}
	} else {
		// Cluster-scoped resource
		list, err := client.Resource(gvr).List(ctx, listOptions)
		if err != nil {
			return 0, nil, err
		}
		totalCount = len(list.Items)
		namespaces["cluster-scoped"] = true
	}

	return totalCount, namespaces, nil
}

func (o *Options) parseGVKFilter(filters []string) map[string]bool {
	filterMap := make(map[string]bool)
	for _, filter := range filters {
		// Expected format: group/version/kind or version/kind
		parts := strings.Split(filter, "/")
		if len(parts) == 3 {
			// group/version/kind
			gvk := schema.GroupVersionKind{
				Group:   parts[0],
				Version: parts[1],
				Kind:    parts[2],
			}
			filterMap[gvk.String()] = true
		} else if len(parts) == 2 {
			// version/kind (core API)
			gvk := schema.GroupVersionKind{
				Group:   "",
				Version: parts[0],
				Kind:    parts[1],
			}
			filterMap[gvk.String()] = true
		}
	}
	return filterMap
}

func (o *Options) generateMigrationPlan(ctx context.Context, inventory *ResourceInventory, targetContext string) (*MigrationPlan, error) {
	log := o.globalFlags.GetLogger()

	// Connect to target cluster
	targetClient, _, err := o.getKubeClient(targetContext)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to target cluster: %w", err)
	}

	targetDiscovery := targetClient.Discovery()
	targetResources, err := targetDiscovery.ServerPreferredResources()
	if err != nil {
		log.Warnf("Target discovery returned partial results: %v", err)
	}

	// Build target GVK map
	targetGVKs := make(map[string]bool)
	for _, apiResourceList := range targetResources {
		gv, err := schema.ParseGroupVersion(apiResourceList.GroupVersion)
		if err != nil {
			continue
		}

		for _, resource := range apiResourceList.APIResources {
			gvk := schema.GroupVersionKind{
				Group:   gv.Group,
				Version: gv.Version,
				Kind:    resource.Kind,
			}
			targetGVKs[gvk.String()] = true
		}
	}

	// Analyze compatibility
	plan := &MigrationPlan{
		TargetContext:       targetContext,
		CompatibleResources: []string{},
		MissingAPIs:         []string{},
		Recommendations:     []string{},
	}

	for gvk := range inventory.ResourceCounts {
		if targetGVKs[gvk] {
			plan.CompatibleResources = append(plan.CompatibleResources, gvk)
		} else {
			plan.MissingAPIs = append(plan.MissingAPIs, gvk)

			// Add recommendations
			if strings.Contains(gvk, "Route") {
				plan.Recommendations = append(plan.Recommendations,
					fmt.Sprintf("Route resource detected but not available on target - consider using RouteToIngress plugin"))
			}
			if strings.Contains(gvk, "DeploymentConfig") {
				plan.Recommendations = append(plan.Recommendations,
					fmt.Sprintf("DeploymentConfig detected but not available on target - consider using DeploymentConfigToDeployment plugin"))
			}
		}
	}

	sort.Strings(plan.CompatibleResources)
	sort.Strings(plan.MissingAPIs)

	return plan, nil
}

func (o *Options) outputReport(report DiscoveryReport) error {
	switch o.Format {
	case "json":
		return o.outputJSON(report)
	case "yaml":
		return o.outputYAML(report)
	case "table":
		return o.outputTable(report)
	default:
		return fmt.Errorf("unsupported format: %s", o.Format)
	}
}

func (o *Options) outputJSON(report DiscoveryReport) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func (o *Options) outputYAML(report DiscoveryReport) error {
	// For simplicity, output as JSON for now
	return o.outputJSON(report)
}

func (o *Options) outputTable(report DiscoveryReport) error {
	log := o.globalFlags.GetLogger()

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Printf("Discovery Report - %s\n", report.Timestamp.Format(time.RFC3339))
	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("Source Context: %s\n", report.SourceContext)
	if report.AllNamespaces {
		fmt.Println("Scope: All namespaces")
	} else {
		fmt.Printf("Namespace: %s\n", report.Namespace)
	}
	if report.LabelSelector != "" {
		fmt.Printf("Label Selector: %s\n", report.LabelSelector)
	}
	fmt.Println(strings.Repeat("=", 80))

	// Summary
	fmt.Printf("\nTotal Resources Found: %d\n", report.TotalResources)
	fmt.Printf("Unique Resource Types: %d\n", len(report.ResourceCounts))
	fmt.Printf("Namespaces: %d\n\n", len(report.Namespaces))

	// Resource counts table
	if len(report.ResourceCounts) > 0 {
		fmt.Println("Resource Types:")
		table := tablewriter.NewWriter(os.Stdout)
		table.SetHeader([]string{"Resource Type (GVK)", "Count"})
		table.SetBorder(false)

		// Sort by GVK
		gvks := make([]string, 0, len(report.ResourceCounts))
		for gvk := range report.ResourceCounts {
			gvks = append(gvks, gvk)
		}
		sort.Strings(gvks)

		for _, gvk := range gvks {
			count := report.ResourceCounts[gvk]
			table.Append([]string{gvk, fmt.Sprintf("%d", count)})
		}
		table.Render()
	}

	// Migration plan
	if report.MigrationPlan != nil {
		plan := report.MigrationPlan
		fmt.Println("\n" + strings.Repeat("-", 80))
		fmt.Printf("Migration Plan for Target: %s\n", plan.TargetContext)
		fmt.Println(strings.Repeat("-", 80))

		fmt.Printf("\nCompatible Resources: %d\n", len(plan.CompatibleResources))
		fmt.Printf("Missing APIs on Target: %d\n", len(plan.MissingAPIs))

		if len(plan.MissingAPIs) > 0 {
			fmt.Println("\nMissing APIs (require transformation or CRD installation):")
			for _, gvk := range plan.MissingAPIs {
				fmt.Printf("  - %s\n", gvk)
			}
		}

		if len(plan.Recommendations) > 0 {
			fmt.Println("\nRecommendations:")
			for i, rec := range plan.Recommendations {
				fmt.Printf("  %d. %s\n", i+1, rec)
			}
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 80) + "\n")

	// Write plan file if requested
	if o.OutputPlanFile != "" && report.MigrationPlan != nil {
		if err := o.writePlanFile(report); err != nil {
			log.Warnf("Failed to write plan file: %v", err)
		} else {
			log.Infof("Migration plan written to: %s", o.OutputPlanFile)
		}
	}

	return nil
}

func (o *Options) writePlanFile(report DiscoveryReport) error {
	file, err := os.Create(o.OutputPlanFile)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

// Data structures

type ResourceInventory struct {
	ResourceCounts map[string]int
	TotalCount     int
	Namespaces     map[string]bool
}

type DiscoveryReport struct {
	Timestamp      time.Time         `json:"timestamp"`
	SourceContext  string            `json:"source_context"`
	Namespace      string            `json:"namespace,omitempty"`
	AllNamespaces  bool              `json:"all_namespaces"`
	LabelSelector  string            `json:"label_selector,omitempty"`
	TotalResources int               `json:"total_resources"`
	ResourceCounts map[string]int    `json:"resource_counts"`
	Namespaces     map[string]bool   `json:"namespaces"`
	MigrationPlan  *MigrationPlan    `json:"migration_plan,omitempty"`
}

type MigrationPlan struct {
	TargetContext       string   `json:"target_context"`
	CompatibleResources []string `json:"compatible_resources"`
	MissingAPIs         []string `json:"missing_apis"`
	Recommendations     []string `json:"recommendations"`
}
