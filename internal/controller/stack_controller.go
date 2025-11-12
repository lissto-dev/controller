/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	"github.com/lissto-dev/controller/pkg/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// StackReconciler reconciles a Stack object
type StackReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Config *config.Config
}

// +kubebuilder:rbac:groups=env.lissto.dev,resources=stacks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=env.lissto.dev,resources=stacks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=env.lissto.dev,resources=stacks/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

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

	// Fetch manifests from ConfigMap
	configMapName := stack.Spec.ManifestsConfigMapRef
	if configMapName == "" {
		log.Error(nil, "Stack has no manifests ConfigMap reference")
		return ctrl.Result{}, nil
	}

	manifests, err := r.fetchManifests(ctx, stack.Namespace, configMapName)
	if err != nil {
		log.Error(err, "Failed to fetch manifests from ConfigMap")
		r.updateStackStatus(ctx, stack, nil, fmt.Errorf("failed to fetch manifests: %w", err))
		return ctrl.Result{}, err
	}

	// Parse manifests into Kubernetes objects
	objects, err := parseManifests(manifests)
	if err != nil {
		log.Error(err, "Failed to parse manifests")
		r.updateStackStatus(ctx, stack, nil, fmt.Errorf("failed to parse manifests: %w", err))
		return ctrl.Result{}, err
	}

	log.Info("Parsed manifests", "count", len(objects))

	// Inject images from Stack spec into deployments
	r.injectImages(ctx, objects, stack.Spec.Images)

	// Apply all resources
	results := r.applyResources(ctx, objects, stack.Namespace)

	// Set owner references on successfully applied resources
	r.setOwnerReferences(ctx, stack, objects, results)

	// Update Stack status with results
	r.updateStackStatus(ctx, stack, results, nil)

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

// injectImages updates deployment container images from Stack spec
func (r *StackReconciler) injectImages(ctx context.Context, objects []*unstructured.Unstructured, images map[string]envv1alpha1.ImageInfo) {
	log := logf.FromContext(ctx)

	for _, obj := range objects {
		// Only process Deployments
		if obj.GetKind() != "Deployment" {
			continue
		}

		deploymentName := obj.GetName()

		// Get the containers array from spec.template.spec.containers
		containers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
		if err != nil || !found {
			log.Error(err, "Failed to get containers from Deployment", "name", deploymentName)
			continue
		}

		// Update each container's image if we have it in stack.Spec.Images
		updated := false
		for i, container := range containers {
			containerMap, ok := container.(map[string]interface{})
			if !ok {
				continue
			}

			containerName, _, _ := unstructured.NestedString(containerMap, "name")

			// Check if we have an image for this service/container
			if imageInfo, exists := images[containerName]; exists {
				// Use the digest from Stack spec
				containerMap["image"] = imageInfo.Digest
				containers[i] = containerMap
				updated = true

				log.Info("Injected image into Deployment",
					"deployment", deploymentName,
					"container", containerName,
					"image", imageInfo.Digest)
			}
		}

		// Write back the updated containers
		if updated {
			if err := unstructured.SetNestedSlice(obj.Object, containers, "spec", "template", "spec", "containers"); err != nil {
				log.Error(err, "Failed to set containers in Deployment", "name", deploymentName)
			}
		}
	}
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

		// Try to get the resource first to see if it exists
		existing := &unstructured.Unstructured{}
		existing.SetGroupVersionKind(obj.GroupVersionKind())
		err := r.Get(ctx, client.ObjectKeyFromObject(obj), existing)

		if err != nil {
			// Resource doesn't exist, create it
			if err := r.Create(ctx, obj); err != nil {
				log.Error(err, "Failed to create resource",
					"kind", obj.GetKind(),
					"name", obj.GetName())
				result.Applied = false
				result.Error = err
			} else {
				log.Info("Created resource",
					"kind", obj.GetKind(),
					"name", obj.GetName())
				result.Applied = true
			}
		} else {
			// Resource exists, check if it's a PVC (special handling)
			if obj.GetKind() == "PersistentVolumeClaim" {
				// PVCs can't be updated after creation except for resources.requests
				// Skip update for PVCs to avoid immutable field errors
				log.Info("Skipping PVC update (immutable after creation)",
					"kind", obj.GetKind(),
					"name", obj.GetName())
				result.Applied = true // Consider it successful since it exists
			} else {
				// For other resources, update normally
				obj.SetResourceVersion(existing.GetResourceVersion())
				if err := r.Update(ctx, obj); err != nil {
					log.Error(err, "Failed to update resource",
						"kind", obj.GetKind(),
						"name", obj.GetName())
					result.Applied = false
					result.Error = err
				} else {
					log.Info("Updated resource",
						"kind", obj.GetKind(),
						"name", obj.GetName())
					result.Applied = true
				}
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
		for _, ref := range ownerRefs {
			if ref.UID == stack.UID {
				log.Info("Owner reference already exists",
					"kind", obj.GetKind(),
					"name", obj.GetName())
				continue
			}
		}

		if err := controllerutil.SetOwnerReference(stack, obj, r.Scheme); err != nil {
			log.Error(err, "Failed to set owner reference",
				"kind", obj.GetKind(),
				"name", obj.GetName())
			continue
		}

		if err := r.Update(ctx, obj); err != nil {
			log.Error(err, "Failed to update resource with owner reference",
				"kind", obj.GetKind(),
				"name", obj.GetName())
		} else {
			log.Info("Set owner reference",
				"kind", obj.GetKind(),
				"name", obj.GetName())
		}
	}
}

// updateStackStatus updates Stack conditions based on resource application results
func (r *StackReconciler) updateStackStatus(ctx context.Context, stack *envv1alpha1.Stack,
	results []ResourceResult, parseError error) {
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

// SetupWithManager sets up the controller with the Manager.
func (r *StackReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&envv1alpha1.Stack{}).
		Named("stack").
		Complete(r)
}
