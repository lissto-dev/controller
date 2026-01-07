/*
Copyright 2025 Lissto.

Licensed under the Sustainable Use License, Version 1.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://github.com/lissto-dev/controller/blob/main/LICENSE.md

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	"github.com/lissto-dev/controller/internal/controller/snapshot"
	"github.com/lissto-dev/controller/pkg/config"
)

const stackFinalizerName = "stack.lissto.dev/config-cleanup"

// StackReconciler reconciles a Stack object
type StackReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Config   *config.Config
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=env.lissto.dev,resources=stacks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=env.lissto.dev,resources=stacks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=env.lissto.dev,resources=stacks/finalizers,verbs=update
// +kubebuilder:rbac:groups=env.lissto.dev,resources=lisstovariables,verbs=get;list;watch
// +kubebuilder:rbac:groups=env.lissto.dev,resources=lisstosecrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=env.lissto.dev,resources=blueprints,verbs=get;list;watch
// +kubebuilder:rbac:groups=env.lissto.dev,resources=volumesnapshots,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Stack object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.1/pkg/reconcile
func (r *StackReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the Stack instance
	stack := &envv1alpha1.Stack{}
	if err := r.Get(ctx, req.NamespacedName, stack); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling Stack", "name", stack.Name, "namespace", stack.Namespace)

	// Handle finalizer for cleanup
	if stack.DeletionTimestamp.IsZero() {
		// Stack is not being deleted, ensure finalizer is present
		if !controllerutil.ContainsFinalizer(stack, stackFinalizerName) {
			controllerutil.AddFinalizer(stack, stackFinalizerName)
			if err := r.Update(ctx, stack); err != nil {
				log.Error(err, "Failed to add finalizer")
				return ctrl.Result{}, err
			}
			log.Info("Added finalizer to Stack")
		}
	} else {
		// Stack is being deleted, run cleanup
		if controllerutil.ContainsFinalizer(stack, stackFinalizerName) {
			// Run comprehensive cleanup as safety net
			// Owner references should handle most cleanup, but this ensures everything is gone
			if err := r.cleanupStackResources(ctx, stack); err != nil {
				log.Error(err, "Failed to cleanup stack resources")
				return ctrl.Result{}, err
			}

			// Remove finalizer
			controllerutil.RemoveFinalizer(stack, stackFinalizerName)
			if err := r.Update(ctx, stack); err != nil {
				log.Error(err, "Failed to remove finalizer")
				return ctrl.Result{}, err
			}
			log.Info("Removed finalizer from Stack")
		}
		// Stop reconciliation as the Stack is being deleted
		return ctrl.Result{}, nil
	}

	// Fetch manifests from ConfigMap
	configMapName := stack.Spec.ManifestsConfigMapRef
	if configMapName == "" {
		log.Error(nil, "Stack has no manifests ConfigMap reference")
		return ctrl.Result{}, nil
	}

	manifests, err := r.fetchManifests(ctx, stack.Namespace, configMapName)
	if err != nil {
		log.Error(err, "Failed to fetch manifests from ConfigMap")
		r.updateStackStatus(ctx, stack, nil, nil, nil, fmt.Errorf("failed to fetch manifests: %w", err))
		return ctrl.Result{}, err
	}

	// Parse manifests into Kubernetes objects
	objects, err := parseManifests(manifests)
	if err != nil {
		log.Error(err, "Failed to parse manifests")
		r.updateStackStatus(ctx, stack, nil, nil, nil, fmt.Errorf("failed to parse manifests: %w", err))
		return ctrl.Result{}, err
	}

	log.Info("Parsed manifests", "count", len(objects))

	// Fetch blueprint for config discovery
	blueprint, err := r.getBlueprint(ctx, stack)
	if err != nil {
		log.Error(err, "Failed to fetch blueprint for config discovery")
		// Continue without config injection - not fatal
	}

	// Discover and inject config (variables and secrets)
	var configResult *ConfigInjectionResult
	if blueprint != nil {
		variables, _ := r.discoverVariables(ctx, stack, blueprint)
		secrets, _ := r.discoverSecrets(ctx, stack, blueprint)

		mergedVars := r.resolveVariables(variables)
		resolvedKeys := r.resolveSecretKeys(secrets)

		// Copy secrets to stack namespace
		stackSecretName := ""
		var missingSecretKeys map[string][]string
		if len(resolvedKeys) > 0 {
			stackSecret, missing, err := r.copySecretsToStackNamespace(ctx, stack, resolvedKeys)
			missingSecretKeys = missing
			if err != nil {
				log.Error(err, "Failed to copy secrets to stack namespace")
			} else if stackSecret != nil {
				stackSecretName = stackSecret.Name
			}

			// Emit events for missing keys
			if len(missingSecretKeys) > 0 {
				for secretRef, keys := range missingSecretKeys {
					r.Recorder.Eventf(stack, corev1.EventTypeWarning, "MissingSecretKeys",
						"Secret %s is missing keys: %v", secretRef, keys)
				}
			}
		}

		// Inject config into workloads (deployments and pods)
		// Extract stack metadata for injection
		stackMetadata := extractStackMetadata(stack)
		configResult = r.injectConfigIntoWorkloads(ctx, objects, mergedVars, resolvedKeys, stackSecretName, stackMetadata)
		if configResult != nil {
			configResult.MissingSecretKeys = missingSecretKeys

			log.Info("Config injection complete",
				"variables", configResult.VariablesInjected,
				"secrets", configResult.SecretsInjected,
				"metadata", configResult.MetadataInjected,
				"missingKeys", len(missingSecretKeys))

			// Emit events for config injection warnings (e.g., non-existent key mappings)
			for _, warning := range configResult.Warnings {
				r.Recorder.Eventf(stack, corev1.EventTypeWarning, "ConfigInjectionWarning", warning)
			}
		}
	}

	// Inject images from Stack spec into deployments
	imageWarnings := r.injectImages(ctx, objects, stack.Spec.Images)

	// Separate PVCs from other resources for restore flow
	var pvcObjects, otherObjects []*unstructured.Unstructured
	for _, obj := range objects {
		if obj.GetKind() == "PersistentVolumeClaim" {
			pvcObjects = append(pvcObjects, obj)
		} else {
			otherObjects = append(otherObjects, obj)
		}
	}

	// Apply PVCs first
	pvcResults := r.applyResources(ctx, pvcObjects, stack.Namespace)
	r.setOwnerReferences(ctx, stack, pvcObjects, pvcResults)

	// Handle volume restore if needed
	if r.needsVolumeRestore(stack) {
		restoreComplete, restoreResults, err := r.restoreVolumes(ctx, stack, pvcObjects)
		if err != nil {
			log.Error(err, "Failed to restore volumes")
			r.Recorder.Eventf(stack, corev1.EventTypeWarning, "RestoreFailed",
				"Failed to restore volumes: %v", err)
		}

		// Log restore results
		for _, result := range restoreResults {
			if result.Error != nil {
				log.Error(result.Error, "Volume restore failed",
					"service", result.Service,
					"snapshot", result.SnapshotName)
			}
		}

		if !restoreComplete {
			log.Info("Volume restore in progress, requeuing", "results", len(restoreResults))
			// Requeue to check restore status
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		log.Info("Volume restore complete", "results", len(restoreResults))
	}

	// Apply other resources (deployments, services, etc.)
	otherResults := r.applyResources(ctx, otherObjects, stack.Namespace)
	r.setOwnerReferences(ctx, stack, otherObjects, otherResults)

	// Combine results for status update
	results := append(pvcResults, otherResults...)

	// Update Stack status with results
	r.updateStackStatus(ctx, stack, results, imageWarnings, configResult, nil)

	log.Info("Stack reconciliation complete", "name", stack.Name)
	return ctrl.Result{}, nil
}

// ResourceResult tracks application result
type ResourceResult struct {
	Kind    string
	Name    string
	Applied bool
	Error   error
}

// ImageWarning tracks image injection issues
type ImageWarning struct {
	Service         string
	Deployment      string
	TargetContainer string
	Message         string
}

// fetchManifests retrieves manifests from ConfigMap
func (r *StackReconciler) fetchManifests(ctx context.Context, namespace, configMapName string) (string, error) {
	configMap := &corev1.ConfigMap{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      configMapName,
	}, configMap); err != nil {
		return "", err
	}

	manifests, ok := configMap.Data["manifests.yaml"]
	if !ok {
		return "", fmt.Errorf("manifests.yaml not found in ConfigMap")
	}

	return manifests, nil
}

// injectImages updates deployment and pod container images from Stack spec
func (r *StackReconciler) injectImages(ctx context.Context, objects []*unstructured.Unstructured, images map[string]envv1alpha1.ImageInfo) []ImageWarning {
	log := logf.FromContext(ctx)
	var warnings []ImageWarning

	for _, obj := range objects {
		// Support both Deployment and Pod
		if obj.GetKind() != kindDeployment && obj.GetKind() != kindPod {
			continue
		}

		resourceKind := obj.GetKind()
		resourceName := obj.GetName()

		// Get containers path based on resource type
		containersPath := getContainersPath(resourceKind)

		// Get the service name from the resource's labels
		// The service name is stored in the "io.kompose.service" label
		labels := obj.GetLabels()
		serviceName, ok := labels["io.kompose.service"]
		if !ok {
			log.V(1).Info("Resource missing io.kompose.service label, skipping image injection",
				"kind", resourceKind,
				"name", resourceName)
			warnings = append(warnings, ImageWarning{
				Deployment: resourceName,
				Message:    fmt.Sprintf("%s missing io.kompose.service label", resourceKind),
			})
			continue
		}

		// Look up image by service name
		imageInfo, exists := images[serviceName]
		if !exists {
			log.V(1).Info("No image found for service",
				"kind", resourceKind,
				"name", resourceName,
				"service", serviceName)
			warnings = append(warnings, ImageWarning{
				Service:    serviceName,
				Deployment: resourceName,
				Message:    "No image specification found for service",
			})
			continue
		}

		// Get main containers (not init containers)
		containers, found, err := unstructured.NestedSlice(obj.Object, containersPath...)
		if err != nil || !found {
			log.Error(err, "Failed to get containers",
				"kind", resourceKind,
				"name", resourceName)
			warnings = append(warnings, ImageWarning{
				Service:    serviceName,
				Deployment: resourceName,
				Message:    fmt.Sprintf("Failed to get containers: %v", err),
			})
			continue
		}

		// Determine target container name
		targetContainerName := imageInfo.ContainerName
		if targetContainerName == "" {
			targetContainerName = serviceName // Default to service name
		}

		// Update matching containers
		updated := false
		for i, container := range containers {
			containerMap, ok := container.(map[string]interface{})
			if !ok {
				continue
			}

			containerName, _, _ := unstructured.NestedString(containerMap, "name")

			// Match by target container name
			if containerName == targetContainerName {
				containerMap["image"] = imageInfo.Digest
				containers[i] = containerMap
				updated = true

				log.Info("Injected image",
					"kind", resourceKind,
					"name", resourceName,
					"service", serviceName,
					"container", containerName,
					"image", imageInfo.Digest)
			}
		}

		if !updated {
			log.Info("No matching container found for image injection",
				"kind", resourceKind,
				"name", resourceName,
				"service", serviceName,
				"targetContainer", targetContainerName)
			warnings = append(warnings, ImageWarning{
				Service:         serviceName,
				Deployment:      resourceName,
				TargetContainer: targetContainerName,
				Message:         fmt.Sprintf("No container named '%s' found in %s", targetContainerName, resourceKind),
			})
		}

		// Write back updated containers
		if updated {
			if err := unstructured.SetNestedSlice(obj.Object, containers, containersPath...); err != nil {
				log.Error(err, "Failed to set containers",
					"kind", resourceKind,
					"name", resourceName)
				warnings = append(warnings, ImageWarning{
					Service:    serviceName,
					Deployment: resourceName,
					Message:    fmt.Sprintf("Failed to update containers: %v", err),
				})
			}
		}
	}

	return warnings
}

// applyResources applies all resources to cluster using server-side apply
func (r *StackReconciler) applyResources(ctx context.Context, objects []*unstructured.Unstructured, namespace string) []ResourceResult {
	log := logf.FromContext(ctx)
	results := make([]ResourceResult, 0, len(objects))

	for _, obj := range objects {
		result := ResourceResult{
			Kind: obj.GetKind(),
			Name: obj.GetName(),
		}

		// Ensure the namespace is set on the object
		obj.SetNamespace(namespace)

		// Special handling for PVCs - they are immutable after creation
		if obj.GetKind() == "PersistentVolumeClaim" {
			// Check if PVC already exists
			existing := &unstructured.Unstructured{}
			existing.SetGroupVersionKind(obj.GroupVersionKind())
			err := r.Get(ctx, client.ObjectKeyFromObject(obj), existing)

			if err != nil {
				// PVC doesn't exist, create it
				if err := r.Create(ctx, obj); err != nil {
					log.Error(err, "Failed to create PVC",
						"name", obj.GetName())
					result.Applied = false
					result.Error = err
				} else {
					log.Info("Created PVC",
						"name", obj.GetName())
					result.Applied = true
				}
			} else {
				// PVC exists, skip update (immutable)
				log.Info("PVC already exists, skipping update (immutable)",
					"name", obj.GetName())
				result.Applied = true
			}
		} else {
			// For other resources, use server-side apply with proper field management
			// This allows proper updates and field ownership tracking
			if err := r.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("stack-controller")); err != nil {
				log.Error(err, "Failed to apply resource",
					"kind", obj.GetKind(),
					"name", obj.GetName())
				result.Applied = false
				result.Error = err
			} else {
				log.V(1).Info("Applied resource",
					"kind", obj.GetKind(),
					"name", obj.GetName())
				result.Applied = true
			}
		}

		results = append(results, result)
	}

	return results
}

// setOwnerReferences sets Stack as owner of all successfully applied resources
func (r *StackReconciler) setOwnerReferences(ctx context.Context, stack *envv1alpha1.Stack,
	objects []*unstructured.Unstructured, results []ResourceResult) {
	log := logf.FromContext(ctx)

	for i, result := range results {
		if !result.Applied {
			continue
		}

		// Fetch the latest version of the resource to avoid race conditions
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(objects[i].GroupVersionKind())
		obj.SetName(objects[i].GetName())
		obj.SetNamespace(objects[i].GetNamespace())

		if err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			log.Error(err, "Failed to fetch resource for owner reference",
				"kind", obj.GetKind(),
				"name", obj.GetName())
			continue
		}

		// Check if owner reference already exists
		ownerRefs := obj.GetOwnerReferences()
		hasOwnerRef := false
		for _, ref := range ownerRefs {
			if ref.UID == stack.UID {
				log.Info("Owner reference already exists",
					"kind", obj.GetKind(),
					"name", obj.GetName())
				hasOwnerRef = true
				break
			}
		}

		// Skip setting owner reference if it already exists
		if hasOwnerRef {
			continue
		}

		if err := controllerutil.SetOwnerReference(stack, obj, r.Scheme); err != nil {
			log.Error(err, "Failed to set owner reference",
				"kind", obj.GetKind(),
				"name", obj.GetName())
			continue
		}

		if err := r.Update(ctx, obj); err != nil {
			// Check if it's a conflict error (optimistic locking)
			if apierrors.IsConflict(err) {
				// Conflict errors are expected and will be retried - log at debug level
				log.V(1).Info("Conflict updating resource with owner reference, will retry",
					"kind", obj.GetKind(),
					"name", obj.GetName())
			} else {
				// Real errors should be logged
				log.Error(err, "Failed to update resource with owner reference",
					"kind", obj.GetKind(),
					"name", obj.GetName())
			}
		} else {
			log.Info("Set owner reference",
				"kind", obj.GetKind(),
				"name", obj.GetName())
		}
	}
}

// updateStackStatus updates Stack conditions based on resource application results
func (r *StackReconciler) updateStackStatus(ctx context.Context, stack *envv1alpha1.Stack,
	results []ResourceResult, imageWarnings []ImageWarning, configResult *ConfigInjectionResult, parseError error) {
	log := logf.FromContext(ctx)

	conditions := []metav1.Condition{}

	// Handle parse error
	if parseError != nil {
		conditions = append(conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: stack.Generation,
			LastTransitionTime: metav1.Now(),
			Reason:             "ParseFailed",
			Message:            parseError.Error(),
		})

		stack.Status.Conditions = conditions
		// Update ObservedGeneration even on parse failure
		stack.Status.ObservedGeneration = stack.Generation
		if err := r.Status().Update(ctx, stack); err != nil {
			log.Error(err, "Failed to update Stack status")
		}
		return
	}

	// Handle image injection warnings
	if len(imageWarnings) > 0 {
		// Create warning messages
		warningMessages := make([]string, 0, len(imageWarnings))
		for _, warning := range imageWarnings {
			if warning.Service != "" {
				warningMessages = append(warningMessages,
					fmt.Sprintf("Service '%s' (deployment '%s'): %s", warning.Service, warning.Deployment, warning.Message))
			} else {
				warningMessages = append(warningMessages,
					fmt.Sprintf("Deployment '%s': %s", warning.Deployment, warning.Message))
			}
		}

		conditions = append(conditions, metav1.Condition{
			Type:               "ImageInjection",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: stack.Generation,
			LastTransitionTime: metav1.Now(),
			Reason:             "ImageInjectionWarning",
			Message:            strings.Join(warningMessages, "; "),
		})
	} else {
		// All images injected successfully
		conditions = append(conditions, metav1.Condition{
			Type:               "ImageInjection",
			Status:             metav1.ConditionTrue,
			ObservedGeneration: stack.Generation,
			LastTransitionTime: metav1.Now(),
			Reason:             "AllImagesInjected",
			Message:            "All service images injected successfully",
		})
	}

	// Handle config injection result
	if configResult != nil {
		hasMissingKeys := len(configResult.MissingSecretKeys) > 0
		hasWarnings := len(configResult.Warnings) > 0

		if hasMissingKeys || hasWarnings {
			// Build warning message
			var messages []string

			if hasMissingKeys {
				keyMsgs := []string{}
				for secretRef, keys := range configResult.MissingSecretKeys {
					keyMsgs = append(keyMsgs, fmt.Sprintf("%s[%v]", secretRef, keys))
				}
				messages = append(messages, "Missing secret keys: "+strings.Join(keyMsgs, ", "))
			}

			if hasWarnings {
				messages = append(messages, configResult.Warnings...)
			}

			reason := "ConfigInjectionWarning"
			if hasMissingKeys {
				reason = "MissingSecretKeys"
			}

			conditions = append(conditions, metav1.Condition{
				Type:               "ConfigInjection",
				Status:             metav1.ConditionFalse,
				ObservedGeneration: stack.Generation,
				LastTransitionTime: metav1.Now(),
				Reason:             reason,
				Message:            strings.Join(messages, "; "),
			})
		} else if configResult.VariablesInjected > 0 || configResult.SecretsInjected > 0 {
			// Config injected successfully
			conditions = append(conditions, metav1.Condition{
				Type:               "ConfigInjection",
				Status:             metav1.ConditionTrue,
				ObservedGeneration: stack.Generation,
				LastTransitionTime: metav1.Now(),
				Reason:             "ConfigInjected",
				Message:            fmt.Sprintf("Injected %d variables and %d secrets", configResult.VariablesInjected, configResult.SecretsInjected),
			})
		}
	}

	// Track resource conditions
	hasFailures := false
	for _, result := range results {
		// Use a valid Kubernetes condition type format (lowercase, no slashes)
		conditionType := fmt.Sprintf("Resource-%s-%s", strings.ToLower(result.Kind), result.Name)

		if result.Applied {
			conditions = append(conditions, metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: stack.Generation,
				LastTransitionTime: metav1.Now(),
				Reason:             "Applied",
				Message:            "Resource applied successfully",
			})
		} else {
			hasFailures = true
			conditions = append(conditions, metav1.Condition{
				Type:               conditionType,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: stack.Generation,
				LastTransitionTime: metav1.Now(),
				Reason:             "Failed",
				Message:            result.Error.Error(),
			})
		}
	}

	// Set overall Ready condition
	readyCondition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: stack.Generation,
		LastTransitionTime: metav1.Now(),
	}

	if hasFailures {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = "PartialFailure"
		readyCondition.Message = "Some resources failed to apply"
	} else {
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = "AllResourcesApplied"
		readyCondition.Message = fmt.Sprintf("All %d resources applied successfully", len(results))
	}

	conditions = append([]metav1.Condition{readyCondition}, conditions...)
	stack.Status.Conditions = conditions

	// Update ObservedGeneration to indicate this spec version was reconciled
	stack.Status.ObservedGeneration = stack.Generation

	if err := r.Status().Update(ctx, stack); err != nil {
		log.Error(err, "Failed to update Stack status")
	}
}

// cleanupStackResources performs comprehensive cleanup of all Stack resources
// This acts as a safety net in case owner references didn't work properly
// cleanupStackResources cleans up resources owned by the Stack during deletion.
// It intentionally returns nil even on partial failures to allow Stack deletion to proceed.
//
//nolint:unparam // error return is kept for interface consistency and future use
func (r *StackReconciler) cleanupStackResources(ctx context.Context, stack *envv1alpha1.Stack) error {
	log := logf.FromContext(ctx)

	// List of resource types to cleanup (in deletion order)
	// Owner references should have handled most of these, but we verify and cleanup as needed
	resourceTypes := []struct {
		name string
		list client.ObjectList
	}{
		// Clean up workloads first (they may have finalizers)
		{"Pods", &corev1.PodList{}},
		{"Deployments", &appsv1.DeploymentList{}},

		// Then networking
		{"Ingresses", &networkingv1.IngressList{}},
		{"Services", &corev1.ServiceList{}},

		// Finally storage (in case workloads are holding them)
		{"PersistentVolumeClaims", &corev1.PersistentVolumeClaimList{}},

		// Secrets managed by controller
		{"Secrets", &corev1.SecretList{}},
	}

	errorCount := 0
	deletedCount := 0

	for _, rt := range resourceTypes {
		// List resources with Stack label (if they have it) or in Stack namespace
		listOpts := []client.ListOption{
			client.InNamespace(stack.Namespace),
		}

		if err := r.List(ctx, rt.list, listOpts...); err != nil {
			log.Error(err, "Failed to list resources during cleanup", "type", rt.name)
			errorCount++
			continue
		}

		// Extract items from the list
		items, err := meta.ExtractList(rt.list)
		if err != nil {
			log.Error(err, "Failed to extract list items", "type", rt.name)
			errorCount++
			continue
		}

		// Check each resource to see if it's owned by this Stack
		for _, item := range items {
			obj, ok := item.(client.Object)
			if !ok {
				continue
			}

			// Check if this resource is owned by the Stack
			isOwned := false
			for _, ref := range obj.GetOwnerReferences() {
				if ref.UID == stack.UID {
					isOwned = true
					break
				}
			}

			// Also check for managed-by label (for secrets)
			labels := obj.GetLabels()
			if labels != nil && labels["lissto.dev/stack"] == stack.Name {
				isOwned = true
			}

			if !isOwned {
				continue
			}

			// Delete the resource
			if err := r.Delete(ctx, obj); err != nil {
				if !apierrors.IsNotFound(err) {
					log.Error(err, "Failed to delete resource during cleanup",
						"type", rt.name,
						"name", obj.GetName())
					errorCount++
				}
			} else {
				log.Info("Deleted resource during cleanup",
					"type", rt.name,
					"name", obj.GetName())
				deletedCount++
			}
		}
	}

	if deletedCount > 0 {
		log.Info("Cleaned up Stack resources via finalizer",
			"deleted", deletedCount,
			"errors", errorCount,
			"note", "Most resources should be auto-deleted by owner references")
	}

	// Don't fail the finalizer if some resources couldn't be deleted
	// Log errors but allow Stack deletion to proceed
	if errorCount > 0 {
		log.Error(nil, "Some resources failed to delete, but allowing Stack deletion to proceed",
			"errorCount", errorCount)
	}

	return nil
}

// cleanupConfigSecrets cleans up orphaned config secrets that may not have owner references
// Deprecated: Use cleanupStackResources instead
func (r *StackReconciler) cleanupConfigSecrets(ctx context.Context, stack *envv1alpha1.Stack) error {
	log := logf.FromContext(ctx)

	// Find all secrets with our managed-by label
	secretList := &corev1.SecretList{}
	if err := r.List(ctx, secretList,
		client.InNamespace(stack.Namespace),
		client.MatchingLabels{
			"lissto.dev/managed-by": "stack-controller",
			"lissto.dev/stack":      stack.Name,
		}); err != nil {
		return fmt.Errorf("failed to list managed secrets: %w", err)
	}

	// Delete all managed secrets for this stack
	for i := range secretList.Items {
		secret := &secretList.Items[i]
		if err := r.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
			log.Error(err, "Failed to delete config secret", "secret", secret.Name)
			// Continue trying to delete others
		} else {
			log.Info("Deleted config secret", "secret", secret.Name)
		}
	}

	log.Info("Cleaned up config secrets", "count", len(secretList.Items))
	return nil
}

// RestoreResult tracks the result of a volume restore operation
type RestoreResult struct {
	Service        string
	PVCName        string
	SnapshotName   string
	JobName        string
	Status         string // "pending", "running", "completed", "failed"
	Error          error
}

// needsVolumeRestore checks if the stack has volumes to restore
func (r *StackReconciler) needsVolumeRestore(stack *envv1alpha1.Stack) bool {
	return len(stack.Spec.Volumes) > 0
}

// restoreVolumes handles volume restoration from snapshots
// Returns true if restore is complete, false if still in progress
func (r *StackReconciler) restoreVolumes(ctx context.Context, stack *envv1alpha1.Stack, pvcObjects []*unstructured.Unstructured) (bool, []RestoreResult, error) {
	log := logf.FromContext(ctx)

	if !r.needsVolumeRestore(stack) {
		return true, nil, nil
	}

	var results []RestoreResult
	allComplete := true

	for serviceName, serviceVolumes := range stack.Spec.Volumes {
		for _, snapshotRef := range serviceVolumes.SnapshotRefs {
			// Get the VolumeSnapshot
			var vs envv1alpha1.VolumeSnapshot
			if err := r.Get(ctx, client.ObjectKey{Name: snapshotRef, Namespace: stack.Namespace}, &vs); err != nil {
				if apierrors.IsNotFound(err) {
					results = append(results, RestoreResult{
						Service:      serviceName,
						SnapshotName: snapshotRef,
						Status:       "failed",
						Error:        fmt.Errorf("VolumeSnapshot %s not found", snapshotRef),
					})
					continue
				}
				return false, results, fmt.Errorf("failed to get VolumeSnapshot %s: %w", snapshotRef, err)
			}

			// Check if snapshot is completed
			if vs.Status.Phase != envv1alpha1.VolumeSnapshotPhaseCompleted {
				results = append(results, RestoreResult{
					Service:      serviceName,
					SnapshotName: snapshotRef,
					Status:       "failed",
					Error:        fmt.Errorf("VolumeSnapshot %s is not completed (phase: %s)", snapshotRef, vs.Status.Phase),
				})
				continue
			}

			// Find matching PVC by mount path
			pvcName := r.findPVCForRestore(pvcObjects, serviceName, vs.Spec.VolumeIdentifier.MountPath)
			if pvcName == "" {
				log.Info("No matching PVC found for restore", "service", serviceName, "mountPath", vs.Spec.VolumeIdentifier.MountPath)
				continue
			}

			// Check if restore job already exists
			jobName := snapshot.GenerateRestoreJobName(snapshotRef, stack.Name)
			var existingJob batchv1.Job
			err := r.Get(ctx, client.ObjectKey{Name: jobName, Namespace: stack.Namespace}, &existingJob)

			if err == nil {
				// Job exists, check its status
				result := RestoreResult{
					Service:      serviceName,
					PVCName:      pvcName,
					SnapshotName: snapshotRef,
					JobName:      jobName,
				}

				if existingJob.Status.Succeeded > 0 {
					result.Status = "completed"
					log.Info("Restore job completed", "job", jobName, "pvc", pvcName)
				} else if existingJob.Status.Failed > 0 {
					result.Status = "failed"
					result.Error = fmt.Errorf("restore job failed")
					log.Info("Restore job failed", "job", jobName, "pvc", pvcName)
				} else {
					result.Status = "running"
					allComplete = false
					log.Info("Restore job still running", "job", jobName, "pvc", pvcName)
				}

				results = append(results, result)
				continue
			}

			if !apierrors.IsNotFound(err) {
				return false, results, fmt.Errorf("failed to check restore job %s: %w", jobName, err)
			}

			// Create restore job
			jobConfig := snapshot.RestoreJobConfig{
				Name:               jobName,
				Namespace:          stack.Namespace,
				PVCName:            pvcName,
				StoragePath:        vs.Status.StoragePath,
				VolumeSnapshotName: snapshotRef,
				Labels: map[string]string{
					"env.lissto.dev/stack": stack.Name,
				},
			}

			job := snapshot.BuildRestoreJob(jobConfig)

			// Set owner reference so job is cleaned up with stack
			if err := controllerutil.SetControllerReference(stack, job, r.Scheme); err != nil {
				log.Error(err, "Failed to set owner reference on restore job", "job", jobName)
			}

			if err := r.Create(ctx, job); err != nil {
				results = append(results, RestoreResult{
					Service:      serviceName,
					PVCName:      pvcName,
					SnapshotName: snapshotRef,
					JobName:      jobName,
					Status:       "failed",
					Error:        fmt.Errorf("failed to create restore job: %w", err),
				})
				continue
			}

			log.Info("Created restore job", "job", jobName, "pvc", pvcName, "snapshot", snapshotRef)
			results = append(results, RestoreResult{
				Service:      serviceName,
				PVCName:      pvcName,
				SnapshotName: snapshotRef,
				JobName:      jobName,
				Status:       "pending",
			})
			allComplete = false
		}
	}

	return allComplete, results, nil
}

// findPVCForRestore finds the PVC name that matches the service and mount path
func (r *StackReconciler) findPVCForRestore(pvcObjects []*unstructured.Unstructured, serviceName, mountPath string) string {
	for _, obj := range pvcObjects {
		if obj.GetKind() != "PersistentVolumeClaim" {
			continue
		}

		labels := obj.GetLabels()
		annotations := obj.GetAnnotations()

		// Check if PVC belongs to the service
		pvcService := labels["io.kompose.service"]
		if pvcService == "" {
			pvcService = labels["app.kubernetes.io/component"]
		}
		if pvcService == "" {
			pvcService = labels["app"]
		}

		if pvcService != serviceName {
			continue
		}

		// Check mount path if specified
		pvcMountPath := annotations["env.lissto.dev/mount-path"]
		if mountPath != "" && pvcMountPath != "" && pvcMountPath != mountPath {
			continue
		}

		return obj.GetName()
	}

	return ""
}

// SetupWithManager sets up the controller with the Manager.
func (r *StackReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&envv1alpha1.Stack{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("stack").
		Complete(r)
}
