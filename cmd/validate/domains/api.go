package domains

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

const (
	APIDomainName = "API Compatibility"
)

// APIValidator validates API compatibility and creatability
type APIValidator struct {
	discoveryClient discovery.DiscoveryInterface
	dynamicClient   dynamic.Interface
}

// NewAPIValidator creates a new API compatibility validator
func NewAPIValidator(config *rest.Config, discoveryClient discovery.DiscoveryInterface) (*APIValidator, error) {
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return &APIValidator{
		discoveryClient: discoveryClient,
		dynamicClient:   dynamicClient,
	}, nil
}

// Validate performs API compatibility checks
func (a *APIValidator) Validate(ctx context.Context, objects []*unstructured.Unstructured) DomainResult {
	startTime := time.Now()
	result := DomainResult{
		Name:        APIDomainName,
		Description: "Validates API version compatibility and resource creatability on target cluster",
		Issues:      []Issue{},
	}

	// Get server API resources
	serverResources, err := a.discoveryClient.ServerPreferredResources()
	if err != nil {
		// Discovery might return partial results with error
		// We'll continue with what we have
		if serverResources == nil {
			result.Status = DomainStatusFail
			result.Issues = append(result.Issues, Issue{
				Severity:    IssueSeverityError,
				Message:     "Failed to discover server API resources",
				Details:     err.Error(),
				Remediation: "Ensure target cluster is accessible and discovery API is available",
			})
			result.Duration = time.Since(startTime).String()
			return result
		}
	}

	// Build GVK map for quick lookup
	serverGVKs := a.buildServerGVKMap(serverResources)

	// Track unique GVKs we've checked
	checkedGVKs := make(map[string]bool)
	errorCount := 0
	warnCount := 0

	for _, obj := range objects {
		gvk := obj.GroupVersionKind()
		gvkKey := gvk.String()

		// Skip if already checked
		if checkedGVKs[gvkKey] {
			continue
		}
		checkedGVKs[gvkKey] = true

		// Check 1: Is GVK available on target?
		resourceInfo, exists := serverGVKs[gvkKey]
		if !exists {
			// Try to find if the Kind exists in a different version
			alternateVersions := a.findAlternateVersions(gvk, serverGVKs)
			if len(alternateVersions) > 0 {
				result.Issues = append(result.Issues, Issue{
					Severity:    IssueSeverityWarning,
					Message:     fmt.Sprintf("API version %s not available on target", gvk.GroupVersion().String()),
					Resource:    fmt.Sprintf("%s/%s", gvk.Kind, obj.GetName()),
					Details:     fmt.Sprintf("Available versions: %v", alternateVersions),
					Remediation: "Consider updating apiVersion in manifests or use API version conversion",
				})
				warnCount++
			} else {
				result.Issues = append(result.Issues, Issue{
					Severity:    IssueSeverityError,
					Message:     fmt.Sprintf("Resource type %s not available on target cluster", gvk.Kind),
					Resource:    fmt.Sprintf("%s/%s", gvk.Kind, obj.GetName()),
					Remediation: "This resource type may require a CRD installation or may be platform-specific (e.g., OpenShift-only)",
				})
				errorCount++
			}
			continue
		}

		// Check 2: Is the API version deprecated?
		if a.isDeprecated(resourceInfo) {
			result.Issues = append(result.Issues, Issue{
				Severity:    IssueSeverityWarning,
				Message:     fmt.Sprintf("API version %s is deprecated", gvk.GroupVersion().String()),
				Resource:    fmt.Sprintf("%s/%s", gvk.Kind, obj.GetName()),
				Remediation: "Update to a non-deprecated API version before the deprecated version is removed",
			})
			warnCount++
		}

		// Check 3: Server-side dry-run validation (sample-based to avoid overload)
		// Only validate first object of each type
		if err := a.performDryRunCreate(ctx, obj, resourceInfo); err != nil {
			result.Issues = append(result.Issues, Issue{
				Severity:    IssueSeverityWarning,
				Message:     fmt.Sprintf("Dry-run validation failed for %s", gvk.Kind),
				Resource:    fmt.Sprintf("%s/%s", gvk.Kind, obj.GetName()),
				Details:     err.Error(),
				Remediation: "Review schema validation errors; resource may need adjustments for target cluster",
			})
			warnCount++
		}
	}

	// Determine overall status
	if errorCount > 0 {
		result.Status = DomainStatusFail
	} else if warnCount > 0 {
		result.Status = DomainStatusWarn
	} else {
		result.Status = DomainStatusPass
	}

	result.Duration = time.Since(startTime).String()
	return result
}

// buildServerGVKMap creates a map of GVK strings to resource info
func (a *APIValidator) buildServerGVKMap(resources []*metav1.APIResourceList) map[string]resourceInfo {
	gvkMap := make(map[string]resourceInfo)

	for _, apiResourceList := range resources {
		gv, err := schema.ParseGroupVersion(apiResourceList.GroupVersion)
		if err != nil {
			continue
		}

		for _, resource := range apiResourceList.APIResources {
			// Skip subresources
			if len(resource.Name) == 0 || resource.Name[0] == '/' {
				continue
			}

			gvk := schema.GroupVersionKind{
				Group:   gv.Group,
				Version: gv.Version,
				Kind:    resource.Kind,
			}

			gvkMap[gvk.String()] = resourceInfo{
				gvk:      gvk,
				resource: resource,
				gv:       gv,
			}
		}
	}

	return gvkMap
}

// findAlternateVersions finds other versions of the same Kind in the same Group
func (a *APIValidator) findAlternateVersions(gvk schema.GroupVersionKind, serverGVKs map[string]resourceInfo) []string {
	versions := []string{}

	for _, info := range serverGVKs {
		if info.gvk.Group == gvk.Group && info.gvk.Kind == gvk.Kind && info.gvk.Version != gvk.Version {
			versions = append(versions, info.gvk.Version)
		}
	}

	return versions
}

// isDeprecated checks if a resource is deprecated (basic heuristic)
func (a *APIValidator) isDeprecated(info resourceInfo) bool {
	// Check common deprecated API versions
	deprecatedVersions := map[string][]string{
		"apps":                      {"v1beta1", "v1beta2"},
		"extensions":                {"v1beta1"},
		"networking.k8s.io":         {"v1beta1"},
		"policy":                    {"v1beta1"},
		"rbac.authorization.k8s.io": {"v1alpha1", "v1beta1"},
		"storage.k8s.io":            {"v1beta1"},
	}

	if deprecatedList, exists := deprecatedVersions[info.gvk.Group]; exists {
		for _, depVer := range deprecatedList {
			if info.gvk.Version == depVer {
				return true
			}
		}
	}

	return false
}

// performDryRunCreate performs a server-side dry-run to validate the object
func (a *APIValidator) performDryRunCreate(ctx context.Context, obj *unstructured.Unstructured, info resourceInfo) error {
	gvr := schema.GroupVersionResource{
		Group:    info.gvk.Group,
		Version:  info.gvk.Version,
		Resource: info.resource.Name,
	}

	var resourceClient dynamic.ResourceInterface
	if info.resource.Namespaced {
		namespace := obj.GetNamespace()
		if namespace == "" {
			namespace = "default"
		}
		resourceClient = a.dynamicClient.Resource(gvr).Namespace(namespace)
	} else {
		resourceClient = a.dynamicClient.Resource(gvr)
	}

	// Perform dry-run create
	_, err := resourceClient.Create(ctx, obj, metav1.CreateOptions{
		DryRun: []string{metav1.DryRunAll},
	})

	return err
}

// resourceInfo holds information about a server resource
type resourceInfo struct {
	gvk      schema.GroupVersionKind
	resource metav1.APIResource
	gv       schema.GroupVersion
}
