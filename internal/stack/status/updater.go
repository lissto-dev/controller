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

package status

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	"github.com/lissto-dev/controller/internal/stack/image"
	"github.com/lissto-dev/controller/internal/stack/resource"
	"github.com/lissto-dev/controller/internal/stack/suspension"
)

// Updater handles stack status updates
type Updater struct {
	client   client.Client
	recorder record.EventRecorder
}

// NewUpdater creates a new status updater
func NewUpdater(c client.Client, recorder record.EventRecorder) *Updater {
	return &Updater{client: c, recorder: recorder}
}

// Update updates Stack conditions based on resource application results
func (u *Updater) Update(ctx context.Context, stack *envv1alpha1.Stack, input UpdateInput) {
	log := logf.FromContext(ctx)

	conditions := []metav1.Condition{}

	if input.ParseError != nil {
		conditions = append(conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: stack.Generation,
			LastTransitionTime: metav1.Now(),
			Reason:             "ParseFailed",
			Message:            input.ParseError.Error(),
		})
		stack.Status.Conditions = conditions
		stack.Status.ObservedGeneration = stack.Generation
		if err := u.client.Status().Update(ctx, stack); err != nil {
			log.Error(err, "Failed to update Stack status")
		}
		return
	}

	conditions = append(conditions, u.buildImageCondition(stack, input.ImageWarnings)...)
	conditions = append(conditions, u.buildConfigCondition(stack, input.ConfigResult)...)
	conditions = append(conditions, u.buildResourceConditions(stack, input.Results)...)

	readyCondition := u.buildReadyCondition(stack, input.Results)
	conditions = append([]metav1.Condition{readyCondition}, conditions...)

	stack.Status.Conditions = conditions
	stack.Status.ObservedGeneration = stack.Generation

	if err := u.client.Status().Update(ctx, stack); err != nil {
		log.Error(err, "Failed to update Stack status")
	}
}

// UpdateWithError updates status with an error condition
func (u *Updater) UpdateWithError(ctx context.Context, stack *envv1alpha1.Stack, err error) {
	u.Update(ctx, stack, UpdateInput{ParseError: err})
}

// TransitionPhase transitions the stack to a new phase
func (u *Updater) TransitionPhase(ctx context.Context, stack *envv1alpha1.Stack, newPhase envv1alpha1.StackPhase, reason, message string) {
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

	u.recorder.Eventf(stack, corev1.EventTypeNormal, reason, message)
}

// UpdateServiceStatuses updates per-service status information
func (u *Updater) UpdateServiceStatuses(ctx context.Context, stack *envv1alpha1.Stack, objects []*unstructured.Unstructured) {
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
			u.recorder.Eventf(stack, corev1.EventTypeNormal, "ServiceSuspended", "Service '%s' suspended", svc)
		} else if !suspended && current.Phase != envv1alpha1.StackPhaseRunning {
			if current.Phase == envv1alpha1.StackPhaseSuspended {
				u.recorder.Eventf(stack, corev1.EventTypeNormal, "ServiceResumed", "Service '%s' resumed", svc)
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

func (u *Updater) buildImageCondition(stack *envv1alpha1.Stack, warnings []image.Warning) []metav1.Condition {
	if len(warnings) > 0 {
		return []metav1.Condition{{
			Type:               "ImageInjection",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: stack.Generation,
			LastTransitionTime: metav1.Now(),
			Reason:             "ImageInjectionWarning",
			Message:            strings.Join(image.FormatWarnings(warnings), "; "),
		}}
	}
	return []metav1.Condition{{
		Type:               "ImageInjection",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: stack.Generation,
		LastTransitionTime: metav1.Now(),
		Reason:             "AllImagesInjected",
		Message:            "All service images injected successfully",
	}}
}

func (u *Updater) buildConfigCondition(stack *envv1alpha1.Stack, configResult *ConfigResult) []metav1.Condition {
	if configResult == nil {
		return nil
	}

	hasMissingKeys := len(configResult.MissingSecretKeys) > 0
	hasWarnings := len(configResult.Warnings) > 0

	if hasMissingKeys || hasWarnings {
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

		return []metav1.Condition{{
			Type:               "ConfigInjection",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: stack.Generation,
			LastTransitionTime: metav1.Now(),
			Reason:             reason,
			Message:            strings.Join(messages, "; "),
		}}
	}

	if configResult.VariablesInjected > 0 || configResult.SecretsInjected > 0 {
		return []metav1.Condition{{
			Type:               "ConfigInjection",
			Status:             metav1.ConditionTrue,
			ObservedGeneration: stack.Generation,
			LastTransitionTime: metav1.Now(),
			Reason:             "ConfigInjected",
			Message:            fmt.Sprintf("Injected %d variables and %d secrets", configResult.VariablesInjected, configResult.SecretsInjected),
		}}
	}

	return nil
}

func (u *Updater) buildResourceConditions(stack *envv1alpha1.Stack, results []resource.Result) []metav1.Condition {
	conditions := make([]metav1.Condition, 0, len(results))
	for _, result := range results {
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
	return conditions
}

func (u *Updater) buildReadyCondition(stack *envv1alpha1.Stack, results []resource.Result) metav1.Condition {
	hasFailures := resource.HasFailures(results)

	condition := metav1.Condition{
		Type:               "Ready",
		ObservedGeneration: stack.Generation,
		LastTransitionTime: metav1.Now(),
	}

	if hasFailures {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "PartialFailure"
		condition.Message = "Some resources failed to apply"
	} else {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "AllResourcesApplied"
		condition.Message = fmt.Sprintf("All %d resources applied successfully", len(results))
	}

	return condition
}
