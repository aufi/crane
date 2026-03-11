package domains

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
)

const (
	CapacityDomainName = "Resource Capacity"
)

// CapacityValidator validates resource capacity and sizing
type CapacityValidator struct {
	client kubernetes.Interface
}

// NewCapacityValidator creates a new capacity validator
func NewCapacityValidator(client kubernetes.Interface) (*CapacityValidator, error) {
	return &CapacityValidator{
		client: client,
	}, nil
}

// Validate performs capacity checks
func (c *CapacityValidator) Validate(ctx context.Context, objects []*unstructured.Unstructured, storageClassMap map[string]string) DomainResult {
	startTime := time.Now()
	result := DomainResult{
		Name:        CapacityDomainName,
		Description: "Validates resource capacity including storage, CPU, and memory requirements",
		Issues:      []Issue{},
	}

	// Aggregate resource requirements
	requirements := c.aggregateRequirements(objects)

	// Check 1: Storage capacity
	storageIssues := c.validateStorageCapacity(ctx, objects, storageClassMap)
	result.Issues = append(result.Issues, storageIssues...)

	// Check 2: Compute capacity (CPU/Memory)
	computeIssues := c.validateComputeCapacity(ctx, requirements)
	result.Issues = append(result.Issues, computeIssues...)

	// Check 3: Namespace quotas
	quotaIssues := c.validateNamespaceQuotas(ctx, objects)
	result.Issues = append(result.Issues, quotaIssues...)

	// Determine overall status
	errorCount := 0
	warnCount := 0
	for _, issue := range result.Issues {
		if issue.Severity == IssueSeverityError {
			errorCount++
		} else if issue.Severity == IssueSeverityWarning {
			warnCount++
		}
	}

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

// ResourceRequirements holds aggregated resource requirements
type ResourceRequirements struct {
	TotalCPURequest    resource.Quantity
	TotalMemoryRequest resource.Quantity
	TotalCPULimit      resource.Quantity
	TotalMemoryLimit   resource.Quantity
	PVCsByNamespace    map[string][]PVCInfo
}

type PVCInfo struct {
	Name             string
	Namespace        string
	StorageClassName string
	StorageRequest   resource.Quantity
}

func (c *CapacityValidator) aggregateRequirements(objects []*unstructured.Unstructured) *ResourceRequirements {
	req := &ResourceRequirements{
		TotalCPURequest:    resource.Quantity{},
		TotalMemoryRequest: resource.Quantity{},
		TotalCPULimit:      resource.Quantity{},
		TotalMemoryLimit:   resource.Quantity{},
		PVCsByNamespace:    make(map[string][]PVCInfo),
	}

	for _, obj := range objects {
		gvk := obj.GroupVersionKind()

		// Handle PVCs
		if gvk.Kind == "PersistentVolumeClaim" {
			pvcInfo := c.extractPVCInfo(obj)
			if pvcInfo != nil {
				req.PVCsByNamespace[pvcInfo.Namespace] = append(req.PVCsByNamespace[pvcInfo.Namespace], *pvcInfo)
			}
		}

		// Handle workloads with resource requests/limits
		if c.isWorkload(gvk.Kind) {
			c.aggregateWorkloadResources(obj, req)
		}
	}

	return req
}

func (c *CapacityValidator) isWorkload(kind string) bool {
	workloadKinds := map[string]bool{
		"Pod":                   true,
		"Deployment":            true,
		"StatefulSet":           true,
		"DaemonSet":             true,
		"ReplicaSet":            true,
		"Job":                   true,
		"CronJob":               true,
		"DeploymentConfig":      true, // OpenShift
	}
	return workloadKinds[kind]
}

func (c *CapacityValidator) extractPVCInfo(obj *unstructured.Unstructured) *PVCInfo {
	spec, found, err := unstructured.NestedMap(obj.Object, "spec")
	if !found || err != nil {
		return nil
	}

	storageClassName, _, _ := unstructured.NestedString(spec, "storageClassName")

	resources, found, err := unstructured.NestedMap(spec, "resources")
	if !found || err != nil {
		return nil
	}

	requests, found, err := unstructured.NestedMap(resources, "requests")
	if !found || err != nil {
		return nil
	}

	storageStr, found, err := unstructured.NestedString(requests, "storage")
	if !found || err != nil {
		return nil
	}

	storage, err := resource.ParseQuantity(storageStr)
	if err != nil {
		return nil
	}

	return &PVCInfo{
		Name:             obj.GetName(),
		Namespace:        obj.GetNamespace(),
		StorageClassName: storageClassName,
		StorageRequest:   storage,
	}
}

func (c *CapacityValidator) aggregateWorkloadResources(obj *unstructured.Unstructured, req *ResourceRequirements) {
	// Extract pod template spec based on kind
	var containers []interface{}
	var err error

	kind := obj.GetKind()
	switch kind {
	case "Pod":
		containers, _, err = unstructured.NestedSlice(obj.Object, "spec", "containers")
	case "CronJob":
		containers, _, err = unstructured.NestedSlice(obj.Object, "spec", "jobTemplate", "spec", "template", "spec", "containers")
	default:
		// Deployment, StatefulSet, DaemonSet, etc.
		containers, _, err = unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
	}

	if err != nil || containers == nil {
		return
	}

	for _, c := range containers {
		container, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		resources, found, _ := unstructured.NestedMap(container, "resources")
		if !found {
			continue
		}

		// Add requests
		if requests, found, _ := unstructured.NestedMap(resources, "requests"); found {
			if cpu, found, _ := unstructured.NestedString(requests, "cpu"); found {
				if cpuQty, err := resource.ParseQuantity(cpu); err == nil {
					req.TotalCPURequest.Add(cpuQty)
				}
			}
			if memory, found, _ := unstructured.NestedString(requests, "memory"); found {
				if memQty, err := resource.ParseQuantity(memory); err == nil {
					req.TotalMemoryRequest.Add(memQty)
				}
			}
		}

		// Add limits
		if limits, found, _ := unstructured.NestedMap(resources, "limits"); found {
			if cpu, found, _ := unstructured.NestedString(limits, "cpu"); found {
				if cpuQty, err := resource.ParseQuantity(cpu); err == nil {
					req.TotalCPULimit.Add(cpuQty)
				}
			}
			if memory, found, _ := unstructured.NestedString(limits, "memory"); found {
				if memQty, err := resource.ParseQuantity(memory); err == nil {
					req.TotalMemoryLimit.Add(memQty)
				}
			}
		}
	}
}

func (c *CapacityValidator) validateStorageCapacity(ctx context.Context, objects []*unstructured.Unstructured, storageClassMap map[string]string) []Issue {
	issues := []Issue{}

	// Get available storage classes
	storageClasses, err := c.client.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		issues = append(issues, Issue{
			Severity:    IssueSeverityWarning,
			Message:     "Failed to list storage classes on target",
			Details:     err.Error(),
			Remediation: "Manual verification of storage capacity required",
		})
		return issues
	}

	// Build storage class map
	availableClasses := make(map[string]bool)
	for _, sc := range storageClasses.Items {
		availableClasses[sc.Name] = true
	}

	// Check PVCs
	for _, obj := range objects {
		if obj.GetKind() != "PersistentVolumeClaim" {
			continue
		}

		pvcInfo := c.extractPVCInfo(obj)
		if pvcInfo == nil {
			continue
		}

		targetClass := pvcInfo.StorageClassName
		if mapped, exists := storageClassMap[pvcInfo.StorageClassName]; exists {
			targetClass = mapped
		}

		if targetClass != "" && !availableClasses[targetClass] {
			issues = append(issues, Issue{
				Severity: IssueSeverityError,
				Message:  fmt.Sprintf("Storage class '%s' not available on target", targetClass),
				Resource: fmt.Sprintf("PVC/%s/%s", pvcInfo.Namespace, pvcInfo.Name),
				Details:  fmt.Sprintf("Requested: %s", pvcInfo.StorageRequest.String()),
				Remediation: fmt.Sprintf("Create storage class '%s' on target or use --storage-class-map to map to an existing class", targetClass),
			})
		}
	}

	return issues
}

func (c *CapacityValidator) validateComputeCapacity(ctx context.Context, req *ResourceRequirements) []Issue {
	issues := []Issue{}

	// Get node list to calculate total allocatable
	nodes, err := c.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		issues = append(issues, Issue{
			Severity:    IssueSeverityWarning,
			Message:     "Failed to list nodes on target cluster",
			Details:     err.Error(),
			Remediation: "Manual verification of compute capacity required",
		})
		return issues
	}

	totalAllocatableCPU := resource.Quantity{}
	totalAllocatableMemory := resource.Quantity{}

	for _, node := range nodes.Items {
		if cpu, exists := node.Status.Allocatable[corev1.ResourceCPU]; exists {
			totalAllocatableCPU.Add(cpu)
		}
		if memory, exists := node.Status.Allocatable[corev1.ResourceMemory]; exists {
			totalAllocatableMemory.Add(memory)
		}
	}

	// Check CPU capacity
	if !req.TotalCPURequest.IsZero() {
		if req.TotalCPURequest.Cmp(totalAllocatableCPU) > 0 {
			issues = append(issues, Issue{
				Severity: IssueSeverityError,
				Message:  "Insufficient CPU capacity on target cluster",
				Details: fmt.Sprintf("Required: %s, Available: %s",
					req.TotalCPURequest.String(), totalAllocatableCPU.String()),
				Remediation: "Add more nodes or reduce CPU requests",
			})
		} else if req.TotalCPURequest.AsApproximateFloat64() > totalAllocatableCPU.AsApproximateFloat64()*0.8 {
			issues = append(issues, Issue{
				Severity: IssueSeverityWarning,
				Message:  "CPU capacity may be tight (>80% utilization)",
				Details: fmt.Sprintf("Required: %s, Available: %s",
					req.TotalCPURequest.String(), totalAllocatableCPU.String()),
				Remediation: "Consider adding buffer capacity for future scaling",
			})
		}
	}

	// Check Memory capacity
	if !req.TotalMemoryRequest.IsZero() {
		if req.TotalMemoryRequest.Cmp(totalAllocatableMemory) > 0 {
			issues = append(issues, Issue{
				Severity: IssueSeverityError,
				Message:  "Insufficient memory capacity on target cluster",
				Details: fmt.Sprintf("Required: %s, Available: %s",
					req.TotalMemoryRequest.String(), totalAllocatableMemory.String()),
				Remediation: "Add more nodes or reduce memory requests",
			})
		} else if req.TotalMemoryRequest.AsApproximateFloat64() > totalAllocatableMemory.AsApproximateFloat64()*0.8 {
			issues = append(issues, Issue{
				Severity: IssueSeverityWarning,
				Message:  "Memory capacity may be tight (>80% utilization)",
				Details: fmt.Sprintf("Required: %s, Available: %s",
					req.TotalMemoryRequest.String(), totalAllocatableMemory.String()),
				Remediation: "Consider adding buffer capacity for future scaling",
			})
		}
	}

	return issues
}

func (c *CapacityValidator) validateNamespaceQuotas(ctx context.Context, objects []*unstructured.Unstructured) []Issue {
	issues := []Issue{}

	// Group objects by namespace
	namespaceCounts := make(map[string]int)
	for _, obj := range objects {
		ns := obj.GetNamespace()
		if ns != "" {
			namespaceCounts[ns]++
		}
	}

	// Check quotas for each namespace
	for ns := range namespaceCounts {
		quotas, err := c.client.CoreV1().ResourceQuotas(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			// Namespace might not exist yet, which is expected
			continue
		}

		if len(quotas.Items) > 0 {
			// Found quotas - add warning since we can't fully validate without details
			for _, quota := range quotas.Items {
				issues = append(issues, Issue{
					Severity: IssueSeverityWarning,
					Message:  fmt.Sprintf("ResourceQuota exists in namespace '%s'", ns),
					Resource: fmt.Sprintf("ResourceQuota/%s", quota.Name),
					Details:  "Manual verification required to ensure migrated resources fit within quota",
					Remediation: fmt.Sprintf("Review quota limits: %s", c.formatQuotaSpec(quota.Spec.Hard)),
				})
			}
		}
	}

	return issues
}

func (c *CapacityValidator) formatQuotaSpec(hard corev1.ResourceList) string {
	parts := []string{}
	for key, value := range hard {
		parts = append(parts, fmt.Sprintf("%s=%s", key, value.String()))
	}
	return strings.Join(parts, ", ")
}

// parseStorageClassMap converts CLI string slice to map
func ParseStorageClassMap(mapStrings []string) map[string]string {
	result := make(map[string]string)
	for _, mapping := range mapStrings {
		parts := strings.Split(mapping, "=")
		if len(parts) == 2 {
			result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return result
}

// Helper to parse size quantities for comparison
func parseSize(sizeStr string) (int64, error) {
	qty, err := resource.ParseQuantity(sizeStr)
	if err != nil {
		return 0, err
	}
	return qty.Value(), nil
}

// Helper to format bytes as human readable
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// Helper to convert string to int64
func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
