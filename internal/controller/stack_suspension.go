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
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
)

// ResourceClassification holds the classification of a resource
type ResourceClassification struct {
	Class       string // "state" or "workload"
	ServiceName string // from io.kompose.service label
}

// getResourceClassification extracts the resource class and service name from an object
// Returns an error if the lissto.dev/class annotation is missing
func getResourceClassification(obj *unstructured.Unstructured) (*ResourceClassification, error) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return nil, fmt.Errorf("resource %s/%s is missing annotations (lissto.dev/class required)",
			obj.GetKind(), obj.GetName())
	}

	class, ok := annotations[envv1alpha1.ResourceClassAnnotation]
	if !ok {
		return nil, fmt.Errorf("resource %s/%s is missing required annotation %s",
			obj.GetKind(), obj.GetName(), envv1alpha1.ResourceClassAnnotation)
	}

	if class != envv1alpha1.ResourceClassState && class != envv1alpha1.ResourceClassWorkload {
		return nil, fmt.Errorf("resource %s/%s has invalid %s value '%s' (must be '%s' or '%s')",
			obj.GetKind(), obj.GetName(), envv1alpha1.ResourceClassAnnotation, class,
			envv1alpha1.ResourceClassState, envv1alpha1.ResourceClassWorkload)
	}

	// Get service name from io.kompose.service label
	labels := obj.GetLabels()
	serviceName := ""
	if labels != nil {
		serviceName = labels["io.kompose.service"]
	}

	return &ResourceClassification{
		Class:       class,
		ServiceName: serviceName,
	}, nil
}

// shouldSuspendService checks if a service should be suspended based on stack spec
func shouldSuspendService(stack *envv1alpha1.Stack, serviceName string) bool {
	// If stack-wide suspension is enabled, all services are suspended
	if stack.Spec.Suspended {
		return true
	}

	// Check if this specific service is in the suspended list
	return slices.Contains(stack.Spec.SuspendedServices, serviceName)
}

// shouldSuspendResource determines if a resource should be suspended
func shouldSuspendResource(stack *envv1alpha1.Stack, classification *ResourceClassification) bool {
	// State resources are never suspended (deleted)
	if classification.Class == envv1alpha1.ResourceClassState {
		return false
	}

	// Workload resources are suspended if the service is suspended
	return shouldSuspendService(stack, classification.ServiceName)
}

// validateResourceAnnotations validates that all resources have the required lissto.dev/class annotation
func validateResourceAnnotations(objects []*unstructured.Unstructured) error {
	var missingAnnotations []string

	for _, obj := range objects {
		_, err := getResourceClassification(obj)
		if err != nil {
			missingAnnotations = append(missingAnnotations, err.Error())
		}
	}

	if len(missingAnnotations) > 0 {
		return fmt.Errorf("resource validation failed: %v", missingAnnotations)
	}

	return nil
}

// collectServiceNames extracts all unique service names from the objects
func collectServiceNames(objects []*unstructured.Unstructured) []string {
	serviceSet := make(map[string]struct{})

	for _, obj := range objects {
		labels := obj.GetLabels()
		if labels != nil {
			if serviceName := labels["io.kompose.service"]; serviceName != "" {
				serviceSet[serviceName] = struct{}{}
			}
		}
	}

	services := make([]string, 0, len(serviceSet))
	for service := range serviceSet {
		services = append(services, service)
	}
	return services
}

// transitionPhase updates the stack phase and records it in history
func (r *StackReconciler) transitionPhase(ctx context.Context, stack *envv1alpha1.Stack, newPhase envv1alpha1.StackPhase, reason, message string) {
	log := logf.FromContext(ctx)

	oldPhase := stack.Status.Phase
	if oldPhase == newPhase {
		return
	}

	now := metav1.Now()

	// Update phase
	stack.Status.Phase = newPhase

	// Add to history
	transition := envv1alpha1.PhaseTransition{
		Phase:          newPhase,
		TransitionTime: now,
		Reason:         reason,
		Message:        message,
	}

	stack.Status.PhaseHistory = append(stack.Status.PhaseHistory, transition)

	// Trim history to max length
	if len(stack.Status.PhaseHistory) > envv1alpha1.MaxPhaseHistoryLength {
		stack.Status.PhaseHistory = stack.Status.PhaseHistory[len(stack.Status.PhaseHistory)-envv1alpha1.MaxPhaseHistoryLength:]
	}

	// Emit event
	eventType := corev1.EventTypeNormal
	r.Recorder.Eventf(stack, eventType, reason, message)

	log.Info("Phase transition", "from", oldPhase, "to", newPhase, "reason", reason)
}

// updateServiceStatuses updates the per-service status in the stack
func (r *StackReconciler) updateServiceStatuses(ctx context.Context, stack *envv1alpha1.Stack, objects []*unstructured.Unstructured) {
	if stack.Status.Services == nil {
		stack.Status.Services = make(map[string]envv1alpha1.ServiceStatus)
	}

	services := collectServiceNames(objects)
	now := metav1.Now()

	for _, serviceName := range services {
		isSuspended := shouldSuspendService(stack, serviceName)
		currentStatus, exists := stack.Status.Services[serviceName]

		if isSuspended {
			if !exists || currentStatus.Phase != envv1alpha1.StackPhaseSuspended {
				stack.Status.Services[serviceName] = envv1alpha1.ServiceStatus{
					Phase:       envv1alpha1.StackPhaseSuspended,
					SuspendedAt: &now,
				}
				r.Recorder.Eventf(stack, corev1.EventTypeNormal, "ServiceSuspended",
					"Service '%s' suspended", serviceName)
			}
		} else {
			if !exists || currentStatus.Phase != envv1alpha1.StackPhaseRunning {
				// If transitioning from suspended to running, emit event
				if exists && currentStatus.Phase == envv1alpha1.StackPhaseSuspended {
					r.Recorder.Eventf(stack, corev1.EventTypeNormal, "ServiceResumed",
						"Service '%s' resumed", serviceName)
				}
				stack.Status.Services[serviceName] = envv1alpha1.ServiceStatus{
					Phase:       envv1alpha1.StackPhaseRunning,
					SuspendedAt: nil,
				}
			}
		}
	}

	// Remove services that no longer exist in manifests
	for serviceName := range stack.Status.Services {
		if !slices.Contains(services, serviceName) {
			delete(stack.Status.Services, serviceName)
		}
	}
}

// getSuspendTimeout returns the suspend timeout for the stack
func (r *StackReconciler) getSuspendTimeout(stack *envv1alpha1.Stack) time.Duration {
	if stack.Spec.SuspendTimeout != nil && stack.Spec.SuspendTimeout.Duration > 0 {
		return stack.Spec.SuspendTimeout.Duration
	}
	return r.Config.GetSuspendTimeout()
}

// deleteWorkloadResource deletes a single workload resource
func (r *StackReconciler) deleteWorkloadResource(ctx context.Context, obj *unstructured.Unstructured, namespace string) error {
	log := logf.FromContext(ctx)

	// Set namespace
	obj.SetNamespace(namespace)

	// Check if resource exists
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())
	err := r.Get(ctx, client.ObjectKeyFromObject(obj), existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Already deleted
			return nil
		}
		return fmt.Errorf("failed to check if resource exists: %w", err)
	}

	// Delete the resource
	if err := r.Delete(ctx, existing); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to delete resource: %w", err)
	}

	log.Info("Deleted workload resource", "kind", obj.GetKind(), "name", obj.GetName())
	return nil
}

// waitForWorkloadsTermination waits for all pods in the namespace to terminate
func (r *StackReconciler) waitForWorkloadsTermination(ctx context.Context, stack *envv1alpha1.Stack) (bool, error) {
	log := logf.FromContext(ctx)

	// List pods in the namespace
	var podList corev1.PodList
	if err := r.List(ctx, &podList, client.InNamespace(stack.Namespace)); err != nil {
		return false, fmt.Errorf("failed to list pods: %w", err)
	}

	// Count running/pending pods for suspended services
	runningPods := 0
	for _, pod := range podList.Items {
		// Check if pod belongs to a suspended service
		serviceName := pod.Labels["io.kompose.service"]
		if serviceName == "" {
			continue
		}

		if !shouldSuspendService(stack, serviceName) {
			continue
		}

		if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending {
			runningPods++
		}
	}

	if runningPods == 0 {
		log.Info("All suspended workloads terminated")
		return true, nil
	}

	log.Info("Waiting for workloads to terminate", "runningPods", runningPods)
	return false, nil
}

// determineStackPhase determines the overall stack phase based on current state
func (r *StackReconciler) determineStackPhase(stack *envv1alpha1.Stack, allWorkloadsTerminated bool) envv1alpha1.StackPhase {
	// If stack-wide suspension is requested
	if stack.Spec.Suspended {
		if allWorkloadsTerminated {
			return envv1alpha1.StackPhaseSuspended
		}
		return envv1alpha1.StackPhaseSuspending
	}

	// If no stack-wide suspension, phase is Running
	// (per-service suspension doesn't change stack phase)
	currentPhase := stack.Status.Phase

	// If we were suspended and now resuming
	if currentPhase == envv1alpha1.StackPhaseSuspended {
		return envv1alpha1.StackPhaseResuming
	}

	// If we were resuming and workloads are back
	if currentPhase == envv1alpha1.StackPhaseResuming {
		// Check if all services that should be running are running
		// For simplicity, we transition to Running after one reconcile
		return envv1alpha1.StackPhaseRunning
	}

	// Default to Running
	if currentPhase == "" {
		return envv1alpha1.StackPhaseRunning
	}

	return envv1alpha1.StackPhaseRunning
}

// categorizeResources separates resources into state and workload categories
// and further separates workloads into those to apply and those to delete
func (r *StackReconciler) categorizeResources(stack *envv1alpha1.Stack, objects []*unstructured.Unstructured) (
	stateResources []*unstructured.Unstructured,
	workloadsToApply []*unstructured.Unstructured,
	workloadsToDelete []*unstructured.Unstructured,
) {
	for _, obj := range objects {
		classification, err := getResourceClassification(obj)
		if err != nil {
			// This shouldn't happen if validation passed, but handle gracefully
			continue
		}

		if classification.Class == envv1alpha1.ResourceClassState {
			stateResources = append(stateResources, obj)
		} else {
			// Workload resource
			if shouldSuspendResource(stack, classification) {
				workloadsToDelete = append(workloadsToDelete, obj)
			} else {
				workloadsToApply = append(workloadsToApply, obj)
			}
		}
	}

	return stateResources, workloadsToApply, workloadsToDelete
}
