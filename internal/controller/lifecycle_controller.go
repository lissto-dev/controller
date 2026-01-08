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
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	"github.com/lissto-dev/controller/pkg/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	lifecycleFinalizerName     = "lifecycle.env.lissto.dev/finalizer"
	DefaultStackSuspendTimeout = 5 * time.Minute
)

// workerHandle holds the cancel function and metadata for a running worker
type workerHandle struct {
	cancel     context.CancelFunc
	generation int64
}

// LifecycleReconciler reconciles a Lifecycle object
type LifecycleReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Config   *config.Config

	workers map[string]workerHandle
	mu      sync.Mutex
}

// +kubebuilder:rbac:groups=env.lissto.dev,resources=lifecycles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=env.lissto.dev,resources=lifecycles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=env.lissto.dev,resources=lifecycles/finalizers,verbs=update
// +kubebuilder:rbac:groups=env.lissto.dev,resources=stacks,verbs=get;list;watch;delete;update;patch
// +kubebuilder:rbac:groups=env.lissto.dev,resources=blueprints,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile handles Lifecycle object changes
func (r *LifecycleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var lifecycle envv1alpha1.Lifecycle
	if err := r.Get(ctx, req.NamespacedName, &lifecycle); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		r.stopWorker(req.Name)
		return ctrl.Result{}, nil
	}

	// Handle deletion
	if !lifecycle.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &lifecycle)
	}

	// Ensure finalizer
	if !controllerutil.ContainsFinalizer(&lifecycle, lifecycleFinalizerName) {
		controllerutil.AddFinalizer(&lifecycle, lifecycleFinalizerName)
		if err := r.Update(ctx, &lifecycle); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Manage worker
	r.mu.Lock()
	handle, exists := r.workers[lifecycle.Name]
	needsRestart := exists && handle.generation != lifecycle.Generation
	r.mu.Unlock()

	if needsRestart {
		log.Info("Lifecycle spec changed, restarting worker")
		r.stopWorker(lifecycle.Name)
		exists = false
	}

	if !exists {
		log.Info("Starting worker", "interval", lifecycle.Spec.Interval.Duration)
		r.startWorker(&lifecycle)
		r.updateNextRunTime(ctx, &lifecycle)
	}

	return ctrl.Result{}, nil
}

func (r *LifecycleReconciler) handleDeletion(ctx context.Context, lifecycle *envv1alpha1.Lifecycle) (ctrl.Result, error) {
	r.stopWorker(lifecycle.Name)

	if controllerutil.ContainsFinalizer(lifecycle, lifecycleFinalizerName) {
		controllerutil.RemoveFinalizer(lifecycle, lifecycleFinalizerName)
		if err := r.Update(ctx, lifecycle); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *LifecycleReconciler) updateNextRunTime(ctx context.Context, lifecycle *envv1alpha1.Lifecycle) {
	now := metav1.Now()
	nextRun := metav1.NewTime(now.Add(lifecycle.Spec.Interval.Duration))
	lifecycle.Status.NextRunTime = &nextRun
	_ = r.Status().Update(ctx, lifecycle)
}

// SetupWithManager sets up the controller with the Manager
func (r *LifecycleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&envv1alpha1.Lifecycle{}).
		Named("lifecycle").
		Complete(r)
}
