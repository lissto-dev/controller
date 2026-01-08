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
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	"github.com/lissto-dev/controller/internal/stack/suspension"
)

// StackReconciler suspension methods

func (r *StackReconciler) deleteWorkloadResource(ctx context.Context, obj *unstructured.Unstructured, namespace string) error {
	obj.SetNamespace(namespace)
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())
	if err := r.Get(ctx, client.ObjectKeyFromObject(obj), existing); err != nil {
		return client.IgnoreNotFound(err)
	}
	if err := r.Delete(ctx, existing); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	logf.FromContext(ctx).Info("Deleted workload", "kind", obj.GetKind(), "name", obj.GetName())
	return nil
}

func (r *StackReconciler) waitForWorkloadsTermination(ctx context.Context, stack *envv1alpha1.Stack) (bool, error) {
	var podList corev1.PodList
	if err := r.List(ctx, &podList, client.InNamespace(stack.Namespace)); err != nil {
		return false, err
	}

	for _, pod := range podList.Items {
		svc := pod.Labels["io.kompose.service"]
		if svc != "" && suspension.ShouldSuspendService(stack, svc) {
			if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending {
				return false, nil
			}
		}
	}
	return true, nil
}

func (r *StackReconciler) transitionPhase(ctx context.Context, stack *envv1alpha1.Stack, newPhase envv1alpha1.StackPhase, reason, message string) {
	if stack.Status.Phase == newPhase {
		return
	}

	stack.Status.Phase = newPhase
	stack.Status.PhaseHistory = append(stack.Status.PhaseHistory, envv1alpha1.PhaseTransition{
		Phase:          newPhase,
		TransitionTime: metav1.Now(),
		Reason:         reason,
		Message:        message,
	})

	if len(stack.Status.PhaseHistory) > envv1alpha1.MaxPhaseHistoryLength {
		stack.Status.PhaseHistory = stack.Status.PhaseHistory[len(stack.Status.PhaseHistory)-envv1alpha1.MaxPhaseHistoryLength:]
	}

	r.Recorder.Eventf(stack, corev1.EventTypeNormal, reason, message)
}

func (r *StackReconciler) updateServiceStatuses(ctx context.Context, stack *envv1alpha1.Stack, objects []*unstructured.Unstructured) {
	if stack.Status.Services == nil {
		stack.Status.Services = make(map[string]envv1alpha1.ServiceStatus)
	}

	services := make(map[string]bool)
	for _, obj := range objects {
		if svc := suspension.GetServiceName(obj); svc != "" {
			services[svc] = true
		}
	}

	now := metav1.Now()
	for svc := range services {
		suspended := suspension.ShouldSuspendService(stack, svc)
		current := stack.Status.Services[svc]

		if suspended && current.Phase != envv1alpha1.StackPhaseSuspended {
			stack.Status.Services[svc] = envv1alpha1.ServiceStatus{Phase: envv1alpha1.StackPhaseSuspended, SuspendedAt: &now}
			r.Recorder.Eventf(stack, corev1.EventTypeNormal, "ServiceSuspended", "Service '%s' suspended", svc)
		} else if !suspended && current.Phase != envv1alpha1.StackPhaseRunning {
			if current.Phase == envv1alpha1.StackPhaseSuspended {
				r.Recorder.Eventf(stack, corev1.EventTypeNormal, "ServiceResumed", "Service '%s' resumed", svc)
			}
			stack.Status.Services[svc] = envv1alpha1.ServiceStatus{Phase: envv1alpha1.StackPhaseRunning}
		}
	}

	for svc := range stack.Status.Services {
		if !services[svc] {
			delete(stack.Status.Services, svc)
		}
	}
}

func (r *StackReconciler) getSuspendTimeout(stack *envv1alpha1.Stack) time.Duration {
	if stack.Spec.Suspension != nil && stack.Spec.Suspension.Timeout != nil {
		return stack.Spec.Suspension.Timeout.Duration
	}
	return r.Config.GetSuspendTimeout()
}
