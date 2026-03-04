package domains

import (
	"context"
	"fmt"
	"time"

	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	AuthDomainName = "Target Reachability & Authentication"
)

// AuthValidator validates target cluster connectivity and permissions
type AuthValidator struct {
	targetContext string
	config        *rest.Config
	client        kubernetes.Interface
}

// NewAuthValidator creates a new auth domain validator
func NewAuthValidator(targetContext string) (*AuthValidator, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{
		CurrentContext: targetContext,
	}

	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig for context %s: %w", targetContext, err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &AuthValidator{
		targetContext: targetContext,
		config:        config,
		client:        client,
	}, nil
}

// Validate performs authentication and reachability checks
func (a *AuthValidator) Validate(ctx context.Context, gvks []schema.GroupVersionKind) DomainResult {
	startTime := time.Now()
	result := DomainResult{
		Name:        AuthDomainName,
		Description: "Validates target cluster reachability, authentication, and create permissions",
		Issues:      []Issue{},
	}

	// Check 1: API server reachability and authentication
	if err := a.checkServerReachability(ctx); err != nil {
		result.Status = DomainStatusFail
		result.Issues = append(result.Issues, Issue{
			Severity:    IssueSeverityError,
			Message:     fmt.Sprintf("Failed to reach API server: %v", err),
			Remediation: "Verify kubeconfig context, network connectivity, and credentials",
		})
		result.Duration = time.Since(startTime).String()
		return result
	}

	// Check 2: Discovery access
	if err := a.checkDiscoveryAccess(ctx); err != nil {
		result.Status = DomainStatusFail
		result.Issues = append(result.Issues, Issue{
			Severity:    IssueSeverityError,
			Message:     fmt.Sprintf("Discovery access failed: %v", err),
			Remediation: "Ensure service account has discovery permissions",
		})
		result.Duration = time.Since(startTime).String()
		return result
	}

	// Check 3: Create permissions for each GVK
	permissionIssues := a.checkCreatePermissions(ctx, gvks)
	result.Issues = append(result.Issues, permissionIssues...)

	// Determine overall status
	if len(permissionIssues) > 0 {
		hasErrors := false
		for _, issue := range permissionIssues {
			if issue.Severity == IssueSeverityError {
				hasErrors = true
				break
			}
		}
		if hasErrors {
			result.Status = DomainStatusFail
		} else {
			result.Status = DomainStatusWarn
		}
	} else {
		result.Status = DomainStatusPass
	}

	result.Duration = time.Since(startTime).String()
	return result
}

// checkServerReachability verifies API server is reachable and authentication works
func (a *AuthValidator) checkServerReachability(ctx context.Context) error {
	// Try to get server version as a simple connectivity check
	_, err := a.client.Discovery().ServerVersion()
	return err
}

// checkDiscoveryAccess verifies we can access discovery APIs
func (a *AuthValidator) checkDiscoveryAccess(ctx context.Context) error {
	discoveryClient := a.client.Discovery()
	_, err := discoveryClient.ServerGroups()
	return err
}

// checkCreatePermissions verifies create permissions for all required resource types
func (a *AuthValidator) checkCreatePermissions(ctx context.Context, gvks []schema.GroupVersionKind) []Issue {
	issues := []Issue{}
	checkedResources := make(map[string]bool)

	for _, gvk := range gvks {
		// Get the resource name for this GVK
		resourceName, err := a.getResourceName(gvk)
		if err != nil {
			// Skip if we can't determine resource name
			continue
		}

		// Avoid duplicate checks
		resourceKey := fmt.Sprintf("%s.%s", resourceName, gvk.Group)
		if checkedResources[resourceKey] {
			continue
		}
		checkedResources[resourceKey] = true

		// Perform SubjectAccessReview
		sar := &authv1.SelfSubjectAccessReview{
			Spec: authv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authv1.ResourceAttributes{
					Verb:     "create",
					Group:    gvk.Group,
					Version:  gvk.Version,
					Resource: resourceName,
				},
			},
		}

		result, err := a.client.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, sar, metav1.CreateOptions{})
		if err != nil {
			issues = append(issues, Issue{
				Severity:    IssueSeverityWarning,
				Message:     fmt.Sprintf("Failed to check permissions for %s", resourceKey),
				Resource:    resourceKey,
				Details:     err.Error(),
				Remediation: "Manual verification required",
			})
			continue
		}

		if !result.Status.Allowed {
			reason := result.Status.Reason
			if reason == "" {
				reason = "RBAC permission denied"
			}
			issues = append(issues, Issue{
				Severity:    IssueSeverityError,
				Message:     fmt.Sprintf("Missing 'create' permission for %s", resourceKey),
				Resource:    resourceKey,
				Details:     reason,
				Remediation: fmt.Sprintf("Grant 'create' permission for resource type '%s' in group '%s'", resourceName, gvk.Group),
			})
		}
	}

	return issues
}

// getResourceName attempts to get the resource name for a GVK
func (a *AuthValidator) getResourceName(gvk schema.GroupVersionKind) (string, error) {
	discoveryClient := a.client.Discovery()

	// Get API resources for the group version
	apiResourceList, err := discoveryClient.ServerResourcesForGroupVersion(gvk.GroupVersion().String())
	if err != nil {
		return "", err
	}

	// Find matching resource by Kind
	for _, resource := range apiResourceList.APIResources {
		if resource.Kind == gvk.Kind {
			return resource.Name, nil
		}
	}

	return "", fmt.Errorf("resource not found for kind %s", gvk.Kind)
}

// GetDiscoveryClient returns the discovery client for other validators
func (a *AuthValidator) GetDiscoveryClient() discovery.DiscoveryInterface {
	return a.client.Discovery()
}

// GetKubernetesClient returns the kubernetes client for other validators
func (a *AuthValidator) GetKubernetesClient() kubernetes.Interface {
	return a.client
}

// GetRestConfig returns the rest config for advanced use cases
func (a *AuthValidator) GetRestConfig() *rest.Config {
	return a.config
}
