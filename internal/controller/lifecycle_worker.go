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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	"github.com/lissto-dev/controller/internal/lifecycle/tasks"
)

// startWorker spawns a goroutine that executes lifecycle tasks at the specified interval
func (r *LifecycleReconciler) startWorker(lifecycle *envv1alpha1.Lifecycle) {
	ctx, cancel := context.WithCancel(context.Background())

	r.mu.Lock()
	if r.workers == nil {
		r.workers = make(map[string]workerHandle)
	}
	r.workers[lifecycle.Name] = workerHandle{
		cancel:     cancel,
		generation: lifecycle.Generation,
	}
	r.mu.Unlock()

	go r.runWorker(ctx, lifecycle.Name, lifecycle.Spec.Interval.Duration)
}

// stopWorker cancels the worker goroutine for the given Lifecycle
func (r *LifecycleReconciler) stopWorker(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if handle, exists := r.workers[name]; exists {
		handle.cancel()
		delete(r.workers, name)
	}
}

// runWorker is the main worker loop that executes tasks at the specified interval
func (r *LifecycleReconciler) runWorker(ctx context.Context, lifecycleName string, interval time.Duration) {
	log := logf.FromContext(ctx).WithValues("lifecycle", lifecycleName)
	log.Info("Worker started", "interval", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("Worker stopped")
			return
		case <-ticker.C:
			r.executeTasks(ctx, lifecycleName)
		}
	}
}

// executeTasks runs all configured tasks for the Lifecycle
func (r *LifecycleReconciler) executeTasks(ctx context.Context, lifecycleName string) {
	log := logf.FromContext(ctx).WithValues("lifecycle", lifecycleName)

	var lifecycle envv1alpha1.Lifecycle
	if err := r.Get(ctx, client.ObjectKey{Name: lifecycleName}, &lifecycle); err != nil {
		log.Error(err, "Failed to fetch Lifecycle")
		return
	}

	log.Info("Executing lifecycle tasks", "targetKind", lifecycle.Spec.TargetKind, "taskCount", len(lifecycle.Spec.Tasks))

	deleteExec := &tasks.DeleteExecutor{Client: r.Client}
	scaleExec := &tasks.ScaleExecutor{Client: r.Client}

	stats := &envv1alpha1.RunStats{}
	var successCount, failCount int64
	var scaledDownStacks []envv1alpha1.Stack

	for i, task := range lifecycle.Spec.Tasks {
		taskName := task.Name
		if taskName == "" {
			taskName = fmt.Sprintf("task-%d", i)
		}

		var err error
		switch {
		case task.Delete != nil:
			var deleted, evaluated int64
			deleted, evaluated, err = deleteExec.Execute(ctx, &lifecycle, task.Delete)
			stats.ObjectsEvaluated += evaluated
			stats.ObjectsDeleted += deleted

		case task.ScaleDown != nil:
			scaledDownStacks, err = scaleExec.ExecuteScaleDown(ctx, &lifecycle, task.ScaleDown)

		case task.ScaleUp != nil:
			err = scaleExec.ExecuteScaleUp(ctx, scaledDownStacks)
		}

		if err != nil {
			failCount++
			r.Recorder.Eventf(&lifecycle, corev1.EventTypeWarning, "TaskFailed", "Task %s failed: %v", taskName, err)
			log.Error(err, "Task failed", "task", taskName)
		} else {
			successCount++
			log.Info("Task completed", "task", taskName)
		}
	}

	r.updateLifecycleStatus(ctx, &lifecycle, stats, successCount, failCount)
}

// updateLifecycleStatus updates the Lifecycle status after task execution
func (r *LifecycleReconciler) updateLifecycleStatus(ctx context.Context, lifecycle *envv1alpha1.Lifecycle, stats *envv1alpha1.RunStats, successCount, failCount int64) {
	log := logf.FromContext(ctx)

	now := metav1.Now()
	nextRun := metav1.NewTime(now.Add(lifecycle.Spec.Interval.Duration))

	lifecycle.Status.LastRunTime = &now
	lifecycle.Status.NextRunTime = &nextRun
	lifecycle.Status.SuccessfulTasks += successCount
	lifecycle.Status.FailedTasks += failCount
	lifecycle.Status.LastRunStats = stats

	if err := r.Status().Update(ctx, lifecycle); err != nil {
		log.Error(err, "Failed to update Lifecycle status")
	}
}
