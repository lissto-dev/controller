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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	"github.com/lissto-dev/controller/internal/controller/snapshot"
	"github.com/lissto-dev/controller/pkg/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	lifecycleFinalizerName = "lifecycle.env.lissto.dev/finalizer"

	// DefaultScaleDownTimeout is the default timeout for scale down operations
	DefaultScaleDownTimeout = 5 * time.Minute

	// OriginalReplicasAnnotation stores the original replica count before scale down
	OriginalReplicasAnnotation = "lifecycle.env.lissto.dev/original-replicas"

	// ScaledDownByAnnotation tracks which lifecycle scaled down the deployment
	ScaledDownByAnnotation = "lifecycle.env.lissto.dev/scaled-down-by"
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
	Config   *config.Config

	// workers tracks active worker goroutines by Lifecycle name
	workers map[string]workerHandle
	mu      sync.Mutex
}

// +kubebuilder:rbac:groups=env.lissto.dev,resources=lifecycles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=env.lissto.dev,resources=lifecycles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=env.lissto.dev,resources=lifecycles/finalizers,verbs=update
// +kubebuilder:rbac:groups=env.lissto.dev,resources=stacks,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=env.lissto.dev,resources=blueprints,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=env.lissto.dev,resources=volumesnapshots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
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

	// Track stacks that have been scaled down for snapshot tasks
	var scaledDownStacks []envv1alpha1.Stack

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

		if task.ScaleDown != nil {
			stacks, err := r.executeScaleDownTask(ctx, &lifecycle, task.ScaleDown)
			if err != nil {
				failCount++
				r.Recorder.Eventf(&lifecycle, corev1.EventTypeWarning, "TaskFailed",
					"Task %s failed: %v", taskName, err)
				log.Error(err, "ScaleDown task failed", "task", taskName)
			} else {
				successCount++
				scaledDownStacks = stacks
				log.Info("ScaleDown task completed", "task", taskName, "stacksScaled", len(stacks))
			}
		}

		if task.Snapshot != nil {
			snapshotsCreated, err := r.executeSnapshotTask(ctx, &lifecycle, scaledDownStacks)
			if err != nil {
				failCount++
				r.Recorder.Eventf(&lifecycle, corev1.EventTypeWarning, "TaskFailed",
					"Task %s failed: %v", taskName, err)
				log.Error(err, "Snapshot task failed", "task", taskName)
			} else {
				successCount++
				log.Info("Snapshot task completed", "task", taskName, "snapshotsCreated", snapshotsCreated)
			}
		}

		if task.ScaleUp != nil {
			err := r.executeScaleUpTask(ctx, &lifecycle, scaledDownStacks)
			if err != nil {
				failCount++
				r.Recorder.Eventf(&lifecycle, corev1.EventTypeWarning, "TaskFailed",
					"Task %s failed: %v", taskName, err)
				log.Error(err, "ScaleUp task failed", "task", taskName)
			} else {
				successCount++
				log.Info("ScaleUp task completed", "task", taskName)
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

	case "VolumeSnapshot":
		var snapshotList envv1alpha1.VolumeSnapshotList
		if err := r.List(ctx, &snapshotList, listOpts); err != nil {
			return 0, 0, fmt.Errorf("failed to list VolumeSnapshots: %w", err)
		}

		evaluated = int64(len(snapshotList.Items))
		for i := range snapshotList.Items {
			vs := &snapshotList.Items[i]
			if vs.CreationTimestamp.Time.Before(threshold) {
				log.Info("Deleting VolumeSnapshot", "name", vs.Name, "namespace", vs.Namespace, "age", now.Sub(vs.CreationTimestamp.Time))
				if err := r.Delete(ctx, vs); err != nil {
					errs = append(errs, fmt.Errorf("VolumeSnapshot %s/%s: %w", vs.Namespace, vs.Name, err))
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

// executeScaleDownTask scales down all deployments/statefulsets for matching stacks
func (r *LifecycleReconciler) executeScaleDownTask(ctx context.Context, lifecycle *envv1alpha1.Lifecycle, task *envv1alpha1.ScaleDownTask) ([]envv1alpha1.Stack, error) {
	log := logf.FromContext(ctx).WithValues("lifecycle", lifecycle.Name)

	// Only works for Stack target kind
	if lifecycle.Spec.TargetKind != "Stack" {
		return nil, fmt.Errorf("scaleDown task only supports Stack targetKind, got %s", lifecycle.Spec.TargetKind)
	}

	// Get timeout
	timeout := DefaultScaleDownTimeout
	if task.Timeout.Duration > 0 {
		timeout = task.Timeout.Duration
	}

	// Build label selector
	var selector labels.Selector
	if lifecycle.Spec.LabelSelector != nil {
		var err error
		selector, err = metav1.LabelSelectorAsSelector(lifecycle.Spec.LabelSelector)
		if err != nil {
			return nil, fmt.Errorf("invalid label selector: %w", err)
		}
	} else {
		selector = labels.Everything()
	}

	// List matching stacks
	var stackList envv1alpha1.StackList
	if err := r.List(ctx, &stackList, &client.ListOptions{LabelSelector: selector}); err != nil {
		return nil, fmt.Errorf("failed to list Stacks: %w", err)
	}

	var scaledStacks []envv1alpha1.Stack
	var errs []error

	for i := range stackList.Items {
		stack := &stackList.Items[i]
		log.Info("Scaling down stack", "stack", stack.Name, "namespace", stack.Namespace)

		// Scale down deployments in the stack's namespace
		if err := r.scaleDownDeployments(ctx, stack.Namespace, lifecycle.Name, timeout); err != nil {
			errs = append(errs, fmt.Errorf("stack %s/%s: %w", stack.Namespace, stack.Name, err))
			continue
		}

		// Scale down statefulsets in the stack's namespace
		if err := r.scaleDownStatefulSets(ctx, stack.Namespace, lifecycle.Name, timeout); err != nil {
			errs = append(errs, fmt.Errorf("stack %s/%s: %w", stack.Namespace, stack.Name, err))
			continue
		}

		scaledStacks = append(scaledStacks, *stack)
	}

	if len(errs) > 0 {
		return scaledStacks, fmt.Errorf("failed to scale down %d stack(s): %v", len(errs), errs)
	}

	return scaledStacks, nil
}

// scaleDownDeployments scales all deployments in a namespace to 0 replicas
func (r *LifecycleReconciler) scaleDownDeployments(ctx context.Context, namespace, lifecycleName string, timeout time.Duration) error {
	log := logf.FromContext(ctx)

	var deploymentList appsv1.DeploymentList
	if err := r.List(ctx, &deploymentList, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("failed to list deployments: %w", err)
	}

	for i := range deploymentList.Items {
		deploy := &deploymentList.Items[i]
		if deploy.Spec.Replicas == nil || *deploy.Spec.Replicas == 0 {
			continue
		}

		// Store original replicas in annotation
		if deploy.Annotations == nil {
			deploy.Annotations = make(map[string]string)
		}
		deploy.Annotations[OriginalReplicasAnnotation] = fmt.Sprintf("%d", *deploy.Spec.Replicas)
		deploy.Annotations[ScaledDownByAnnotation] = lifecycleName

		// Scale to 0
		zero := int32(0)
		deploy.Spec.Replicas = &zero

		if err := r.Update(ctx, deploy); err != nil {
			return fmt.Errorf("failed to scale down deployment %s: %w", deploy.Name, err)
		}

		log.Info("Scaled down deployment", "deployment", deploy.Name, "namespace", namespace)
	}

	// Wait for pods to terminate
	return r.waitForPodsTermination(ctx, namespace, timeout)
}

// scaleDownStatefulSets scales all statefulsets in a namespace to 0 replicas
func (r *LifecycleReconciler) scaleDownStatefulSets(ctx context.Context, namespace, lifecycleName string, timeout time.Duration) error {
	log := logf.FromContext(ctx)

	var stsList appsv1.StatefulSetList
	if err := r.List(ctx, &stsList, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("failed to list statefulsets: %w", err)
	}

	for i := range stsList.Items {
		sts := &stsList.Items[i]
		if sts.Spec.Replicas == nil || *sts.Spec.Replicas == 0 {
			continue
		}

		// Store original replicas in annotation
		if sts.Annotations == nil {
			sts.Annotations = make(map[string]string)
		}
		sts.Annotations[OriginalReplicasAnnotation] = fmt.Sprintf("%d", *sts.Spec.Replicas)
		sts.Annotations[ScaledDownByAnnotation] = lifecycleName

		// Scale to 0
		zero := int32(0)
		sts.Spec.Replicas = &zero

		if err := r.Update(ctx, sts); err != nil {
			return fmt.Errorf("failed to scale down statefulset %s: %w", sts.Name, err)
		}

		log.Info("Scaled down statefulset", "statefulset", sts.Name, "namespace", namespace)
	}

	return nil
}

// waitForPodsTermination waits for all pods in a namespace to terminate
func (r *LifecycleReconciler) waitForPodsTermination(ctx context.Context, namespace string, timeout time.Duration) error {
	log := logf.FromContext(ctx)

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for pods to terminate in namespace %s", namespace)
			}

			var podList corev1.PodList
			if err := r.List(ctx, &podList, client.InNamespace(namespace)); err != nil {
				return fmt.Errorf("failed to list pods: %w", err)
			}

			runningPods := 0
			for _, pod := range podList.Items {
				if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending {
					runningPods++
				}
			}

			if runningPods == 0 {
				log.Info("All pods terminated", "namespace", namespace)
				return nil
			}

			log.Info("Waiting for pods to terminate", "namespace", namespace, "runningPods", runningPods)
		}
	}
}

// executeSnapshotTask creates volume snapshots for the given stacks
func (r *LifecycleReconciler) executeSnapshotTask(ctx context.Context, lifecycle *envv1alpha1.Lifecycle, stacks []envv1alpha1.Stack) (int, error) {
	log := logf.FromContext(ctx).WithValues("lifecycle", lifecycle.Name)

	if !r.Config.ObjectStorage.Enabled {
		return 0, fmt.Errorf("object storage is not enabled in controller config")
	}

	snapshotsCreated := 0
	var errs []error

	for _, stack := range stacks {
		// Get PVCs in the stack's namespace
		var pvcList corev1.PersistentVolumeClaimList
		if err := r.List(ctx, &pvcList, client.InNamespace(stack.Namespace)); err != nil {
			errs = append(errs, fmt.Errorf("failed to list PVCs for stack %s: %w", stack.Name, err))
			continue
		}

		// Get the blueprint to extract repo info from labels
		var blueprint envv1alpha1.Blueprint
		if err := r.Get(ctx, client.ObjectKey{Name: stack.Spec.BlueprintReference, Namespace: stack.Namespace}, &blueprint); err != nil {
			errs = append(errs, fmt.Errorf("failed to get blueprint for stack %s: %w", stack.Name, err))
			continue
		}

		// Get repository from blueprint label (set during blueprint creation)
		repo := blueprint.Labels["env.lissto.dev/repository"]
		if repo == "" {
			log.Info("Skipping stack - blueprint has no repository label", "stack", stack.Name, "blueprint", blueprint.Name)
			continue
		}

		for _, pvc := range pvcList.Items {
			// Extract volume identifier from PVC labels/annotations
			serviceName := pvc.Labels["app.kubernetes.io/component"]
			if serviceName == "" {
				serviceName = pvc.Labels["app"]
			}
			if serviceName == "" {
				log.Info("Skipping PVC without service label", "pvc", pvc.Name)
				continue
			}

			// Get image from stack spec
			imageInfo, ok := stack.Spec.Images[serviceName]
			if !ok {
				log.Info("Skipping PVC - service not found in stack images", "pvc", pvc.Name, "service", serviceName)
				continue
			}

			// Get mount path from PVC annotations (set during stack creation)
			mountPath := pvc.Annotations["env.lissto.dev/mount-path"]
			if mountPath == "" {
				mountPath = "/data" // Default mount path
			}

			// Create volume identifier
			volumeID := envv1alpha1.VolumeIdentifier{
				Repo:        repo,
				ServiceName: serviceName,
				Image:       imageInfo.Image,
				MountPath:   mountPath,
			}

			// Generate snapshot name
			suffix := fmt.Sprintf("%d", time.Now().Unix())
			snapshotName := snapshot.GenerateSnapshotName(serviceName, suffix)

			// Get user and env from namespace or stack
			user := stack.Namespace // Convention: namespace is user-based
			env := stack.Spec.Env

			// Generate storage path
			storagePath := snapshot.GenerateStoragePath(user, env, volumeID, snapshotName)

			// Create VolumeSnapshot CR
			vs := &envv1alpha1.VolumeSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      snapshotName,
					Namespace: stack.Namespace,
					Labels:    snapshot.GetVolumeSnapshotLabels(user, env, serviceName, volumeID),
					Annotations: map[string]string{
						envv1alpha1.VolumeSnapshotAnnotationSourceBlueprint: blueprint.Name,
					},
				},
				Spec: envv1alpha1.VolumeSnapshotSpec{
					VolumeIdentifier: volumeID,
				},
				Status: envv1alpha1.VolumeSnapshotStatus{
					Phase:       envv1alpha1.VolumeSnapshotPhasePending,
					StoragePath: storagePath,
				},
			}

			if err := r.Create(ctx, vs); err != nil {
				errs = append(errs, fmt.Errorf("failed to create VolumeSnapshot for PVC %s: %w", pvc.Name, err))
				continue
			}

			// Create snapshot job
			jobConfig := snapshot.SnapshotJobConfig{
				Name:               snapshot.GenerateSnapshotJobName(snapshotName),
				Namespace:          stack.Namespace,
				PVCName:            pvc.Name,
				StoragePath:        storagePath,
				VolumeSnapshotName: snapshotName,
				Labels:             snapshot.GetVolumeSnapshotLabels(user, env, serviceName, volumeID),
			}

			job := snapshot.BuildSnapshotJob(jobConfig)
			if err := r.Create(ctx, job); err != nil {
				errs = append(errs, fmt.Errorf("failed to create snapshot job for PVC %s: %w", pvc.Name, err))
				continue
			}

			// Update VolumeSnapshot with job name
			vs.Status.JobName = job.Name
			if err := r.Status().Update(ctx, vs); err != nil {
				log.Error(err, "Failed to update VolumeSnapshot status with job name", "volumeSnapshot", vs.Name)
			}

			snapshotsCreated++
			log.Info("Created volume snapshot", "volumeSnapshot", snapshotName, "pvc", pvc.Name, "storagePath", storagePath)
		}
	}

	if len(errs) > 0 {
		return snapshotsCreated, fmt.Errorf("failed to create some snapshots: %v", errs)
	}

	return snapshotsCreated, nil
}

// executeScaleUpTask restores deployments/statefulsets to their original replica counts
func (r *LifecycleReconciler) executeScaleUpTask(ctx context.Context, lifecycle *envv1alpha1.Lifecycle, stacks []envv1alpha1.Stack) error {
	log := logf.FromContext(ctx).WithValues("lifecycle", lifecycle.Name)

	var errs []error

	for _, stack := range stacks {
		// Scale up deployments
		if err := r.scaleUpDeployments(ctx, stack.Namespace, lifecycle.Name); err != nil {
			errs = append(errs, fmt.Errorf("stack %s/%s deployments: %w", stack.Namespace, stack.Name, err))
		}

		// Scale up statefulsets
		if err := r.scaleUpStatefulSets(ctx, stack.Namespace, lifecycle.Name); err != nil {
			errs = append(errs, fmt.Errorf("stack %s/%s statefulsets: %w", stack.Namespace, stack.Name, err))
		}

		log.Info("Scaled up stack", "stack", stack.Name, "namespace", stack.Namespace)
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to scale up some stacks: %v", errs)
	}

	return nil
}

// scaleUpDeployments restores deployments to their original replica counts
func (r *LifecycleReconciler) scaleUpDeployments(ctx context.Context, namespace, lifecycleName string) error {
	log := logf.FromContext(ctx)

	var deploymentList appsv1.DeploymentList
	if err := r.List(ctx, &deploymentList, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("failed to list deployments: %w", err)
	}

	for i := range deploymentList.Items {
		deploy := &deploymentList.Items[i]

		// Only restore if we scaled it down
		if deploy.Annotations[ScaledDownByAnnotation] != lifecycleName {
			continue
		}

		originalReplicasStr := deploy.Annotations[OriginalReplicasAnnotation]
		if originalReplicasStr == "" {
			continue
		}

		var originalReplicas int32
		if _, err := fmt.Sscanf(originalReplicasStr, "%d", &originalReplicas); err != nil {
			log.Error(err, "Failed to parse original replicas", "deployment", deploy.Name)
			continue
		}

		deploy.Spec.Replicas = &originalReplicas
		delete(deploy.Annotations, OriginalReplicasAnnotation)
		delete(deploy.Annotations, ScaledDownByAnnotation)

		if err := r.Update(ctx, deploy); err != nil {
			return fmt.Errorf("failed to scale up deployment %s: %w", deploy.Name, err)
		}

		log.Info("Scaled up deployment", "deployment", deploy.Name, "replicas", originalReplicas)
	}

	return nil
}

// scaleUpStatefulSets restores statefulsets to their original replica counts
func (r *LifecycleReconciler) scaleUpStatefulSets(ctx context.Context, namespace, lifecycleName string) error {
	log := logf.FromContext(ctx)

	var stsList appsv1.StatefulSetList
	if err := r.List(ctx, &stsList, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("failed to list statefulsets: %w", err)
	}

	for i := range stsList.Items {
		sts := &stsList.Items[i]

		// Only restore if we scaled it down
		if sts.Annotations[ScaledDownByAnnotation] != lifecycleName {
			continue
		}

		originalReplicasStr := sts.Annotations[OriginalReplicasAnnotation]
		if originalReplicasStr == "" {
			continue
		}

		var originalReplicas int32
		if _, err := fmt.Sscanf(originalReplicasStr, "%d", &originalReplicas); err != nil {
			log.Error(err, "Failed to parse original replicas", "statefulset", sts.Name)
			continue
		}

		sts.Spec.Replicas = &originalReplicas
		delete(sts.Annotations, OriginalReplicasAnnotation)
		delete(sts.Annotations, ScaledDownByAnnotation)

		if err := r.Update(ctx, sts); err != nil {
			return fmt.Errorf("failed to scale up statefulset %s: %w", sts.Name, err)
		}

		log.Info("Scaled up statefulset", "statefulset", sts.Name, "replicas", originalReplicas)
	}

	return nil
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
