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
	"sync"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	lifecycleFinalizerName = "lifecycle.env.lissto.dev/finalizer"
)

// workerHandle holds the cancel function and metadata for a running worker
type workerHandle struct {
	cancel     context.CancelFunc
	generation int64
	executing  *atomic.Bool // Tracks if a task is currently running
}

// LifecycleReconciler reconciles a Lifecycle object
type LifecycleReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	// workers tracks active worker goroutines by Lifecycle name
	workers map[string]workerHandle
	mu      sync.Mutex
}

// +kubebuilder:rbac:groups=env.lissto.dev,resources=lifecycles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=env.lissto.dev,resources=lifecycles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=env.lissto.dev,resources=lifecycles/finalizers,verbs=update
// +kubebuilder:rbac:groups=env.lissto.dev,resources=stacks,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=env.lissto.dev,resources=blueprints,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile handles Lifecycle object changes
func (r *LifecycleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the Lifecycle instance
	var lifecycle envv1alpha1.Lifecycle
	if err := r.Get(ctx, req.NamespacedName, &lifecycle); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		// Lifecycle was deleted, stop the worker if running
		r.stopWorker(req.Name)
		return ctrl.Result{}, nil
	}

	// Handle deletion
	if !lifecycle.DeletionTimestamp.IsZero() {
		log.Info("Lifecycle is being deleted, stopping worker", "name", lifecycle.Name)
		r.stopWorker(lifecycle.Name)

		if controllerutil.ContainsFinalizer(&lifecycle, lifecycleFinalizerName) {
			controllerutil.RemoveFinalizer(&lifecycle, lifecycleFinalizerName)
			if err := r.Update(ctx, &lifecycle); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&lifecycle, lifecycleFinalizerName) {
		controllerutil.AddFinalizer(&lifecycle, lifecycleFinalizerName)
		if err := r.Update(ctx, &lifecycle); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Check if worker needs to be started or restarted
	r.mu.Lock()
	handle, exists := r.workers[lifecycle.Name]
	needsRestart := exists && handle.generation != lifecycle.Generation
	r.mu.Unlock()

	if needsRestart {
		log.Info("Lifecycle spec changed, restarting worker", "name", lifecycle.Name)
		r.stopWorker(lifecycle.Name)
		exists = false
	}

	if !exists {
		log.Info("Starting worker for Lifecycle", "name", lifecycle.Name, "interval", lifecycle.Spec.Interval.Duration)
		r.startWorker(&lifecycle)

		// Update status with next run time
		now := metav1.Now()
		nextRun := metav1.NewTime(now.Add(lifecycle.Spec.Interval.Duration))
		lifecycle.Status.NextRunTime = &nextRun

		if err := r.Status().Update(ctx, &lifecycle); err != nil {
			log.Error(err, "Failed to update Lifecycle status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// startWorker spawns a goroutine that executes lifecycle tasks at the specified interval
func (r *LifecycleReconciler) startWorker(lifecycle *envv1alpha1.Lifecycle) {
	ctx, cancel := context.WithCancel(context.Background())

	r.mu.Lock()
	r.workers[lifecycle.Name] = workerHandle{
		cancel:     cancel,
		generation: lifecycle.Generation,
		executing:  &atomic.Bool{},
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

	// Get the worker handle to access the executing flag
	r.mu.Lock()
	handle, exists := r.workers[lifecycleName]
	r.mu.Unlock()

	if !exists {
		log.Error(fmt.Errorf("worker handle not found"), "Worker started but handle missing")
		return
	}

	for {
		select {
		case <-ctx.Done():
			log.Info("Worker stopped")
			return
		case <-ticker.C:
			// Check if previous execution is still running
			if handle.executing.Load() {
				log.Info("Skipping scheduled run - previous execution still in progress",
					"interval", interval)
				continue
			}

			// Mark as executing and run tasks in goroutine
			handle.executing.Store(true)
			go func() {
				defer handle.executing.Store(false)
				r.executeTasks(ctx, lifecycleName)
			}()
		}
	}
}

// executeTasks runs all configured tasks for the Lifecycle
func (r *LifecycleReconciler) executeTasks(ctx context.Context, lifecycleName string) {
	log := logf.FromContext(ctx).WithValues("lifecycle", lifecycleName)

	start := time.Now()
	defer func() {
		duration := time.Since(start)
		log.Info("Task execution completed", "duration", duration)
	}()

	// Fetch the current Lifecycle object
	var lifecycle envv1alpha1.Lifecycle
	if err := r.Get(ctx, client.ObjectKey{Name: lifecycleName}, &lifecycle); err != nil {
		log.Error(err, "Failed to fetch Lifecycle")
		return
	}

	// Check if execution is taking longer than interval
	defer func() {
		duration := time.Since(start)
		if duration > lifecycle.Spec.Interval.Duration {
			log.Info("Warning: Task execution exceeded interval",
				"duration", duration,
				"interval", lifecycle.Spec.Interval.Duration,
				"drift", duration-lifecycle.Spec.Interval.Duration)
		}
	}()

	log.Info("Executing lifecycle tasks", "targetKind", lifecycle.Spec.TargetKind, "taskCount", len(lifecycle.Spec.Tasks))

	stats := &envv1alpha1.RunStats{}
	var successCount, failCount int64

	for i, task := range lifecycle.Spec.Tasks {
		taskName := task.Name
		if taskName == "" {
			taskName = fmt.Sprintf("task-%d", i)
		}

		if task.Delete != nil {
			deleted, evaluated, err := r.executeDeleteTask(ctx, &lifecycle, task.Delete)
			stats.ObjectsEvaluated += evaluated
			stats.ObjectsDeleted += deleted

			if err != nil {
				failCount++
				r.Recorder.Eventf(&lifecycle, corev1.EventTypeWarning, "TaskFailed",
					"Task %s failed: %v", taskName, err)
				log.Error(err, "Delete task failed", "task", taskName)
			} else {
				successCount++
				log.Info("Delete task completed", "task", taskName, "deleted", deleted, "evaluated", evaluated)
			}
		}
	}

	// Update status
	now := metav1.Now()
	nextRun := metav1.NewTime(now.Add(lifecycle.Spec.Interval.Duration))

	lifecycle.Status.LastRunTime = &now
	lifecycle.Status.NextRunTime = &nextRun
	lifecycle.Status.SuccessfulTasks += successCount
	lifecycle.Status.FailedTasks += failCount
	lifecycle.Status.LastRunStats = stats

	if err := r.Status().Update(ctx, &lifecycle); err != nil {
		log.Error(err, "Failed to update Lifecycle status")
	}
}

// executeDeleteTask deletes objects matching the criteria
func (r *LifecycleReconciler) executeDeleteTask(ctx context.Context, lifecycle *envv1alpha1.Lifecycle, task *envv1alpha1.DeleteTask) (deleted, evaluated int64, err error) {
	log := logf.FromContext(ctx).WithValues("lifecycle", lifecycle.Name, "targetKind", lifecycle.Spec.TargetKind)

	// Build label selector
	var selector labels.Selector
	if lifecycle.Spec.LabelSelector != nil {
		var err error
		selector, err = metav1.LabelSelectorAsSelector(lifecycle.Spec.LabelSelector)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid label selector: %w", err)
		}
	} else {
		selector = labels.Everything()
	}

	listOpts := &client.ListOptions{
		LabelSelector: selector,
	}

	now := time.Now()
	threshold := now.Add(-task.OlderThan.Duration)

	// Collect errors instead of returning immediately
	var errs []error

	switch lifecycle.Spec.TargetKind {
	case "Stack":
		var stackList envv1alpha1.StackList
		if err := r.List(ctx, &stackList, listOpts); err != nil {
			return 0, 0, fmt.Errorf("failed to list Stacks: %w", err)
		}

		evaluated = int64(len(stackList.Items))
		for i := range stackList.Items {
			stack := &stackList.Items[i]
			if stack.CreationTimestamp.Time.Before(threshold) {
				log.Info("Deleting Stack", "name", stack.Name, "namespace", stack.Namespace, "age", now.Sub(stack.CreationTimestamp.Time))
				if err := r.Delete(ctx, stack); err != nil {
					errs = append(errs, fmt.Errorf("Stack %s/%s: %w", stack.Namespace, stack.Name, err))
				} else {
					deleted++
				}
			}
		}

	case "Blueprint":
		var blueprintList envv1alpha1.BlueprintList
		if err := r.List(ctx, &blueprintList, listOpts); err != nil {
			return 0, 0, fmt.Errorf("failed to list Blueprints: %w", err)
		}

		evaluated = int64(len(blueprintList.Items))
		for i := range blueprintList.Items {
			blueprint := &blueprintList.Items[i]
			if blueprint.CreationTimestamp.Time.Before(threshold) {
				log.Info("Deleting Blueprint", "name", blueprint.Name, "namespace", blueprint.Namespace, "age", now.Sub(blueprint.CreationTimestamp.Time))
				if err := r.Delete(ctx, blueprint); err != nil {
					errs = append(errs, fmt.Errorf("Blueprint %s/%s: %w", blueprint.Namespace, blueprint.Name, err))
				} else {
					deleted++
				}
			}
		}

	default:
		return 0, 0, fmt.Errorf("unsupported target kind: %s", lifecycle.Spec.TargetKind)
	}

	// Return accumulated errors if any
	if len(errs) > 0 {
		return deleted, evaluated, fmt.Errorf("failed to delete %d object(s): %v", len(errs), errs)
	}

	return deleted, evaluated, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LifecycleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Initialize workers map
	r.workers = make(map[string]workerHandle)

	return ctrl.NewControllerManagedBy(mgr).
		For(&envv1alpha1.Lifecycle{}).
		Named("lifecycle").
		Complete(r)
}
