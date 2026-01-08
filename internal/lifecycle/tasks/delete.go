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

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
)

// DeleteExecutor handles delete task execution
type DeleteExecutor struct {
	Client client.Client
}

// Execute deletes objects older than the specified duration
func (e *DeleteExecutor) Execute(ctx context.Context, lifecycle *envv1alpha1.Lifecycle, task *envv1alpha1.DeleteTask) (deleted, evaluated int64, err error) {
	log := logf.FromContext(ctx)

	selector, err := BuildSelector(lifecycle.Spec.LabelSelector)
	if err != nil {
		return 0, 0, err
	}

	threshold := time.Now().Add(-task.OlderThan.Duration)
	opts := &client.ListOptions{LabelSelector: selector}

	switch lifecycle.Spec.TargetKind {
	case "Stack":
		return e.deleteOldStacks(ctx, opts, threshold, log)
	case "Blueprint":
		return e.deleteOldBlueprints(ctx, opts, threshold, log)
	default:
		return 0, 0, fmt.Errorf("unsupported target kind: %s", lifecycle.Spec.TargetKind)
	}
}

func (e *DeleteExecutor) deleteOldStacks(ctx context.Context, opts *client.ListOptions, threshold time.Time, log logr.Logger) (deleted, evaluated int64, err error) {
	var list envv1alpha1.StackList
	if err := e.Client.List(ctx, &list, opts); err != nil {
		return 0, 0, fmt.Errorf("failed to list Stacks: %w", err)
	}

	evaluated = int64(len(list.Items))
	var errs []error

	for i := range list.Items {
		stack := &list.Items[i]
		if stack.CreationTimestamp.Time.Before(threshold) {
			log.Info("Deleting Stack", "name", stack.Name, "namespace", stack.Namespace)
			if err := e.Client.Delete(ctx, stack); err != nil {
				errs = append(errs, fmt.Errorf("stack %s/%s: %w", stack.Namespace, stack.Name, err))
			} else {
				deleted++
			}
		}
	}

	if len(errs) > 0 {
		return deleted, evaluated, fmt.Errorf("failed to delete %d object(s): %v", len(errs), errs)
	}
	return deleted, evaluated, nil
}

func (e *DeleteExecutor) deleteOldBlueprints(ctx context.Context, opts *client.ListOptions, threshold time.Time, log logr.Logger) (deleted, evaluated int64, err error) {
	var list envv1alpha1.BlueprintList
	if err := e.Client.List(ctx, &list, opts); err != nil {
		return 0, 0, fmt.Errorf("failed to list Blueprints: %w", err)
	}

	evaluated = int64(len(list.Items))
	var errs []error

	for i := range list.Items {
		bp := &list.Items[i]
		if bp.CreationTimestamp.Time.Before(threshold) {
			log.Info("Deleting Blueprint", "name", bp.Name, "namespace", bp.Namespace)
			if err := e.Client.Delete(ctx, bp); err != nil {
				errs = append(errs, fmt.Errorf("blueprint %s/%s: %w", bp.Namespace, bp.Name, err))
			} else {
				deleted++
			}
		}
	}

	if len(errs) > 0 {
		return deleted, evaluated, fmt.Errorf("failed to delete %d object(s): %v", len(errs), errs)
	}
	return deleted, evaluated, nil
}
