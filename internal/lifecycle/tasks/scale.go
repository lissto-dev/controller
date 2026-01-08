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

package tasks

import (
	"context"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
)

const DefaultSuspendTimeout = 5 * time.Minute

// ScaleExecutor handles scale up/down task execution
type ScaleExecutor struct {
	Client client.Client
}

// ExecuteScaleDown suspends all matching stacks
func (e *ScaleExecutor) ExecuteScaleDown(ctx context.Context, lifecycle *envv1alpha1.Lifecycle, task *envv1alpha1.ScaleDownTask) ([]envv1alpha1.Stack, error) {
	log := logf.FromContext(ctx)

	if lifecycle.Spec.TargetKind != "Stack" {
		return nil, fmt.Errorf("scaleDown task only supports Stack targetKind")
	}

	selector, err := BuildSelector(lifecycle.Spec.LabelSelector)
	if err != nil {
		return nil, err
	}

	var stackList envv1alpha1.StackList
	if err := e.Client.List(ctx, &stackList, &client.ListOptions{LabelSelector: selector}); err != nil {
		return nil, fmt.Errorf("failed to list Stacks: %w", err)
	}

	timeout := DefaultSuspendTimeout
	if task.Timeout.Duration > 0 {
		timeout = task.Timeout.Duration
	}

	var suspendedStacks []envv1alpha1.Stack
	var errs []error

	for i := range stackList.Items {
		stack := &stackList.Items[i]

		if isAlreadySuspended(stack) {
			log.Info("Stack already suspended", "stack", stack.Name)
			suspendedStacks = append(suspendedStacks, *stack)
			continue
		}

		log.Info("Suspending stack", "stack", stack.Name)
		stack.Spec.Suspension = &envv1alpha1.SuspensionSpec{Services: []string{"*"}}
		if err := e.Client.Update(ctx, stack); err != nil {
			errs = append(errs, fmt.Errorf("failed to suspend stack %s/%s: %w", stack.Namespace, stack.Name, err))
			continue
		}
		suspendedStacks = append(suspendedStacks, *stack)
	}

	if err := e.waitForStacksPhase(ctx, suspendedStacks, envv1alpha1.StackPhaseSuspended, timeout); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return suspendedStacks, fmt.Errorf("errors during scale down: %v", errs)
	}
	return suspendedStacks, nil
}

// ExecuteScaleUp resumes all stacks
func (e *ScaleExecutor) ExecuteScaleUp(ctx context.Context, stacks []envv1alpha1.Stack) error {
	log := logf.FromContext(ctx)
	var errs []error

	for _, stack := range stacks {
		var currentStack envv1alpha1.Stack
		if err := e.Client.Get(ctx, client.ObjectKey{Name: stack.Name, Namespace: stack.Namespace}, &currentStack); err != nil {
			errs = append(errs, err)
			continue
		}

		if currentStack.Spec.Suspension == nil {
			continue
		}

		log.Info("Resuming stack", "stack", stack.Name)
		currentStack.Spec.Suspension = nil
		if err := e.Client.Update(ctx, &currentStack); err != nil {
			errs = append(errs, err)
		}
	}

	if err := e.waitForStacksPhase(ctx, stacks, envv1alpha1.StackPhaseRunning, DefaultSuspendTimeout); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during scale up: %v", errs)
	}
	return nil
}

func isAlreadySuspended(stack *envv1alpha1.Stack) bool {
	if stack.Spec.Suspension == nil {
		return false
	}
	for _, svc := range stack.Spec.Suspension.Services {
		if svc == "*" {
			return true
		}
	}
	return false
}

func (e *ScaleExecutor) waitForStacksPhase(ctx context.Context, stacks []envv1alpha1.Stack, phase envv1alpha1.StackPhase, timeout time.Duration) error {
	if len(stacks) == 0 {
		return nil
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for stacks to reach phase %s", phase)
			}

			allReady := true
			for _, stack := range stacks {
				var current envv1alpha1.Stack
				if err := e.Client.Get(ctx, client.ObjectKey{Name: stack.Name, Namespace: stack.Namespace}, &current); err != nil {
					allReady = false
					continue
				}
				if current.Status.Phase != phase {
					allReady = false
				}
			}

			if allReady {
				return nil
			}
		}
	}
}
