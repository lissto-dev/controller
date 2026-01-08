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

package suspension

import (
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
)

// ShouldSuspendService checks if a service should be suspended
func ShouldSuspendService(stack *envv1alpha1.Stack, serviceName string) bool {
	if stack.Spec.Suspension == nil || len(stack.Spec.Suspension.Services) == 0 {
		return false
	}
	return slices.Contains(stack.Spec.Suspension.Services, "*") ||
		slices.Contains(stack.Spec.Suspension.Services, serviceName)
}

// IsStackSuspendedAll returns true if all services are suspended (via "*")
func IsStackSuspendedAll(stack *envv1alpha1.Stack) bool {
	return stack.Spec.Suspension != nil && slices.Contains(stack.Spec.Suspension.Services, "*")
}

// GetResourceClass extracts the lissto.dev/class annotation value
func GetResourceClass(obj *unstructured.Unstructured) (string, error) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return "", fmt.Errorf("%s/%s missing annotations", obj.GetKind(), obj.GetName())
	}
	class, ok := annotations[envv1alpha1.ResourceClassAnnotation]
	if !ok {
		return "", fmt.Errorf("%s/%s missing %s", obj.GetKind(), obj.GetName(), envv1alpha1.ResourceClassAnnotation)
	}
	if class != envv1alpha1.ResourceClassState && class != envv1alpha1.ResourceClassWorkload {
		return "", fmt.Errorf("%s/%s has invalid class '%s'", obj.GetKind(), obj.GetName(), class)
	}
	return class, nil
}

// GetServiceName extracts the io.kompose.service label value
func GetServiceName(obj *unstructured.Unstructured) string {
	if labels := obj.GetLabels(); labels != nil {
		return labels["io.kompose.service"]
	}
	return ""
}

// ValidateResourceAnnotations validates all resources have required annotations
func ValidateResourceAnnotations(objects []*unstructured.Unstructured) error {
	var errors []string
	for _, obj := range objects {
		if _, err := GetResourceClass(obj); err != nil {
			errors = append(errors, err.Error())
		}
	}
	if len(errors) > 0 {
		return fmt.Errorf("validation failed: %v", errors)
	}
	return nil
}

// CategorizeResources separates resources into state, workloads to apply, and workloads to delete
func CategorizeResources(stack *envv1alpha1.Stack, objects []*unstructured.Unstructured) (
	state, workloadsApply, workloadsDelete []*unstructured.Unstructured,
) {
	for _, obj := range objects {
		class, _ := GetResourceClass(obj)
		if class == envv1alpha1.ResourceClassState {
			state = append(state, obj)
			continue
		}
		if ShouldSuspendService(stack, GetServiceName(obj)) {
			workloadsDelete = append(workloadsDelete, obj)
		} else {
			workloadsApply = append(workloadsApply, obj)
		}
	}
	return
}

// DetermineStackPhase determines the stack phase based on suspension state
func DetermineStackPhase(stack *envv1alpha1.Stack, allTerminated bool) envv1alpha1.StackPhase {
	if IsStackSuspendedAll(stack) {
		if allTerminated {
			return envv1alpha1.StackPhaseSuspended
		}
		return envv1alpha1.StackPhaseSuspending
	}

	switch stack.Status.Phase {
	case envv1alpha1.StackPhaseSuspended:
		return envv1alpha1.StackPhaseResuming
	case envv1alpha1.StackPhaseResuming, "":
		return envv1alpha1.StackPhaseRunning
	default:
		return envv1alpha1.StackPhaseRunning
	}
}
